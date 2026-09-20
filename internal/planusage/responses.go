// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T683: keep what the vendor actually said.
//
// plan_readings stores one float per window, which is everything the
// sparkline needs and nothing else. Every other field the vendor sent is
// gone by the time the row is written — which is how Anthropic's
// per-model weekly windows travelled in each response for weeks while
// the product reported no such figure existed, and why the only way to
// answer "what did it say earlier?" was to ask again, on the endpoint
// where asking again is what gets us refused (🎯T677 / 🎯T681).
//
// claudia v0.39.0 carries the body home on the reading (its 🎯T84). This
// table is where it lands: a short, bounded history of raw payloads, so
// a surface question is answered from disk rather than from the network.

const (
	// ResponsesPerProvider is how many payloads are kept per provider.
	// Enough to see a change and what preceded it; small enough that the
	// table stays a rounding error beside the readings.
	ResponsesPerProvider = 50

	// MaxResponseBody bounds one stored payload. claudia already caps
	// what it carries; this is the second wall, so a vendor cannot grow
	// our database by growing its response.
	MaxResponseBody = 64 << 10
)

const responsesSchema = `
CREATE TABLE IF NOT EXISTS plan_responses (
	id INTEGER PRIMARY KEY,
	provider TEXT NOT NULL,
	fetched_at TEXT NOT NULL,
	http_status INTEGER NOT NULL,
	status TEXT NOT NULL,
	reason TEXT NOT NULL,
	body TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plan_responses_provider
	ON plan_responses(provider, fetched_at);
`

// Response is one recorded vendor payload.
type Response struct {
	Provider   string
	FetchedAt  time.Time
	HTTPStatus int
	// Status is claudia's verdict for the reading (available / unavailable).
	Status string
	Reason string
	Body   string
}

// AppendResponses stores one payload per reading that carries a body, then
// trims each touched provider back to ResponsesPerProvider.
//
// A reading with no body is skipped rather than stored empty: a carried-
// forward or bodyless reading is not a response, and a row saying nothing
// would dilute the very history this table exists to keep.
func (s *ReadingStore) AppendResponses(rs []Response) error {
	if s == nil || s.db == nil || len(rs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("plan responses: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	touched := map[string]bool{}
	for _, r := range rs {
		body := r.Body
		if strings.TrimSpace(body) == "" {
			continue
		}
		if len(body) > MaxResponseBody {
			body = body[:MaxResponseBody]
		}
		provider := normKey(r.Provider)
		if provider == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO plan_responses(provider, fetched_at, http_status, status, reason, body)
			 VALUES (?,?,?,?,?,?)`,
			provider, r.FetchedAt.UTC().Format(time.RFC3339Nano),
			r.HTTPStatus, r.Status, r.Reason, body,
		); err != nil {
			return fmt.Errorf("plan responses insert: %w", err)
		}
		touched[provider] = true
	}
	for provider := range touched {
		if _, err := tx.Exec(
			`DELETE FROM plan_responses WHERE provider = ? AND id NOT IN (
				SELECT id FROM plan_responses WHERE provider = ?
				ORDER BY id DESC LIMIT ?
			)`, provider, provider, ResponsesPerProvider); err != nil {
			return fmt.Errorf("plan responses trim: %w", err)
		}
	}
	return tx.Commit()
}

// Responses returns the most recent payloads for a provider, newest first.
func (s *ReadingStore) Responses(provider string, limit int) ([]Response, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > ResponsesPerProvider {
		limit = ResponsesPerProvider
	}
	rows, err := s.db.Query(
		`SELECT provider, fetched_at, http_status, status, reason, body
		 FROM plan_responses WHERE provider = ? ORDER BY id DESC LIMIT ?`,
		normKey(provider), limit)
	if err != nil {
		return nil, fmt.Errorf("plan responses query: %w", err)
	}
	defer rows.Close()
	var out []Response
	for rows.Next() {
		var r Response
		var at string
		if err := rows.Scan(&r.Provider, &at, &r.HTTPStatus, &r.Status, &r.Reason, &r.Body); err != nil {
			return nil, err
		}
		// A row whose timestamp cannot be read is still a payload worth
		// returning; the zero time says the clock is unknown, which beats
		// dropping the evidence.
		r.FetchedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// responsesFromReadings lifts the recorded payloads off a fetch.
func responsesFromReadings(readings []claudia.PlanUsage, now time.Time) []Response {
	out := make([]Response, 0, len(readings))
	for _, pu := range readings {
		if strings.TrimSpace(pu.RawBody) == "" {
			continue
		}
		at := pu.FetchedAt
		if at.IsZero() {
			at = now
		}
		out = append(out, Response{
			Provider:   string(pu.Provider),
			FetchedAt:  at,
			HTTPStatus: pu.HTTPStatus,
			Status:     string(pu.Status),
			Reason:     pu.Reason,
			Body:       pu.RawBody,
		})
	}
	return out
}

// ensureResponsesSchema is called on open; separated so the readings
// schema stays the single statement it has always been.
func ensureResponsesSchema(db *sql.DB) error {
	_, err := db.Exec(responsesSchema)
	return err
}

// ResponseRecorder is the half of the store that keeps payloads. It is a
// separate interface from History so a test double stays a few lines and
// a caller that only needs samples is not made to care.
type ResponseRecorder interface {
	AppendResponses(rs []Response) error
}
