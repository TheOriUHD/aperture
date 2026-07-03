package store

import (
	"strings"
	"time"
)

// Webhook is an outbound integration endpoint: matching events are POSTed to it
// as signed JSON. Events empty = all event types.
type Webhook struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Secret    string   `json:"secret,omitempty"`
	Enabled   bool     `json:"enabled"`
	CreatedAt int64    `json:"createdAt"`
}

// WebhookDelivery records one delivery attempt (for the audit/status view).
type WebhookDelivery struct {
	ID        string `json:"id"`
	WebhookID string `json:"webhookId"`
	Event     string `json:"event"`
	Status    string `json:"status"`
	Code      int    `json:"code"`
	Detail    string `json:"detail,omitempty"`
	TS        int64  `json:"ts"`
}

func csv(v []string) string { return strings.Join(v, ",") }
func uncsv(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func (s *Store) CreateWebhook(wh *Webhook) error {
	if wh.ID == "" {
		wh.ID = NewID()
	}
	wh.CreatedAt = time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT INTO webhooks (id, name, url, events, secret, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		wh.ID, wh.Name, wh.URL, csv(wh.Events), wh.Secret, boolInt(wh.Enabled), wh.CreatedAt)
	return err
}

func (s *Store) UpdateWebhook(wh *Webhook) error {
	return affected(s.db.Exec(
		`UPDATE webhooks SET name = ?, url = ?, events = ?, secret = ?, enabled = ? WHERE id = ?`,
		wh.Name, wh.URL, csv(wh.Events), wh.Secret, boolInt(wh.Enabled), wh.ID))
}

func (s *Store) DeleteWebhook(id string) error {
	_, err := s.db.Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

func (s *Store) GetWebhook(id string) (*Webhook, error) {
	return scanWebhook(s.db.QueryRow(
		`SELECT id, name, url, events, secret, enabled, created_at FROM webhooks WHERE id = ?`, id))
}

func (s *Store) ListWebhooks() ([]Webhook, error) {
	rows, err := s.db.Query(`SELECT id, name, url, events, secret, enabled, created_at FROM webhooks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		wh, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *wh)
	}
	return out, rows.Err()
}

// ListEnabledWebhooks returns webhooks that should receive the given event type.
func (s *Store) ListEnabledWebhooks(eventType string) ([]Webhook, error) {
	all, err := s.ListWebhooks()
	if err != nil {
		return nil, err
	}
	var out []Webhook
	for _, wh := range all {
		if !wh.Enabled {
			continue
		}
		if len(wh.Events) == 0 || contains(wh.Events, eventType) {
			out = append(out, wh)
		}
	}
	return out, nil
}

func (s *Store) RecordDelivery(d WebhookDelivery) {
	if d.ID == "" {
		d.ID = NewID()
	}
	_, _ = s.db.Exec(
		`INSERT INTO webhook_deliveries (id, webhook_id, event, status, code, detail, ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.WebhookID, d.Event, d.Status, d.Code, d.Detail, d.TS)
	// Keep only the most recent 50 deliveries per webhook.
	_, _ = s.db.Exec(
		`DELETE FROM webhook_deliveries WHERE webhook_id = ? AND id NOT IN (
		   SELECT id FROM webhook_deliveries WHERE webhook_id = ? ORDER BY ts DESC LIMIT 50)`,
		d.WebhookID, d.WebhookID)
}

func (s *Store) ListDeliveries(webhookID string, limit int) ([]WebhookDelivery, error) {
	rows, err := s.db.Query(
		`SELECT id, webhook_id, event, status, code, detail, ts FROM webhook_deliveries
		 WHERE webhook_id = ? ORDER BY ts DESC LIMIT ?`, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &d.Status, &d.Code, &d.Detail, &d.TS); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanWebhook(row rowScanner) (*Webhook, error) {
	var wh Webhook
	var events string
	var enabled int
	if err := row.Scan(&wh.ID, &wh.Name, &wh.URL, &events, &wh.Secret, &enabled, &wh.CreatedAt); err != nil {
		return nil, err
	}
	wh.Events = uncsv(events)
	wh.Enabled = enabled != 0
	return &wh, nil
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}
