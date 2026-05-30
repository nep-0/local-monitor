package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"local-monitor/internal/database"
	"local-monitor/internal/monitor"
)

type fakeStore struct {
	statuses []database.DeviceStatus
	history  []database.DeviceStatus
	deviceID int64
	recorded int
}

func (f *fakeStore) GetLatestStatuses() ([]database.DeviceStatus, error) {
	return f.statuses, nil
}

func (f *fakeStore) GetDeviceHistory(string, int) ([]database.DeviceStatus, error) {
	return f.history, nil
}

func (f *fakeStore) GetDeviceIDByIP(string) (int64, error) {
	return f.deviceID, nil
}

func (f *fakeStore) RecordStatus(int64, bool, *time.Time) error {
	f.recorded++
	return nil
}

type fakeProber struct {
	results []monitor.Result
}

func (f fakeProber) Probe(context.Context, []monitor.Device) []monitor.Result {
	return f.results
}

func TestStatusesEndpoint(t *testing.T) {
	store := &fakeStore{
		statuses: []database.DeviceStatus{
			{ID: 1, Name: "Router", IP: "192.168.1.1", Group: "network", Online: true},
		},
	}
	srv := New(store, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/statuses", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got []database.DeviceStatus
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "Router" {
		t.Fatalf("statuses = %#v, want Router", got)
	}
}

func TestProbeEndpointRecordsResults(t *testing.T) {
	ip := netip.MustParseAddr("192.168.1.1")
	store := &fakeStore{deviceID: 7}
	srv := New(store, fakeProber{
		results: []monitor.Result{
			{Device: monitor.Device{Name: "Router", IP: ip}, Online: true},
		},
	}, []monitor.Device{{Name: "Router", IP: ip}}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/probe", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if store.recorded != 1 {
		t.Fatalf("recorded = %d, want 1", store.recorded)
	}
	if !strings.Contains(rec.Body.String(), `"online":true`) {
		t.Fatalf("response = %s, want online result", rec.Body.String())
	}
}

func TestProbeEndpointUnavailableWithoutProber(t *testing.T) {
	srv := New(&fakeStore{}, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/probe", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
