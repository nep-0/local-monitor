package database

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
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
	RETURNING id
	`
	var id int64
	err := db.conn.QueryRow(query, name, ip, mac, group).Scan(&id)
	return id, err
}

func (db *DB) GetDeviceIDByIP(ip string) (int64, error) {
	var id int64
	err := db.conn.QueryRow(`SELECT id FROM devices WHERE ip = ?`, ip).Scan(&id)
	return id, err
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
		var mac, group sql.NullString
		var online sql.NullBool
		var lastSeen, checkedAt sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.IP, &mac, &group,
			&online, &lastSeen, &checkedAt)
		if err != nil {
			return nil, err
		}

		if mac.Valid {
			s.MAC = mac.String
		}
		if group.Valid {
			s.Group = group.String
		}
		if online.Valid {
			s.Online = online.Bool
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
		h.online, h.last_seen, h.checked_at
	FROM (
		SELECT ds.device_id, ds.online, ds.last_seen, ds.checked_at,
			LAG(ds.online) OVER (PARTITION BY ds.device_id ORDER BY ds.checked_at ASC, ds.id ASC) as previous_online
		FROM device_status ds
	) h
	JOIN devices d ON d.id = h.device_id
	WHERE d.ip = ?
		AND (h.previous_online IS NULL OR h.previous_online != h.online)
	ORDER BY h.checked_at DESC
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
		var mac, group sql.NullString
		var lastSeen, checkedAt sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.IP, &mac, &group,
			&s.Online, &lastSeen, &checkedAt)
		if err != nil {
			return nil, err
		}

		if mac.Valid {
			s.MAC = mac.String
		}
		if group.Valid {
			s.Group = group.String
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

func (db *DB) GetStatusesSince(since time.Time) ([]DeviceStatus, error) {
	query := `
	WITH timeline AS (
		SELECT device_id, online, last_seen, checked_at
		FROM (
			SELECT ds.device_id, ds.online, ds.last_seen, ds.checked_at,
				ROW_NUMBER() OVER (PARTITION BY ds.device_id ORDER BY ds.checked_at DESC, ds.id DESC) as rn
			FROM device_status ds
			WHERE ds.checked_at < ?
		)
		WHERE rn = 1

		UNION ALL

		SELECT device_id, online, last_seen, checked_at
		FROM (
			SELECT ds.device_id, ds.online, ds.last_seen, ds.checked_at,
				LAG(ds.online) OVER (PARTITION BY ds.device_id ORDER BY ds.checked_at ASC, ds.id ASC) as previous_online
			FROM device_status ds
		)
		WHERE checked_at >= ?
			AND (previous_online IS NULL OR previous_online != online)
	)
	SELECT d.id, d.name, d.ip, d.mac, d.[groups],
		t.online, t.last_seen, t.checked_at
	FROM timeline t
	JOIN devices d ON d.id = t.device_id
	ORDER BY d.name, t.checked_at ASC
	`

	rows, err := db.conn.Query(query, since, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var statuses []DeviceStatus
	for rows.Next() {
		var s DeviceStatus
		var mac, group sql.NullString
		var lastSeen, checkedAt sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.IP, &mac, &group,
			&s.Online, &lastSeen, &checkedAt)
		if err != nil {
			return nil, err
		}

		if mac.Valid {
			s.MAC = mac.String
		}
		if group.Valid {
			s.Group = group.String
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

func (db *DB) CleanupOldRecords(olderThan time.Duration) (int64, error) {
	query := `DELETE FROM device_status WHERE checked_at < ?`
	result, err := db.conn.Exec(query, time.Now().UTC().Add(-olderThan))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
