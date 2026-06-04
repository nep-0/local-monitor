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
	ChangedAt time.Time
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

	CREATE TABLE IF NOT EXISTS device_transitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id INTEGER NOT NULL,
		online BOOLEAN NOT NULL,
		last_seen DATETIME,
		changed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (device_id) REFERENCES devices(id)
	);

	CREATE INDEX IF NOT EXISTS idx_device_transitions_device_changed_id ON device_transitions(device_id, changed_at, id);
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
	INSERT INTO device_transitions (device_id, online, last_seen, changed_at)
	SELECT ?, ?, ?, CURRENT_TIMESTAMP
	WHERE NOT EXISTS (
		SELECT 1
		FROM (
			SELECT online
			FROM device_transitions
			WHERE device_id = ?
			ORDER BY changed_at DESC, id DESC
			LIMIT 1
		) latest
		WHERE latest.online = ?
	)
	`
	_, err := db.conn.Exec(query, deviceID, online, lastSeen, deviceID, online)
	return err
}

func (db *DB) GetLatestStatuses() ([]DeviceStatus, error) {
	query := `
	SELECT d.id, d.name, d.ip, d.mac, d.[groups],
		dt.online, dt.last_seen, dt.changed_at
	FROM devices d
	LEFT JOIN (
		SELECT device_id, online, last_seen, changed_at
		FROM device_transitions current
		WHERE id = (
			SELECT id
			FROM device_transitions latest
			WHERE latest.device_id = current.device_id
			ORDER BY changed_at DESC, id DESC
			LIMIT 1
		)
	) dt ON d.id = dt.device_id
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
		var lastSeen, changedAt sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.IP, &mac, &group,
			&online, &lastSeen, &changedAt)
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
		if changedAt.Valid {
			s.ChangedAt = changedAt.Time
		}

		statuses = append(statuses, s)
	}

	return statuses, rows.Err()
}

func (db *DB) GetDeviceHistory(deviceIP string, limit int) ([]DeviceStatus, error) {
	query := `
	SELECT d.id, d.name, d.ip, d.mac, d.[groups],
		dt.online, dt.last_seen, dt.changed_at
	FROM device_transitions dt
	JOIN devices d ON d.id = dt.device_id
	WHERE d.ip = ?
	ORDER BY dt.changed_at DESC, dt.id DESC
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
		var lastSeen, changedAt sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.IP, &mac, &group,
			&s.Online, &lastSeen, &changedAt)
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
		if changedAt.Valid {
			s.ChangedAt = changedAt.Time
		}

		history = append(history, s)
	}

	return history, rows.Err()
}

func (db *DB) GetStatusesSince(since time.Time) ([]DeviceStatus, error) {
	query := `
	WITH timeline AS (
		SELECT dt.device_id, dt.online, dt.last_seen, dt.changed_at
		FROM devices d
		JOIN device_transitions dt ON dt.id = (
			SELECT id
			FROM device_transitions
			WHERE device_id = d.id AND changed_at < ?
			ORDER BY changed_at DESC, id DESC
			LIMIT 1
		)

		UNION ALL

		SELECT device_id, online, last_seen, changed_at
		FROM device_transitions
		WHERE changed_at >= ?
	)
	SELECT d.id, d.name, d.ip, d.mac, d.[groups],
		t.online, t.last_seen, t.changed_at
	FROM timeline t
	JOIN devices d ON d.id = t.device_id
	ORDER BY d.name, t.changed_at ASC
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
		var lastSeen, changedAt sql.NullTime

		err := rows.Scan(&s.ID, &s.Name, &s.IP, &mac, &group,
			&s.Online, &lastSeen, &changedAt)
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
		if changedAt.Valid {
			s.ChangedAt = changedAt.Time
		}

		statuses = append(statuses, s)
	}

	return statuses, rows.Err()
}

func (db *DB) CleanupOldRecords(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	transitionResult, err := db.conn.Exec(`DELETE FROM device_transitions WHERE changed_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	transitionRows, err := transitionResult.RowsAffected()
	if err != nil {
		return 0, err
	}

	return transitionRows, nil
}
