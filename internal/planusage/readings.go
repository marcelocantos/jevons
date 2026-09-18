// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo
)

// MaxHistoryPoints is how many samples ride the snapshot / mux frame.
// A sparkline is tens of pixels wide; the store keeps every Refresh.
const MaxHistoryPoints = 80

// History persists vendor remaining samples (🎯T634). Not usage.db.
type History interface {
	Append(samples []Reading) error
	Series(provider, window string, resetsAt *time.Time) ([]HistoryPoint, error)
}

// Reading is one published remaining sample from a successful Refresh.
type Reading struct {
	Provider  string
	Window    string
	FetchedAt time.Time
	Remaining float64
	ResetsAt  *time.Time
}

// ReadingStore is an append-only SQLite History under StateDir.
type ReadingStore struct {
	db *sql.DB
}

const readingsSchema = `
CREATE TABLE IF NOT EXISTS plan_readings (
	id INTEGER PRIMARY KEY,
	provider TEXT NOT NULL,
	window TEXT NOT NULL,
	resets_key TEXT NOT NULL,
	fetched_at TEXT NOT NULL,
	remaining REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plan_readings_series
	ON plan_readings(provider, window, resets_key, fetched_at);
`

// DefaultReadingsPath is state_dir/plan-usage-readings.db — never usage.db.
func DefaultReadingsPath(stateDir string) string {
	return filepath.Join(stateDir, "plan-usage-readings.db")
}

// OpenReadingStore opens (or creates) the readings database at path.
// Use ":memory:" for hermetic tests. Isolates pass their own state_dir.
func OpenReadingStore(path string) (*ReadingStore, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("planusage readings: mkdir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("planusage readings: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("planusage readings: %s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(readingsSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("planusage readings: schema: %w", err)
	}
	if err := rebucketSeriesKeys(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("planusage readings: rebucket series keys: %w", err)
	}
	return &ReadingStore{db: db}, nil
}

// Close closes the database.
func (s *ReadingStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Append records published remaining samples from one successful Refresh.
func (s *ReadingStore) Append(samples []Reading) error {
	if s == nil || s.db == nil || len(samples) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO plan_readings(provider, window, resets_key, fetched_at, remaining)
		VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range samples {
		if r.FetchedAt.IsZero() {
			continue
		}
		if _, err := stmt.Exec(
			normKey(r.Provider),
			normKey(r.Window),
			resetsKey(r.ResetsAt),
			r.FetchedAt.UTC().Format(time.RFC3339),
			r.Remaining,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Series returns stored samples for one (provider, window, resets_at) period.
func (s *ReadingStore) Series(provider, window string, resetsAt *time.Time) ([]HistoryPoint, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT fetched_at, remaining FROM plan_readings
		 WHERE provider = ? AND window = ? AND resets_key = ?
		 ORDER BY fetched_at ASC, id ASC`,
		normKey(provider), normKey(window), resetsKey(resetsAt),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryPoint
	for rows.Next() {
		var at string
		var rem float64
		if err := rows.Scan(&at, &rem); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			continue
		}
		out = append(out, HistoryPoint{At: t, Remaining: rem})
	}
	return out, rows.Err()
}

// AttachHistory copies snap and fills each window's History from h.
// A nil History or a query error leaves History empty — never invented.
func AttachHistory(snap Snapshot, h History) Snapshot {
	if h == nil || len(snap.Backends) == 0 {
		return snap
	}
	out := snap
	out.Backends = make([]Backend, len(snap.Backends))
	copy(out.Backends, snap.Backends)
	for i := range out.Backends {
		be := &out.Backends[i]
		if len(be.Windows) == 0 {
			continue
		}
		windows := make([]Window, len(be.Windows))
		copy(windows, be.Windows)
		for j := range windows {
			pts, err := h.Series(be.Provider, windows[j].Name, windows[j].ResetsAt)
			if err != nil || len(pts) == 0 {
				windows[j].History = nil
				continue
			}
			windows[j].History = Downsample(pts, MaxHistoryPoints)
		}
		be.Windows = windows
	}
	return out
}

// Downsample keeps first and last and strides the middle. It does not invent
// a 100% point at the period start.
func Downsample(pts []HistoryPoint, max int) []HistoryPoint {
	if max < 2 || len(pts) <= max {
		return pts
	}
	out := make([]HistoryPoint, 0, max)
	out = append(out, pts[0])
	inner := max - 2
	span := len(pts) - 1
	for i := 1; i <= inner; i++ {
		idx := (i * span) / (inner + 1)
		if idx < 1 {
			idx = 1
		}
		if idx >= span {
			idx = span - 1
		}
		if out[len(out)-1].At.Equal(pts[idx].At) && out[len(out)-1].Remaining == pts[idx].Remaining {
			continue
		}
		out = append(out, pts[idx])
	}
	last := pts[len(pts)-1]
	if !out[len(out)-1].At.Equal(last.At) || out[len(out)-1].Remaining != last.Remaining {
		out = append(out, last)
	}
	return out
}

func samplesFromReadings(readings []claudia.PlanUsage, now time.Time) []Reading {
	snap := Convert(readings, nil, now, 0)
	var out []Reading
	for _, b := range snap.Backends {
		fetched := b.FetchedAt
		if fetched.IsZero() {
			fetched = now
		}
		for _, w := range b.Windows {
			if w.RemainingPercent == nil {
				continue
			}
			out = append(out, Reading{
				Provider:  b.Provider,
				Window:    w.Name,
				FetchedAt: fetched,
				Remaining: *w.RemainingPercent,
				ResetsAt:  w.ResetsAt,
			})
		}
	}
	return out
}

func normKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ResetsKeyBucket is how coarsely a period's rollover time is bucketed into
// a series key (🎯T669).
//
// The key was the exact published timestamp, which assumed providers publish
// a stable one. Anthropic does not: the same Claude week arrived as
// 01:59:58, 01:59:59, 02:00:00 and 02:00:01 across successive fetches, so one
// week's samples landed in four series and the sparkline drew only whichever
// bucket the current snapshot happened to match — a week that began 31% in.
// Ten minutes is far wider than any observed jitter and far narrower than the
// gap between two periods (five hours at the shortest), so it cannot merge
// periods that are genuinely different.
const ResetsKeyBucket = 10 * time.Minute

func resetsKey(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Round(ResetsKeyBucket).Format(time.RFC3339)
}

// rebucketSeriesKeys rewrites stored keys that predate 🎯T669's bucketing, so
// the samples a jittering provider scattered across neighbouring keys rejoin
// the one series they always belonged to. Idempotent and cheap: it touches
// only rows whose key is not already its own bucket.
func rebucketSeriesKeys(db *sql.DB) error {
	rows, err := db.Query(`SELECT DISTINCT resets_key FROM plan_readings WHERE resets_key <> ''`)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, k := range keys {
		t, err := time.Parse(time.RFC3339, k)
		if err != nil {
			continue
		}
		want := resetsKey(&t)
		if want == k {
			continue
		}
		if _, err := db.Exec(`UPDATE plan_readings SET resets_key = ? WHERE resets_key = ?`, want, k); err != nil {
			return err
		}
	}
	return nil
}
