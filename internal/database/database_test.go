package database

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return db
}

func TestGetLatestStatusesReturnsDevicesWithoutStatus(t *testing.T) {
	db := newTestDB(t)

	if _, err := db.UpsertDevice("Router", "192.168.1.1", "", "network"); err != nil {
		t.Fatalf("UpsertDevice() error = %v", err)
	}

	statuses, err := db.GetLatestStatuses()
	if err != nil {
		t.Fatalf("GetLatestStatuses() error = %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("len(statuses) = %d, want 1", len(statuses))
	}
	if statuses[0].Name != "Router" {
		t.Errorf("Name = %q, want Router", statuses[0].Name)
	}
	if statuses[0].Online {
		t.Error("Online = true, want false for device without status")
	}
	if !statuses[0].CheckedAt.IsZero() {
		t.Errorf("CheckedAt = %v, want zero time", statuses[0].CheckedAt)
	}
}

func TestGetLatestStatusesReturnsRecordedStatus(t *testing.T) {
	db := newTestDB(t)

	deviceID, err := db.UpsertDevice("NAS", "192.168.1.100", "aa:bb:cc:dd:ee:ff", "storage")
	if err != nil {
		t.Fatalf("UpsertDevice() error = %v", err)
	}
	lastSeen := time.Now().UTC().Truncate(time.Second)
	if err := db.RecordStatus(deviceID, true, &lastSeen); err != nil {
		t.Fatalf("RecordStatus() error = %v", err)
	}

	statuses, err := db.GetLatestStatuses()
	if err != nil {
		t.Fatalf("GetLatestStatuses() error = %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("len(statuses) = %d, want 1", len(statuses))
	}
	if !statuses[0].Online {
		t.Error("Online = false, want true")
	}
	if statuses[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC = %q, want aa:bb:cc:dd:ee:ff", statuses[0].MAC)
	}
	if statuses[0].Group != "storage" {
		t.Errorf("Group = %q, want storage", statuses[0].Group)
	}
	if statuses[0].LastSeen.IsZero() {
		t.Error("LastSeen is zero, want recorded timestamp")
	}
}

func TestGetDeviceHistoryReturnsStatusChanges(t *testing.T) {
	db := newTestDB(t)

	deviceID, err := db.UpsertDevice("NAS", "192.168.1.100", "", "storage")
	if err != nil {
		t.Fatalf("UpsertDevice() error = %v", err)
	}

	baseTime := time.Now().UTC().Add(-time.Hour)
	for i, online := range []bool{false, false, true, true, false} {
		if err := recordStatusAt(db, deviceID, online, baseTime.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("recordStatusAt(%v) error = %v", online, err)
		}
	}

	history, err := db.GetDeviceHistory("192.168.1.100", 10)
	if err != nil {
		t.Fatalf("GetDeviceHistory() error = %v", err)
	}

	if len(history) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(history))
	}
	want := []bool{false, true, false}
	for i, status := range history {
		if status.Online != want[i] {
			t.Errorf("history[%d].Online = %v, want %v", i, status.Online, want[i])
		}
	}
}

func TestGetStatusesSince(t *testing.T) {
	db := newTestDB(t)

	deviceID, err := db.UpsertDevice("Router", "192.168.1.1", "", "network")
	if err != nil {
		t.Fatalf("UpsertDevice() error = %v", err)
	}

	baseTime := time.Now().UTC().Add(-2 * time.Hour)
	if err := recordStatusAt(db, deviceID, false, baseTime); err != nil {
		t.Fatalf("recordStatusAt(false) error = %v", err)
	}
	if err := recordStatusAt(db, deviceID, true, baseTime.Add(time.Hour)); err != nil {
		t.Fatalf("recordStatusAt(true) error = %v", err)
	}

	statuses, err := db.GetStatusesSince(baseTime.Add(30 * time.Minute))
	if err != nil {
		t.Fatalf("GetStatusesSince() error = %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("len(statuses) = %d, want 1", len(statuses))
	}
	if !statuses[0].Online {
		t.Error("Online = false, want true")
	}
}

func recordStatusAt(db *DB, deviceID int64, online bool, checkedAt time.Time) error {
	_, err := db.conn.Exec(
		`INSERT INTO device_status (device_id, online, checked_at) VALUES (?, ?, ?)`,
		deviceID,
		online,
		checkedAt,
	)
	return err
}
