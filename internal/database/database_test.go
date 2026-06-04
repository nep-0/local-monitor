package database

import (
	"database/sql"
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
	if !statuses[0].ChangedAt.IsZero() {
		t.Errorf("ChangedAt = %v, want zero time", statuses[0].ChangedAt)
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

func TestRecordStatusOnlyStoresTransitions(t *testing.T) {
	db := newTestDB(t)

	deviceID, err := db.UpsertDevice("Router", "192.168.1.1", "", "network")
	if err != nil {
		t.Fatalf("UpsertDevice() error = %v", err)
	}

	for _, online := range []bool{false, false, true, true, false} {
		if err := db.RecordStatus(deviceID, online, nil); err != nil {
			t.Fatalf("RecordStatus(%v) error = %v", online, err)
		}
	}

	var transitionCount int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM device_transitions WHERE device_id = ?`, deviceID).Scan(&transitionCount); err != nil {
		t.Fatalf("transition count query error = %v", err)
	}
	if transitionCount != 3 {
		t.Fatalf("transitionCount = %d, want 3", transitionCount)
	}

	var rawTableName string
	err = db.conn.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'device_status'`).Scan(&rawTableName)
	if err != sql.ErrNoRows {
		t.Fatalf("device_status table query error = %v, want sql.ErrNoRows", err)
	}
}

func TestGetDeviceHistoryReturnsStatusChanges(t *testing.T) {
	db := newTestDB(t)

	deviceID, err := db.UpsertDevice("NAS", "192.168.1.100", "", "storage")
	if err != nil {
		t.Fatalf("UpsertDevice() error = %v", err)
	}

	baseTime := time.Now().UTC().Add(-time.Hour)
	sequence := []bool{false, false, true, true, false}
	for i, online := range sequence {
		if i > 0 && online == sequence[i-1] {
			continue
		}
		if err := recordTransitionAt(db, deviceID, online, baseTime.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("recordTransitionAt(%v) error = %v", online, err)
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
	if err := recordTransitionAt(db, deviceID, false, baseTime); err != nil {
		t.Fatalf("recordTransitionAt(false) error = %v", err)
	}
	if err := recordTransitionAt(db, deviceID, true, baseTime.Add(time.Hour)); err != nil {
		t.Fatalf("recordTransitionAt(true) error = %v", err)
	}

	statuses, err := db.GetStatusesSince(baseTime.Add(30 * time.Minute))
	if err != nil {
		t.Fatalf("GetStatusesSince() error = %v", err)
	}

	if len(statuses) != 2 {
		t.Fatalf("len(statuses) = %d, want 2", len(statuses))
	}
	if statuses[0].Online {
		t.Error("statuses[0].Online = true, want false for pre-window state")
	}
	if !statuses[1].Online {
		t.Error("statuses[1].Online = false, want true for in-window transition")
	}
}

func recordTransitionAt(db *DB, deviceID int64, online bool, changedAt time.Time) error {
	_, err := db.conn.Exec(
		`INSERT INTO device_transitions (device_id, online, changed_at) VALUES (?, ?, ?)`,
		deviceID,
		online,
		changedAt,
	)
	return err
}
