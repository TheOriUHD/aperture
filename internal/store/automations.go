package store

import (
	"encoding/json"
	"time"
)

// Automation is a when-this-then-that rule. Trigger/Conditions/Actions are kept
// as raw JSON so the UI owns their shape and the engine parses them.
type Automation struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	Trigger    json.RawMessage `json:"trigger"`
	Conditions json.RawMessage `json:"conditions"`
	Actions    json.RawMessage `json:"actions"`
	CooldownMs int64           `json:"cooldownMs"`
	LastFired  int64           `json:"lastFired"`
	CreatedAt  int64           `json:"createdAt"`
}

func rawOr(v json.RawMessage, def string) string {
	if len(v) == 0 {
		return def
	}
	return string(v)
}

func (s *Store) CreateAutomation(a *Automation) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	a.CreatedAt = time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT INTO automations (id, name, enabled, trigger, conditions, actions, cooldown_ms, last_fired, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		a.ID, a.Name, boolInt(a.Enabled), rawOr(a.Trigger, "{}"), rawOr(a.Conditions, "{}"),
		rawOr(a.Actions, "[]"), a.CooldownMs, a.CreatedAt)
	return err
}

func (s *Store) UpdateAutomation(a *Automation) error {
	return affected(s.db.Exec(
		`UPDATE automations SET name = ?, enabled = ?, trigger = ?, conditions = ?, actions = ?, cooldown_ms = ? WHERE id = ?`,
		a.Name, boolInt(a.Enabled), rawOr(a.Trigger, "{}"), rawOr(a.Conditions, "{}"),
		rawOr(a.Actions, "[]"), a.CooldownMs, a.ID))
}

func (s *Store) DeleteAutomation(id string) error {
	_, err := s.db.Exec(`DELETE FROM automations WHERE id = ?`, id)
	return err
}

func (s *Store) GetAutomation(id string) (*Automation, error) {
	return scanAutomation(s.db.QueryRow(automationSelect+` WHERE id = ?`, id))
}

func (s *Store) ListAutomations() ([]Automation, error) {
	return s.queryAutomations(automationSelect + ` ORDER BY created_at DESC`)
}

func (s *Store) ListEnabledAutomations() ([]Automation, error) {
	return s.queryAutomations(automationSelect + ` WHERE enabled = 1 ORDER BY created_at ASC`)
}

// TouchAutomationFired records the last-fired time for cooldown accounting.
func (s *Store) TouchAutomationFired(id string, ts int64) {
	_, _ = s.db.Exec(`UPDATE automations SET last_fired = ? WHERE id = ?`, ts, id)
}

const automationSelect = `SELECT id, name, enabled, trigger, conditions, actions, cooldown_ms, last_fired, created_at FROM automations`

func (s *Store) queryAutomations(q string, args ...any) ([]Automation, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func scanAutomation(row rowScanner) (*Automation, error) {
	var a Automation
	var enabled int
	var trig, cond, act string
	if err := row.Scan(&a.ID, &a.Name, &enabled, &trig, &cond, &act, &a.CooldownMs, &a.LastFired, &a.CreatedAt); err != nil {
		return nil, err
	}
	a.Enabled = enabled != 0
	a.Trigger = json.RawMessage(trig)
	a.Conditions = json.RawMessage(cond)
	a.Actions = json.RawMessage(act)
	return &a, nil
}
