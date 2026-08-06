package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// AuditEntry is one row of the append-only, hash-chained audit log.
type AuditEntry struct {
	ID         int64  `json:"id"`
	TS         int64  `json:"ts"`
	ActorID    string `json:"actorId"`
	ActorEmail string `json:"actorEmail"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Detail     string `json:"detail"`
	IP         string `json:"ip"`
	UA         string `json:"ua"`
	PrevHash   string `json:"-"`
	Hash       string `json:"hash"`
}

func auditHash(prev string, e *AuditEntry) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s|%s|%s",
		prev, e.TS, e.ActorID, e.Action, e.Target, e.Detail)))
	return hex.EncodeToString(h[:])
}

func (s *Store) lastAuditHash() (string, error) {
	var h string
	err := s.db.QueryRow(`SELECT hash FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return h, err
}

// AppendAudit links a new entry into the hash chain and inserts it. Callers must
// serialize calls (the audit.Logger holds a mutex) to keep the chain coherent.
func (s *Store) AppendAudit(e *AuditEntry) error {
	prev, err := s.lastAuditHash()
	if err != nil {
		return err
	}
	e.PrevHash = prev
	e.Hash = auditHash(prev, e)
	_, err = s.db.Exec(
		`INSERT INTO audit_log (ts, actor_id, actor_email, action, target, detail, ip, ua, prev_hash, hash)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TS, e.ActorID, e.ActorEmail, e.Action, e.Target, e.Detail, e.IP, e.UA, e.PrevHash, e.Hash)
	return err
}

// AuditFilter narrows an audit query.
type AuditFilter struct {
	Action string
	Actor  string
	From   int64
	To     int64
	Limit  int
	Offset int
	// BeforeID is the keyset cursor: when >0, only rows with id < BeforeID are
	// returned (entries are ordered id DESC). Preferred over Offset, which is
	// retained for the current web viewer until it migrates (Phase 5).
	BeforeID int64
}

// ListAudit returns audit entries newest-first matching the filter.
func (s *Store) ListAudit(f AuditFilter) ([]*AuditEntry, error) {
	q := `SELECT id, ts, actor_id, actor_email, action, target, detail, ip, ua FROM audit_log WHERE 1=1`
	var args []any
	if f.Action != "" {
		q += " AND action LIKE ?"
		args = append(args, f.Action+"%")
	}
	if f.Actor != "" {
		q += " AND (actor_email LIKE ? OR actor_id = ?)"
		args = append(args, "%"+f.Actor+"%", f.Actor)
	}
	if f.From > 0 {
		q += " AND ts >= ?"
		args = append(args, f.From)
	}
	if f.To > 0 {
		q += " AND ts <= ?"
		args = append(args, f.To)
	}
	if f.BeforeID > 0 {
		q += " AND id < ?"
		args = append(args, f.BeforeID)
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q += " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, f.Offset)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*AuditEntry, 0, limit)
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TS, &e.ActorID, &e.ActorEmail, &e.Action, &e.Target, &e.Detail, &e.IP, &e.UA); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// DistinctAuditActions returns the set of action keys present, for filter UIs.
func (s *Store) DistinctAuditActions() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT action FROM audit_log ORDER BY action`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// VerifyAuditChain walks the chain and returns the id of the first tampered row,
// or 0 if intact.
func (s *Store) VerifyAuditChain() (int64, error) {
	rows, err := s.db.Query(`SELECT id, ts, actor_id, action, target, detail, prev_hash, hash FROM audit_log ORDER BY id ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	prev := ""
	for rows.Next() {
		var e AuditEntry
		var ph, hh string
		if err := rows.Scan(&e.ID, &e.TS, &e.ActorID, &e.Action, &e.Target, &e.Detail, &ph, &hh); err != nil {
			return 0, err
		}
		if ph != prev || hh != auditHash(prev, &e) || strings.TrimSpace(hh) == "" {
			return e.ID, nil
		}
		prev = hh
	}
	return 0, rows.Err()
}
