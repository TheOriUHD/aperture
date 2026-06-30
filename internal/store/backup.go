package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// configTables are the tables included in a configuration backup, in
// parent-before-child order so inserts satisfy foreign keys (deletes run in
// reverse). Runtime/data tables — segments, events, weather_samples, audit_log,
// notify_log, shares, export_jobs, push_subscriptions — are deliberately
// excluded: a backup is configuration, not a footage snapshot.
var configTables = []string{
	"users",
	"cameras",
	"settings",
	"schedules",
	"notify_channels",
	"notify_rules",
	"user_preferences",
	"user_camera_acl",
	"webauthn_credentials",
}

// ConfigBackup is a portable snapshot of configuration (no footage).
type ConfigBackup struct {
	Version       string                     `json:"version"`
	SchemaVersion int                        `json:"schemaVersion"`
	Timestamp     int64                      `json:"timestamp"`
	Tables        map[string]json.RawMessage `json:"tables"`
}

// SchemaVersion returns the highest applied migration version.
func (s *Store) SchemaVersion() (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

// ExportConfig dumps the configuration tables into a ConfigBackup.
func (s *Store) ExportConfig(appVersion string, nowUnix int64) (*ConfigBackup, error) {
	sv, err := s.SchemaVersion()
	if err != nil {
		return nil, err
	}
	b := &ConfigBackup{Version: appVersion, SchemaVersion: sv, Timestamp: nowUnix, Tables: map[string]json.RawMessage{}}
	for _, t := range configTables {
		raw, err := s.dumpTable(t)
		if err != nil {
			return nil, fmt.Errorf("dump %s: %w", t, err)
		}
		b.Tables[t] = raw
	}
	return b, nil
}

func (s *Store) dumpTable(table string) (json.RawMessage, error) {
	rows, err := s.db.Query("SELECT * FROM " + table) // table is a fixed constant, never user input
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]any, len(cols))
		for i, c := range cols {
			v := vals[i]
			if bb, ok := v.([]byte); ok {
				v = string(bb)
			}
			m[c] = v
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// ImportConfig replaces all configuration tables from a backup in one
// transaction. It refuses a backup created on a newer schema than this server.
func (s *Store) ImportConfig(b *ConfigBackup) error {
	cur, err := s.SchemaVersion()
	if err != nil {
		return err
	}
	if b.SchemaVersion > cur {
		return fmt.Errorf("backup is from a newer version (schema v%d > v%d); upgrade this server first", b.SchemaVersion, cur)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck — no-op after a successful Commit
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	// Clear children-first.
	for i := len(configTables) - 1; i >= 0; i-- {
		if _, err := tx.Exec("DELETE FROM " + configTables[i]); err != nil {
			return fmt.Errorf("clear %s: %w", configTables[i], err)
		}
	}
	// Insert parents-first.
	for _, t := range configTables {
		raw, ok := b.Tables[t]
		if !ok {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var rowsData []map[string]any
		if err := dec.Decode(&rowsData); err != nil {
			return fmt.Errorf("parse %s: %w", t, err)
		}
		for _, row := range rowsData {
			if err := insertRow(tx, t, row); err != nil {
				return fmt.Errorf("restore %s: %w", t, err)
			}
		}
	}
	return tx.Commit()
}

func insertRow(tx *sql.Tx, table string, row map[string]any) error {
	cols := make([]string, 0, len(row))
	for k := range row {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	ph := make([]string, len(cols))
	vals := make([]any, len(cols))
	for i, c := range cols {
		ph[i] = "?"
		vals[i] = coerce(row[c])
	}
	q := "INSERT INTO " + table + " (" + strings.Join(cols, ",") + ") VALUES (" + strings.Join(ph, ",") + ")"
	_, err := tx.Exec(q, vals...)
	return err
}

// coerce turns a json.Number back into a concrete int64/float64 so integer
// columns (e.g. unix-ms timestamps) round-trip exactly rather than via float.
func coerce(v any) any {
	n, ok := v.(json.Number)
	if !ok {
		return v
	}
	if iv, err := n.Int64(); err == nil {
		return iv
	}
	if fv, err := n.Float64(); err == nil {
		return fv
	}
	return n.String()
}
