package server

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"local-monitor/internal/database"
	"local-monitor/internal/monitor"
)

//go:embed static/*
var staticFiles embed.FS

type Store interface {
	GetLatestStatuses() ([]database.DeviceStatus, error)
	GetDeviceHistory(deviceIP string, limit int) ([]database.DeviceStatus, error)
	GetStatusesSince(since time.Time) ([]database.DeviceStatus, error)
	GetDeviceIDByIP(ip string) (int64, error)
	RecordStatus(deviceID int64, online bool, lastSeen *time.Time) error
}

type Prober interface {
	Probe(ctx context.Context, devices []monitor.Device) []monitor.Result
}

type Server struct {
	store   Store
	prober  Prober
	devices []monitor.Device
	logger  *slog.Logger
	mux     *http.ServeMux
}

func New(store Store, prober Prober, devices []monitor.Device, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		store:   store,
		prober:  prober,
		devices: devices,
		logger:  logger,
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) HTTPServer(addr string) *http.Server {
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return server
}

func (s *Server) ListenAndServe(addr string) error {
	return s.HTTPServer(addr).ListenAndServe()
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/statuses", s.handleStatuses)
	s.mux.HandleFunc("GET /api/timeline", s.handleTimeline)
	s.mux.HandleFunc("GET /api/devices/{ip}/history", s.handleHistory)
	s.mux.HandleFunc("POST /api/probe", s.handleProbe)
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	s.mux.Handle("GET /", http.FileServerFS(static))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStatuses(w http.ResponseWriter, r *http.Request) {
	statuses, err := s.store.GetLatestStatuses()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to get statuses")
		s.logger.Error("Failed to get statuses", "error", err)
		return
	}
	writeJSON(w, http.StatusOK, statuses)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	days := 7
	if rawDays := r.URL.Query().Get("days"); rawDays != "" {
		parsed, err := strconv.Atoi(rawDays)
		if err != nil || parsed < 1 || parsed > 31 {
			s.writeError(w, http.StatusBadRequest, "days must be between 1 and 31")
			return
		}
		days = parsed
	}

	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	latest, err := s.store.GetLatestStatuses()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to get statuses")
		s.logger.Error("Failed to get statuses", "error", err)
		return
	}
	samples, err := s.store.GetStatusesSince(since)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to get timeline")
		s.logger.Error("Failed to get timeline", "error", err)
		return
	}

	response := timelineResponse{
		Since:   since,
		Until:   time.Now(),
		Devices: buildTimelineDevices(latest, samples),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 1000 {
			s.writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = parsed
	}

	history, err := s.store.GetDeviceHistory(r.PathValue("ip"), limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to get device history")
		s.logger.Error("Failed to get device history", "error", err)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if s.prober == nil {
		s.writeError(w, http.StatusServiceUnavailable, "probe is not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	results := s.prober.Probe(ctx, s.devices)
	response := make([]probeResponse, 0, len(results))
	for _, result := range results {
		item := probeResponse{
			Device: result.Device.Name,
			IP:     result.Device.IP.String(),
			Online: result.Online,
		}
		if result.LastSeen != nil {
			item.LastSeen = result.LastSeen
		}
		if result.Error != nil {
			item.Error = result.Error.Error()
			response = append(response, item)
			continue
		}

		deviceID, err := s.store.GetDeviceIDByIP(result.Device.IP.String())
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				item.Error = "device is not registered"
			} else {
				item.Error = "failed to find device"
			}
			s.logger.Error("Failed to find device", "ip", result.Device.IP, "error", err)
			response = append(response, item)
			continue
		}
		if err := s.store.RecordStatus(deviceID, result.Online, result.LastSeen); err != nil {
			item.Error = "failed to record status"
			s.logger.Error("Failed to record status", "ip", result.Device.IP, "error", err)
		}
		response = append(response, item)
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func LocalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "http://" + net.JoinHostPort(host, port)
}

type probeResponse struct {
	Device   string     `json:"device"`
	IP       string     `json:"ip"`
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
	Error    string     `json:"error,omitempty"`
}

type timelineResponse struct {
	Since   time.Time        `json:"since"`
	Until   time.Time        `json:"until"`
	Devices []timelineDevice `json:"devices"`
}

type timelineDevice struct {
	ID      int64            `json:"id"`
	Name    string           `json:"name"`
	IP      string           `json:"ip"`
	Group   string           `json:"group"`
	Online  bool             `json:"online"`
	Samples []timelineSample `json:"samples"`
}

type timelineSample struct {
	Online    bool      `json:"online"`
	CheckedAt time.Time `json:"checked_at"`
}

func buildTimelineDevices(latest []database.DeviceStatus, samples []database.DeviceStatus) []timelineDevice {
	byIP := make(map[string]*timelineDevice, len(latest))
	devices := make([]timelineDevice, 0, len(latest))
	for _, status := range latest {
		devices = append(devices, timelineDevice{
			ID:     status.ID,
			Name:   status.Name,
			IP:     status.IP,
			Group:  status.Group,
			Online: status.Online,
		})
		byIP[status.IP] = &devices[len(devices)-1]
	}

	for _, sample := range samples {
		device, ok := byIP[sample.IP]
		if !ok {
			continue
		}
		device.Samples = append(device.Samples, timelineSample{
			Online:    sample.Online,
			CheckedAt: sample.CheckedAt,
		})
	}

	return devices
}
