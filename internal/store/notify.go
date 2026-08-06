package store

import (
	"database/sql"
	"encoding/json"
)

// --- channels ------------------------------------------------------------

type NotifyChannel struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Config    string `json:"-"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Store) CreateChannel(c *NotifyChannel) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	_, err := s.db.Exec(
		`INSERT INTO notify_channels (id, user_id, type, name, config, enabled, created_at) VALUES (?,?,?,?,?,?,?)`,
		c.ID, c.UserID, c.Type, c.Name, c.Config, boolInt(c.Enabled), c.CreatedAt)
	return err
}

func (s *Store) UpdateChannel(c *NotifyChannel) error {
	return affected(s.db.Exec(
		`UPDATE notify_channels SET name=?, config=?, enabled=? WHERE id=?`,
		c.Name, c.Config, boolInt(c.Enabled), c.ID))
}

func (s *Store) GetChannel(id string) (*NotifyChannel, error) {
	return scanChannel(s.db.QueryRow(
		`SELECT id, user_id, type, name, config, enabled, created_at FROM notify_channels WHERE id=?`, id))
}

func (s *Store) ListChannelsByUser(userID string) ([]*NotifyChannel, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, type, name, config, enabled, created_at FROM notify_channels WHERE user_id=? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*NotifyChannel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteChannel(id string) error {
	_, err := s.db.Exec(`DELETE FROM notify_channels WHERE id=?`, id)
	return err
}

func scanChannel(row rowScanner) (*NotifyChannel, error) {
	var c NotifyChannel
	var en int
	if err := row.Scan(&c.ID, &c.UserID, &c.Type, &c.Name, &c.Config, &en, &c.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Enabled = en != 0
	return &c, nil
}

// --- rules ---------------------------------------------------------------

// RuleConditions gates a rule on the environmental context at the event. Empty
// fields mean "don't care".
type RuleConditions struct {
	TimeOfDay  string   `json:"timeOfDay"`  // "" | "day" | "night"
	OnlyPrecip bool     `json:"onlyPrecip"` // only when precipitation > 0
	Objects    []string `json:"objects"`    // require one of these detected object labels
}

type NotifyRule struct {
	ID         string         `json:"id"`
	UserID     string         `json:"userId"`
	Name       string         `json:"name"`
	Enabled    bool           `json:"enabled"`
	Cameras    []string       `json:"cameras"`
	EventTypes []string       `json:"eventTypes"`
	Channels   []string       `json:"channels"`
	QuietStart int            `json:"quietStart"`
	QuietEnd   int            `json:"quietEnd"`
	CooldownS  int            `json:"cooldownSec"`
	Conditions RuleConditions `json:"conditions"`
	CreatedAt  int64          `json:"createdAt"`
}

func (c RuleConditions) marshal() string {
	// Persist when ANY condition is set. The Objects check was missing, so a rule
	// whose only condition was object labels (e.g. "person") silently lost it on
	// save (3.4) — the engine reads Conditions.Objects, so it must round-trip.
	if c.TimeOfDay == "" && !c.OnlyPrecip && len(c.Objects) == 0 {
		return ""
	}
	b, _ := json.Marshal(c)
	return string(b)
}

func (s *Store) CreateRule(r *NotifyRule) error {
	if r.ID == "" {
		r.ID = NewID()
	}
	_, err := s.db.Exec(
		`INSERT INTO notify_rules (id, user_id, name, enabled, cameras, event_types, channels, quiet_start, quiet_end, cooldown_sec, conditions, created_at)
         VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.UserID, r.Name, boolInt(r.Enabled), jsonArr(r.Cameras), jsonArr(r.EventTypes),
		jsonArr(r.Channels), r.QuietStart, r.QuietEnd, r.CooldownS, r.Conditions.marshal(), r.CreatedAt)
	return err
}

func (s *Store) UpdateRule(r *NotifyRule) error {
	return affected(s.db.Exec(
		`UPDATE notify_rules SET name=?, enabled=?, cameras=?, event_types=?, channels=?, quiet_start=?, quiet_end=?, cooldown_sec=?, conditions=? WHERE id=?`,
		r.Name, boolInt(r.Enabled), jsonArr(r.Cameras), jsonArr(r.EventTypes), jsonArr(r.Channels),
		r.QuietStart, r.QuietEnd, r.CooldownS, r.Conditions.marshal(), r.ID))
}

func (s *Store) GetRule(id string) (*NotifyRule, error) {
	return scanRule(s.db.QueryRow(ruleSelect+` WHERE id=?`, id))
}

func (s *Store) ListRulesByUser(userID string) ([]*NotifyRule, error) {
	return collectRules(s.db.Query(ruleSelect+` WHERE user_id=? ORDER BY created_at`, userID))
}

func (s *Store) ListEnabledRules() ([]*NotifyRule, error) {
	return collectRules(s.db.Query(ruleSelect + ` WHERE enabled=1`))
}

func (s *Store) DeleteRule(id string) error {
	_, err := s.db.Exec(`DELETE FROM notify_rules WHERE id=?`, id)
	return err
}

const ruleSelect = `SELECT id, user_id, name, enabled, cameras, event_types, channels, quiet_start, quiet_end, cooldown_sec, conditions, created_at FROM notify_rules`

func scanRule(row rowScanner) (*NotifyRule, error) {
	var r NotifyRule
	var en int
	var cams, types, chans, conds string
	if err := row.Scan(&r.ID, &r.UserID, &r.Name, &en, &cams, &types, &chans,
		&r.QuietStart, &r.QuietEnd, &r.CooldownS, &conds, &r.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.Enabled = en != 0
	r.Cameras = parseArr(cams)
	r.EventTypes = parseArr(types)
	r.Channels = parseArr(chans)
	if conds != "" {
		_ = json.Unmarshal([]byte(conds), &r.Conditions)
	}
	return &r, nil
}

func collectRules(rows *sql.Rows, err error) ([]*NotifyRule, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*NotifyRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- push subscriptions --------------------------------------------------

type PushSubscription struct {
	ID        string
	UserID    string
	Endpoint  string
	P256dh    string
	Auth      string
	UA        string
	CreatedAt int64
}

func (s *Store) UpsertPushSub(p *PushSubscription) error {
	if p.ID == "" {
		p.ID = NewID()
	}
	_, err := s.db.Exec(
		`INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, ua, created_at) VALUES (?,?,?,?,?,?,?)
         ON CONFLICT(endpoint) DO UPDATE SET user_id=excluded.user_id, p256dh=excluded.p256dh, auth=excluded.auth, ua=excluded.ua`,
		p.ID, p.UserID, p.Endpoint, p.P256dh, p.Auth, p.UA, p.CreatedAt)
	return err
}

func (s *Store) ListPushSubsByUser(userID string) ([]*PushSubscription, error) {
	rows, err := s.db.Query(`SELECT id, user_id, endpoint, p256dh, auth, ua, created_at FROM push_subscriptions WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PushSubscription
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth, &p.UA, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) DeletePushSubByEndpoint(endpoint string) error {
	_, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE endpoint=?`, endpoint)
	return err
}

// --- cooldown log --------------------------------------------------------

func (s *Store) LastNotify(ruleID string) (int64, error) {
	var ts sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(ts) FROM notify_log WHERE rule_id=?`, ruleID).Scan(&ts)
	return ts.Int64, err
}

func (s *Store) RecordNotify(ruleID, eventKey string, ts int64) error {
	_, err := s.db.Exec(`INSERT INTO notify_log (rule_id, event_key, ts) VALUES (?,?,?)`, ruleID, eventKey, ts)
	return err
}

// --- helpers -------------------------------------------------------------

func jsonArr(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func parseArr(s string) []string {
	var v []string
	if s == "" {
		return nil
	}
	_ = json.Unmarshal([]byte(s), &v)
	return v
}
