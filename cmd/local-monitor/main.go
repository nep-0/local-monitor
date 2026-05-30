package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"local-monitor/internal/config"
	"local-monitor/internal/database"
	"local-monitor/internal/monitor"
	"local-monitor/internal/server"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	var (
		configPath  string
		showVersion bool
		showStatus  bool
		probeOnce   bool
		jsonOutput  bool
		serve       bool
		listenAddr  string
		cleanupDays int
	)

	flag.StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&showStatus, "status", false, "Show current device statuses")
	flag.BoolVar(&probeOnce, "probe", false, "Run a single probe and exit")
	flag.BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	flag.BoolVar(&serve, "serve", false, "Start REST API and web dashboard")
	flag.StringVar(&listenAddr, "listen", "", "Override HTTP listen address from config")
	flag.IntVar(&cleanupDays, "cleanup", 0, "Cleanup records older than N days (0 = disabled)")
	flag.Parse()

	if showVersion {
		fmt.Printf("local-monitor %s (%s)\n", version, commit)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Setup logger
	logger := setupLogger(cfg.Logging)
	if serve {
		cfg.Server.Enabled = true
	}
	if listenAddr != "" {
		cfg.Server.Listen = listenAddr
	}

	// Initialize database
	db, err := database.New(cfg.Database.Path)
	if err != nil {
		logger.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Cleanup old records if requested
	if cleanupDays > 0 {
		affected, err := db.CleanupOldRecords(time.Duration(cleanupDays) * 24 * time.Hour)
		if err != nil {
			logger.Error("Failed to cleanup old records", "error", err)
		} else {
			logger.Info("Cleaned up old records", "count", affected)
		}
		if !showStatus && !probeOnce {
			return
		}
	}

	// Show status
	if showStatus {
		statuses, err := db.GetLatestStatuses()
		if err != nil {
			logger.Error("Failed to get statuses", "error", err)
			os.Exit(1)
		}
		printStatuses(statuses, jsonOutput)
		return
	}

	// Parse devices from config
	devices, err := parseDevices(cfg.Devices)
	if err != nil {
		logger.Error("Failed to parse devices", "error", err)
		os.Exit(1)
	}

	if len(devices) == 0 {
		logger.Error("No devices configured")
		os.Exit(1)
	}

	// Upsert devices in database
	for _, dev := range devices {
		macStr := ""
		if dev.MAC != nil {
			macStr = dev.MAC.String()
		}
		if _, err := db.UpsertDevice(dev.Name, dev.IP.String(), macStr, dev.Group); err != nil {
			logger.Error("Failed to upsert device", "device", dev.Name, "error", err)
		}
	}

	if cfg.Server.Enabled {
		runServer(cfg, db, devices, logger, cfg.Server.Listen)
		return
	}

	// Single probe mode
	if probeOnce {
		m, err := monitor.New(cfg.Monitor.Interface, cfg.Monitor.Timeout,
			cfg.Monitor.RetryCount, cfg.Monitor.RetryDelay, logger)
		if err != nil {
			logger.Error("Failed to create monitor", "error", err)
			os.Exit(1)
		}

		ctx := context.Background()
		results := m.Probe(ctx, devices)
		processResults(db, results, logger)
		statuses, _ := db.GetLatestStatuses()
		printStatuses(statuses, jsonOutput)
		return
	}

	// Continuous monitoring mode
	runMonitor(cfg, db, devices, logger)
}

func setupLogger(cfg config.LoggingConfig) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func parseDevices(cfgDevices []config.Device) ([]monitor.Device, error) {
	var devices []monitor.Device
	for _, cd := range cfgDevices {
		dev, err := monitor.ParseDevice(cd.Name, cd.IP, cd.MAC, cd.Group)
		if err != nil {
			return nil, fmt.Errorf("device %s: %w", cd.Name, err)
		}
		devices = append(devices, dev)
	}
	return devices, nil
}

func runMonitor(cfg *config.Config, db *database.DB, devices []monitor.Device, logger *slog.Logger) {
	m, err := monitor.New(cfg.Monitor.Interface, cfg.Monitor.Timeout,
		cfg.Monitor.RetryCount, cfg.Monitor.RetryDelay, logger)
	if err != nil {
		logger.Error("Failed to create monitor", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("Received shutdown signal", "signal", sig)
		cancel()
	}()

	runMonitorLoop(ctx, cfg, db, devices, m, logger)
}

func runMonitorLoop(ctx context.Context, cfg *config.Config, db *database.DB, devices []monitor.Device, m *monitor.Monitor, logger *slog.Logger) {
	// Startup probe
	if cfg.Monitor.StartupProbe {
		logger.Info("Running startup probe", "delay", cfg.Monitor.StartupDelay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(cfg.Monitor.StartupDelay):
		}
		results := m.Probe(ctx, devices)
		processResults(db, results, logger)
	}

	// Main monitoring loop
	ticker := time.NewTicker(cfg.Monitor.Interval)
	defer ticker.Stop()

	logger.Info("Starting continuous monitoring",
		"interval", cfg.Monitor.Interval,
		"devices", len(devices),
		"interface", cfg.Monitor.Interface)

	for {
		select {
		case <-ctx.Done():
			logger.Info("Monitoring stopped")
			return
		case <-ticker.C:
			results := m.Probe(ctx, devices)
			processResults(db, results, logger)
		}
	}
}

func runServer(cfg *config.Config, db *database.DB, devices []monitor.Device, logger *slog.Logger, listenAddr string) {
	m, err := monitor.New(cfg.Monitor.Interface, cfg.Monitor.Timeout,
		cfg.Monitor.RetryCount, cfg.Monitor.RetryDelay, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("Received shutdown signal", "signal", sig)
		cancel()
	}()

	if err != nil {
		logger.Warn("Probe endpoint and background monitoring disabled", "error", err)
	} else {
		go runMonitorLoop(ctx, cfg, db, devices, m, logger)
	}

	srv := server.New(db, m, devices, logger)
	httpServer := srv.HTTPServer(listenAddr)

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("Failed to stop HTTP server", "error", err)
		}
	}()

	logger.Info("Starting REST API and dashboard", "addr", listenAddr, "url", server.LocalURL(listenAddr))
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("HTTP server stopped", "error", err)
		os.Exit(1)
	}
}

func processResults(db *database.DB, results []monitor.Result, logger *slog.Logger) {
	for _, r := range results {
		if r.Error != nil {
			logger.Error("Probe error",
				"device", r.Device.Name,
				"ip", r.Device.IP,
				"error", r.Error)
			continue
		}

		// Get device ID from database
		statuses, err := db.GetLatestStatuses()
		if err != nil {
			logger.Error("Failed to get device ID", "error", err)
			continue
		}

		var deviceID int64
		for _, s := range statuses {
			if s.IP == r.Device.IP.String() {
				deviceID = s.ID
				break
			}
		}

		if deviceID == 0 {
			// Device not in database yet, upsert it
			macStr := ""
			if r.Device.MAC != nil {
				macStr = r.Device.MAC.String()
			}
			deviceID, err = db.UpsertDevice(r.Device.Name, r.Device.IP.String(), macStr, r.Device.Group)
			if err != nil {
				logger.Error("Failed to upsert device", "error", err)
				continue
			}
		}

		if err := db.RecordStatus(deviceID, r.Online, r.LastSeen); err != nil {
			logger.Error("Failed to record status",
				"device", r.Device.Name,
				"error", err)
			continue
		}

		status := "offline"
		if r.Online {
			status = "online"
		}
		logger.Info("Device status",
			"device", r.Device.Name,
			"ip", r.Device.IP,
			"status", status)
	}
}

func printStatuses(statuses []database.DeviceStatus, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.MarshalIndent(statuses, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(statuses) == 0 {
		fmt.Println("No devices configured")
		return
	}

	// Print table header
	fmt.Printf("%-20s %-15s %-20s %-8s %-20s\n",
		"NAME", "IP", "MAC", "STATUS", "LAST CHECKED")
	fmt.Println(strings.Repeat("-", 85))

	for _, s := range statuses {
		status := "offline"
		if s.Online {
			status = "online"
		}

		mac := s.MAC
		if mac == "" {
			mac = "N/A"
		}

		lastChecked := s.CheckedAt.Format("2006-01-02 15:04:05")
		if s.CheckedAt.IsZero() {
			lastChecked = "never"
		}

		fmt.Printf("%-20s %-15s %-20s %-8s %-20s\n",
			s.Name, s.IP, mac, status, lastChecked)
	}
}
