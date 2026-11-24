// Package sqlite stores alert firings recorded by the webhook receiver.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gaurangomkar/noisefloor/internal/model"
	"github.com/gaurangomkar/noisefloor/internal/source"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when an acknowledgement does not match a stored firing.
var ErrNotFound = errors.New("alert instance not found")

// Store is a SQLite file of alert instances.
type Store struct {
	db *sql.DB
}

// Open creates the file and schema if needed.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(abs),
		RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS events (
  fingerprint TEXT NOT NULL,
  alertname TEXT NOT NULL,
  starts_at TEXT NOT NULL,
  ends_at TEXT,
  status TEXT NOT NULL,
  labels_json TEXT NOT NULL,
  annotations_json TEXT NOT NULL,
  generator_url TEXT,
  receiver TEXT,
  source TEXT NOT NULL,
  acked_at TEXT,
  ack_known INTEGER NOT NULL DEFAULT 0,
  silenced INTEGER NOT NULL DEFAULT 0,
  inhibited INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (fingerprint, alertname, starts_at)
);
CREATE INDEX IF NOT EXISTS events_starts_at ON events (starts_at);
`)
	if err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	return nil
}

// Name implements source.Source.
func (s *Store) Name() string { return "sqlite" }

// Upsert writes events. A later resolved notification updates the open row
// instead of inserting a second firing.
func (s *Store) Upsert(ctx context.Context, events []model.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, ev := range events {
		if err := upsertOne(ctx, tx, ev); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func upsertOne(ctx context.Context, tx *sql.Tx, ev model.Event) error {
	if ev.StartsAt.IsZero() {
		return fmt.Errorf("sqlite: event %s has no starts_at", ev.Alertname)
	}
	if ev.Fingerprint == "" {
		ev.Fingerprint = model.Fingerprint(ev.Labels)
	}
	if ev.Alertname == "" {
		ev.Alertname = ev.Labels["alertname"]
	}
	if ev.Source == "" {
		ev.Source = "webhook"
	}
	labels, err := marshalMap(ev.Labels)
	if err != nil {
		return err
	}
	annotations, err := marshalMap(ev.Annotations)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO events (
  fingerprint, alertname, starts_at, ends_at, status, labels_json, annotations_json,
  generator_url, receiver, source, acked_at, ack_known, silenced, inhibited, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(fingerprint, alertname, starts_at) DO UPDATE SET
  ends_at = CASE
    WHEN excluded.status = 'resolved' AND excluded.ends_at IS NOT NULL THEN excluded.ends_at
    WHEN events.status = 'resolved' THEN events.ends_at
    ELSE events.ends_at
  END,
  status = CASE
    WHEN events.status = 'resolved' OR excluded.status = 'resolved' THEN 'resolved'
    ELSE excluded.status
  END,
  labels_json = excluded.labels_json,
  annotations_json = excluded.annotations_json,
  generator_url = CASE WHEN excluded.generator_url <> '' THEN excluded.generator_url ELSE events.generator_url END,
  receiver = CASE WHEN excluded.receiver <> '' THEN excluded.receiver ELSE events.receiver END,
  source = excluded.source,
  acked_at = CASE
    WHEN excluded.ack_known = 1 AND excluded.acked_at IS NOT NULL AND (events.acked_at IS NULL OR excluded.acked_at < events.acked_at) THEN excluded.acked_at
    ELSE events.acked_at
  END,
  ack_known = CASE WHEN events.ack_known = 1 OR excluded.ack_known = 1 THEN 1 ELSE 0 END,
  silenced = CASE WHEN events.silenced = 1 OR excluded.silenced = 1 THEN 1 ELSE 0 END,
  inhibited = CASE WHEN events.inhibited = 1 OR excluded.inhibited = 1 THEN 1 ELSE 0 END,
  updated_at = excluded.updated_at
`, ev.Fingerprint, ev.Alertname, formatTime(ev.StartsAt), nullTime(ev.EndsAt),
		blank(ev.Status, model.StatusFiring), labels, annotations, ev.GeneratorURL, ev.Receiver,
		ev.Source, nullTime(ev.AckedAt), boolInt(ev.AckKnown), boolInt(ev.Silenced), boolInt(ev.Inhibited),
		formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("sqlite upsert: %w", err)
	}
	return nil
}

// Ack records an acknowledgement. startsAt may be zero to update the latest
// instance of the fingerprint.
func (s *Store) Ack(ctx context.Context, fingerprint string, startsAt, ackedAt time.Time) error {
	if fingerprint == "" {
		return fmt.Errorf("fingerprint is empty")
	}
	if ackedAt.IsZero() {
		ackedAt = time.Now()
	}
	var res sql.Result
	var err error
	if startsAt.IsZero() {
		res, err = s.db.ExecContext(ctx, `
UPDATE events SET acked_at = ?, ack_known = 1, updated_at = ?
WHERE fingerprint = ? AND starts_at = (
  SELECT starts_at FROM events WHERE fingerprint = ? ORDER BY starts_at DESC LIMIT 1
)`, formatTime(ackedAt.UTC()), formatTime(time.Now().UTC()), fingerprint, fingerprint)
	} else {
		res, err = s.db.ExecContext(ctx, `
UPDATE events SET acked_at = ?, ack_known = 1, updated_at = ?
WHERE fingerprint = ? AND starts_at = ?`, formatTime(ackedAt.UTC()), formatTime(time.Now().UTC()), fingerprint, formatTime(startsAt.UTC()))
	}
	if err != nil {
		return fmt.Errorf("sqlite ack: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Fetch implements source.Source. Instances that overlap the window are returned.
func (s *Store) Fetch(ctx context.Context, window model.Window) (source.Result, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT fingerprint, alertname, starts_at, ends_at, status, labels_json, annotations_json,
       generator_url, receiver, source, acked_at, ack_known, silenced, inhibited
FROM events
WHERE starts_at < ?
ORDER BY starts_at, alertname, fingerprint
`, formatTime(window.End.UTC()))
	if err != nil {
		return source.Result{}, fmt.Errorf("sqlite query: %w", err)
	}
	defer rows.Close()
	now := window.End
	if now.IsZero() {
		now = time.Now()
	}
	var events []model.Event
	for rows.Next() {
		ev, err := scanEvent(rows, now)
		if err != nil {
			return source.Result{}, err
		}
		if ev.Overlaps(window) {
			events = append(events, ev)
		}
	}
	if err := rows.Err(); err != nil {
		return source.Result{}, err
	}
	return source.Result{Events: events}, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(sc scanner, now time.Time) (model.Event, error) {
	var (
		ev                                       model.Event
		starts, ends, acked, labels, annotations sql.NullString
		ackKnown, silenced, inhibited            int
	)
	err := sc.Scan(&ev.Fingerprint, &ev.Alertname, &starts, &ends, &ev.Status, &labels, &annotations,
		&ev.GeneratorURL, &ev.Receiver, &ev.Source, &acked, &ackKnown, &silenced, &inhibited)
	if err != nil {
		return model.Event{}, err
	}
	ev.StartsAt, err = parseStored(starts.String)
	if err != nil {
		return model.Event{}, err
	}
	if ends.Valid {
		ev.EndsAt, err = parseStored(ends.String)
		if err != nil {
			return model.Event{}, err
		}
	}
	if acked.Valid && acked.String != "" {
		ev.AckedAt, err = parseStored(acked.String)
		if err != nil {
			return model.Event{}, err
		}
	}
	ev.AckKnown = ackKnown == 1
	ev.Silenced = silenced == 1
	ev.Inhibited = inhibited == 1
	if labels.String != "" {
		if err := json.Unmarshal([]byte(labels.String), &ev.Labels); err != nil {
			return model.Event{}, fmt.Errorf("labels: %w", err)
		}
	}
	if annotations.String != "" {
		if err := json.Unmarshal([]byte(annotations.String), &ev.Annotations); err != nil {
			return model.Event{}, fmt.Errorf("annotations: %w", err)
		}
	}
	return model.Normalize(ev, now), nil
}

func marshalMap(m map[string]string) (string, error) {
	if m == nil {
		m = map[string]string{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return formatTime(t)
}

func parseStored(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("stored time %q: %w", s, err)
	}
	return t.UTC(), nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func blank(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
