// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// These additive tables belong to the existing accounting database. The
// conversation journal remains the source; this is a replayable projection.
// No existing table is rewritten or migrated by their creation.
const usageSchema = `
CREATE TABLE IF NOT EXISTS usage_calls (
	seat TEXT NOT NULL,
	session_id TEXT NOT NULL,
	call_id TEXT NOT NULL,
	seq INTEGER NOT NULL CHECK (seq > 0),
	ts TEXT NOT NULL,
	state TEXT NOT NULL,
	input_tokens INTEGER CHECK (input_tokens >= 0),
	output_tokens INTEGER CHECK (output_tokens >= 0),
	cache_create_tokens INTEGER CHECK (cache_create_tokens >= 0),
	cache_read_tokens INTEGER CHECK (cache_read_tokens >= 0),
	api_equivalent_cost_usd REAL CHECK (api_equivalent_cost_usd >= 0),
	fact TEXT NOT NULL,
	PRIMARY KEY (seat, session_id, call_id)
);
CREATE INDEX IF NOT EXISTS idx_usage_calls_ts ON usage_calls(ts);
CREATE TABLE IF NOT EXISTS usage_cursors (
	seat TEXT NOT NULL,
	session_id TEXT NOT NULL,
	seq INTEGER NOT NULL CHECK (seq >= 0),
	PRIMARY KEY (seat, session_id)
);
CREATE TABLE IF NOT EXISTS usage_failures (
	seat TEXT NOT NULL,
	session_id TEXT NOT NULL,
	ts TEXT NOT NULL,
	message TEXT NOT NULL,
	PRIMARY KEY (seat, session_id, ts, message)
);
CREATE INDEX IF NOT EXISTS idx_usage_failures_ts ON usage_failures(ts);
CREATE TABLE IF NOT EXISTS usage_request_missions (
	seat TEXT NOT NULL,
	request_id TEXT NOT NULL,
	mission TEXT NOT NULL CHECK (mission != ''),
	PRIMARY KEY (seat, request_id)
);
`

const usageTimeLayout = "2006-01-02T15:04:05.000000000Z"

// UsageFailure records a timestamped loss of observation, not a zero-consumption
// call. Later failures do not erase the uncertainty in earlier time windows.
// An empty SessionID means the emitter could identify only the seat.
type UsageFailure struct {
	Seat, SessionID, Message string
	At                       time.Time
}

// ErrUsageFactRejected marks a usage fact that fails Claudia's fact or
// transition contract. It is evidence that cannot be verified, not an
// infrastructure failure: a caller that walks many facts records it as an
// observation failure and carries on, rather than stopping at it.
var ErrUsageFactRejected = errors.New("usage fact rejected")

// RecordRejectedUsageFact records one rejected fact as an observation
// failure. at is the time window the fact covers. The same message for the
// same generation is recorded once, so a fact rejected on every catch-up
// pass is one failure, not one per pass. A failure is never removed: a fact
// that later becomes valid is ingested then, and the earlier loss of
// observation stays on record.
func (s *Store) RecordRejectedUsageFact(ctx context.Context, seat, session string, at time.Time, message string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage_failures (seat, session_id, ts, message)
		SELECT ?, ?, ?, ? WHERE NOT EXISTS
		(SELECT 1 FROM usage_failures WHERE seat = ? AND session_id = ? AND message = ?)`,
		seat, session, at.UTC().Format(usageTimeLayout), message, seat, session, message)
	return err
}

// UsageCursor is the last contiguous journal sequence durably consumed.
// It is independent of live events, whose delivery can have gaps.
func (s *Store) UsageCursor(ctx context.Context, seat, session string) (uint64, error) {
	var seq uint64
	err := s.db.QueryRowContext(ctx, `SELECT seq FROM usage_cursors WHERE seat = ? AND session_id = ?`, seat, session).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return seq, err
}

// IngestUsagePage commits facts and their replay cursor together. Failure
// leaves both unchanged, so a later query retries the same page. The caller
// supplies the requested identities, rather than trusting the response's own.
func (s *Store) IngestUsagePage(ctx context.Context, query claudiawire.UsageQuery, page *claudiawire.UsagePage) error {
	limit := query.Limit
	if limit == 0 {
		limit = claudiawire.UsagePageLimit
	}
	if err := page.Validate(query.Seat, query.SessionID, query.After, limit); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var after uint64
	err = tx.QueryRowContext(ctx, `SELECT seq FROM usage_cursors WHERE seat = ? AND session_id = ?`, query.Seat, query.SessionID).Scan(&after)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if after != query.After {
		return fmt.Errorf("usage cursor changed: have %d, requested %d", after, query.After)
	}
	for i := range page.Facts {
		if _, err := ingestUsageFact(ctx, tx, &page.Facts[i]); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_cursors (seat, session_id, seq) VALUES (?, ?, ?)
		ON CONFLICT(seat, session_id) DO UPDATE SET seq = excluded.seq`, query.Seat, query.SessionID, page.Next); err != nil {
		return err
	}
	return tx.Commit()
}

// IngestUsageFact accepts a live or spooled observation, without advancing
// the contiguous replay cursor. Repeated delivery contributes no new usage.
func (s *Store) IngestUsageFact(ctx context.Context, seat, session string, fact *claudiawire.UsageFact) error {
	if err := fact.ValidateIdentity(seat, session); err != nil {
		return fmt.Errorf("%w: %w", ErrUsageFactRejected, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := ingestUsageFact(ctx, tx, fact); err != nil {
		return err
	}
	return tx.Commit()
}

func ingestUsageFact(ctx context.Context, tx *sql.Tx, f *claudiawire.UsageFact) (bool, error) {
	// Seeing a live final must not suppress replay of earlier calls from the
	// same generation. Record identity with a zero contiguous cursor.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO usage_cursors (seat, session_id, seq) VALUES (?, ?, 0)`, f.Seat, f.SessionID); err != nil {
		return false, err
	}
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT fact FROM usage_calls WHERE seat = ? AND session_id = ? AND call_id = ?`, f.Seat, f.SessionID, f.CallID).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	var previous *claudiawire.UsageFact
	if err == nil {
		var old claudiawire.UsageFact
		if err := json.Unmarshal([]byte(raw), &old); err != nil {
			return false, fmt.Errorf("stored usage fact is corrupt: %w", err)
		}
		previous = &old
	}
	// The transition contract lives in claudiawire. jevons_claudia_next calls
	// Claudia. The published pin uses the local copy of that contract.
	// The host owns only persistence and attribution.
	apply, err := claudiawire.ValidateUsageTransition(previous, f)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrUsageFactRejected, err)
	}
	if !apply {
		return false, nil
	}
	body, err := json.Marshal(f)
	if err != nil {
		return false, err
	}
	ts := f.FinishedAt
	if ts == "" {
		ts = f.StartedAt
	}
	// Validation has already established RFC3339. Normalize offsets for
	// indexed time comparisons; do not compare raw provider date strings.
	stamp, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_calls
		(seat, session_id, call_id, seq, ts, state, input_tokens, output_tokens,
		 cache_create_tokens, cache_read_tokens, api_equivalent_cost_usd, fact)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(seat, session_id, call_id) DO UPDATE SET
		 seq = excluded.seq, ts = excluded.ts, state = excluded.state,
		 input_tokens = excluded.input_tokens, output_tokens = excluded.output_tokens,
		 cache_create_tokens = excluded.cache_create_tokens, cache_read_tokens = excluded.cache_read_tokens,
		 api_equivalent_cost_usd = excluded.api_equivalent_cost_usd, fact = excluded.fact`,
		f.Seat, f.SessionID, f.CallID, f.Seq, stamp.UTC().Format(usageTimeLayout), f.State,
		f.Counters.InputTokens, f.Counters.OutputTokens, f.Counters.CacheCreationInputTokens, f.Counters.CacheReadInputTokens, f.APIEquivalentCostUSD, string(body))
	return err == nil, err
}

// StructuredUsage describes known lower bounds and their missing dimensions.
// Reasoning is a subset of output and context is a separate measurement;
// neither is added to consumption. No subscription quota is inferred here.
type StructuredUsage struct {
	Known                Usage   `json:"known"`
	APIEquivalentCostUSD float64 `json:"api_equivalent_cost_usd"`
	Calls                int64   `json:"calls"`
	IncompleteCalls      int64   `json:"incomplete_calls"`
	UnknownInput         int64   `json:"unknown_input_calls"`
	UnknownOutput        int64   `json:"unknown_output_calls"`
	UnknownCacheCreate   int64   `json:"unknown_cache_create_calls"`
	UnknownCacheRead     int64   `json:"unknown_cache_read_calls"`
	UnknownCost          int64   `json:"unknown_cost_calls"`
	ObservationFailures  int64   `json:"observation_failures"`
	// Prefer observed inclusive input over the disjoint bucket sum, never
	// both. Without either a total or every bucket, this is a lower bound.
	KnownInputTotal   int64 `json:"known_input_total"`
	UnknownInputTotal int64 `json:"unknown_input_total_calls"`
}

func (u StructuredUsage) KnownConsumptionTokens() int64 {
	return u.KnownInputTotal + u.Known.Output
}

func (s *Store) StructuredUsage(ctx context.Context, from, to time.Time) (StructuredUsage, error) {
	var out StructuredUsage
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_create_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
		COALESCE(SUM(api_equivalent_cost_usd), 0), COUNT(*),
		COALESCE(SUM(state = 'started' OR state = 'unknown'), 0),
		COUNT(*) - COUNT(input_tokens), COUNT(*) - COUNT(output_tokens),
		COUNT(*) - COUNT(cache_create_tokens), COUNT(*) - COUNT(cache_read_tokens),
		COUNT(*) - COUNT(api_equivalent_cost_usd),
		COALESCE(SUM(COALESCE(json_extract(fact, '$.counters.total_input_tokens'),
			COALESCE(input_tokens, 0) + COALESCE(cache_create_tokens, 0) + COALESCE(cache_read_tokens, 0))), 0),
		COALESCE(SUM(json_extract(fact, '$.counters.total_input_tokens') IS NULL AND
			(input_tokens IS NULL OR cache_create_tokens IS NULL OR cache_read_tokens IS NULL)), 0)
		FROM usage_calls WHERE ts >= ? AND ts < ?`,
		from.UTC().Format(usageTimeLayout), to.UTC().Format(usageTimeLayout)).Scan(
		&out.Known.Input, &out.Known.Output, &out.Known.CacheCreate, &out.Known.CacheRead,
		&out.APIEquivalentCostUSD, &out.Calls, &out.IncompleteCalls,
		&out.UnknownInput, &out.UnknownOutput, &out.UnknownCacheCreate, &out.UnknownCacheRead, &out.UnknownCost,
		&out.KnownInputTotal, &out.UnknownInputTotal)
	if err != nil {
		return out, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_failures WHERE ts >= ? AND ts < ?`,
		from.UTC().Format(usageTimeLayout), to.UTC().Format(usageTimeLayout)).Scan(&out.ObservationFailures)
	return out, err
}
