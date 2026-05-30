package database

import (
	"database/sql"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

type DeviceStatus struct {
	ID        int64
	Name      string
	IP        string
	MAC       string
	Group     string
	Online    bool
	LastSeen  time.Time
	CheckedAt time.Time
}

type DB struct {
	conn *sql.DB
}

func New(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS devices (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		ip TEXT NOT NULL UNIQUE,
		mac TEXT,
		[groups] TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS device_status (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id INTEGER NOT NULL,
		online BOOLEAN NOT NULL,
		last_seen DATETIME,
		checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (device_id) REFERENCES devices(id)
	);

	CREATE INDEX IF NOT EXISTS idx_device_status_device_id ON device_status(device_id);
	CREATE INDEX IF NOT EXISTS idx_device_status_checked_at ON device_status(checked_at);
	`
	_, err := db.conn.Exec(query)
	return err
}

func (db *DB) UpsertDevice(name, ip, mac, group string) (int64, error) {
	query := `
	INSERT INTO devices (name, ip, mac, [groups])
	VALUES (?, ?, ?, ?)
	ON CONFLICT(ip) DO UPDATE SET
		name = excluded.name,
		mac = excluded.mac,
		[groups] = excluded.[groups]
	`
	result, err := db.conn.Exec(query, name, ip, mac, group)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (db *DB) RecordStatus(deviceID int64, online bool, lastSeen *time.Time) error {
	query := `
	INSERT INTO device_status (device_id, online, last_seen, checked_at)
	VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	`
	_, err := db.conn.Exec(query, deviceID, online, lastSeen)
	return err
}

func (db *DB) GetLatestStatuses() ([]DeviceStatus, error) {
	query := `
	SELECT d.id, d.name, d.ip, d.mac, d.[groups],
		ds.online, ds.last_seen, ds.checked_at
	FROM devices d
	LEFT JOIN (
		SELECT device_id, online, last_seen, checked_at,
			ROW_NUMBER() OVER (PARTITION BY device_id ORDER BY checked_at DESC) as rn
		FROM device_status
	) ds ON d.id = ds.device_id AND ds.rn = 1
	ORDER BY d.name
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []DeviceStatus
	for rows.Next() {
		var s DeviceStatus
		var lastSeen, checkedAt sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.IP, &s.MAC, &s.Group,
			&s.Online, &lastSeen, &checkedAt)
		if err != nil {
			return nil, err
		}

		if lastSeen.Valid {
			s.LastSeen = lastSeen.Time
		}
		if checkedAt.Valid {
			s.CheckedAt = checkedAt.Time
		}

		statuses = append(statuses, s)
	}

	return statuses, rows.Err()
}

func (db *DB) GetDeviceHistory(deviceIP string, limit int) ([]DeviceStatus, error) {
	query := `
	SELECT d.id, d.name, d.ip, d.mac, d.[groups],
		ds.online, ds.last_seen, ds.checked_at
	FROM device_status ds
	JOIN devices d ON d.id = ds.device_id
	WHERE d.ip = ?
	ORDER BY ds.checked_at DESC
	LIMIT ?
	`

	rows, err := db.conn.Query(query, deviceIP, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []DeviceStatus
	for rows.Next() {
		var s DeviceStatus
		var lastSeen, checkedAt sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.IP, &s.MAC, &s.Group,
			&s.Online, &lastSeen, &checkedAt)
		if err != nil {
			return nil, err
		}

		if lastSeen.Valid {
			s.LastSeen = lastSeen.Time
		}
		if checkedAt.Valid {
			s.CheckedAt = checkedAt.Time
		}

		history = append(history, s)
	}

	return history, rows.Err()
}

func (db *DB) CleanupOldRecords(olderThan time.Duration) (int64, error) {
	query := `DELETE FROM device_status WHERE checked_at < ?`
	result, err := db.conn.Exec(query, time.Now().Add(-olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
