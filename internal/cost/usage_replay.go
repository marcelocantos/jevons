// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/marcelocantos/claudia"
)

const (
	// Match Claudia's bounded history-read timeout. Four reads plus one
	// discovery call leave room below its 60-second idle cursor lifetime
	// at the normal three-second collector cadence.
	usageReplayBatch    = 4
	usageHistoryTimeout = 10 * time.Second
	// Completed discovery cursors are retained for retries for 60 seconds,
	// with at most four per sidecar. A new sweep every 30 seconds leaves
	// that bounded cache room; usage replay continues between sweeps.
	usageInventoryInterval = 30 * time.Second
)

// These are projection bookkeeping in the existing accounting database, not
// another journal. A durable keyset position rotates past unavailable histories
// and survives restarts. Errors remain visible until that generation is read.
const usageReplaySchema = `
CREATE TABLE IF NOT EXISTS usage_replay_position (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	seat TEXT NOT NULL,
	session_id TEXT NOT NULL
);
INSERT OR IGNORE INTO usage_replay_position (id, seat, session_id) VALUES (1, '', '');
CREATE TABLE IF NOT EXISTS usage_replay_errors (
	seat TEXT NOT NULL,
	session_id TEXT NOT NULL,
	message TEXT NOT NULL,
	PRIMARY KEY (seat, session_id)
);
`

// CatchUpOnce reads one discovery page and at most usageReplayBatch usage pages.
// It never holds a database transaction during an RPC. Reaching an empty usage
// page means caught up now, not sealed forever: subsequent rotations query again.
// Discovery covers all Claudia histories, without asserting Jevons ownership.
func (c *Collector) CatchUpOnce(ctx context.Context) (result error) {
	if c.usageSource == nil {
		return nil
	}
	if !c.replayMu.TryLock() {
		return fmt.Errorf("history catch-up is already running")
	}
	defer c.replayMu.Unlock()
	c.mu.Lock()
	c.health.HistoryStarted = c.now()
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.health.HistoryStarted = time.Time{}
		c.health.LastHistoryErr = errString(result)
		if result == nil {
			c.health.LastHistoryPass = c.now()
		}
	}()
	if c.usageSource.Discover == nil || c.usageSource.Query == nil {
		return fmt.Errorf("history source requires both discovery and usage queries")
	}
	if !c.now().Before(c.nextDiscovery) {
		query := claudia.GenerationQuery{Cursor: c.discoveryCursor, Limit: claudia.GenerationPageLimit}
		rpcCtx, cancel := context.WithTimeout(ctx, usageHistoryTimeout)
		page, err := c.usageSource.Discover(rpcCtx, query)
		cancel()
		if err == nil {
			err = page.Validate(query.Cursor, query.Limit)
		}
		if err == nil {
			err = c.store.rememberUsageGenerations(ctx, page.Generations)
		}
		if err != nil {
			var expired *claudia.GenerationDiscoveryError
			if errors.As(err, &expired) && expired.Code == claudia.DiscoveryCursorExpired {
				c.discoveryCursor = "" // Known identities/cursors are never discarded.
			}
			if errors.As(err, &expired) && expired.Code == claudia.DiscoveryBusy {
				c.nextDiscovery = c.now().Add(usageInventoryInterval)
			}
			result = fmt.Errorf("discover history: %w", err)
			c.mu.Lock()
			c.health.InventoryIncomplete = true
			c.mu.Unlock()
		} else {
			// Advance only after the whole page's identities are durable. A lost
			// response or database error can replay the source page safely.
			c.discoveryCursor = page.Cursor
			if !page.More {
				c.nextDiscovery = c.now().Add(usageInventoryInterval)
			}
			c.mu.Lock()
			if len(page.Issues) > 0 || (!page.More && !page.Complete) {
				c.health.InventoryIncomplete = true
			}
			if page.Complete {
				c.health.LastInventoryComplete = c.now()
				c.health.InventoryIncomplete = false
			}
			c.mu.Unlock()
			if len(page.Issues) > 0 || (!page.More && !page.Complete) {
				result = fmt.Errorf("history traversal reported incomplete coverage (%d page issues)", len(page.Issues))
			}
		}
	}
	// An unavailable discovery source must not prevent replay of identities
	// already known from previous discovery or live/spool observations.
	queries, err := c.store.nextUsageQueries(ctx)
	if err != nil {
		return errors.Join(result, err)
	}
	for _, q := range queries {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		rpcCtx, cancel := context.WithTimeout(ctx, usageHistoryTimeout)
		page, queryErr := c.usageSource.Query(rpcCtx, q)
		cancel()
		if queryErr == nil {
			queryErr = c.store.IngestUsagePage(ctx, q, page)
		}
		// Advance fairness even on failure, but never the journal cursor.
		// Store a bounded current error per generation, cleared on repair.
		if err := c.store.finishUsageQuery(ctx, q, queryErr); err != nil {
			return errors.Join(result, err)
		}
		if queryErr != nil {
			result = errors.Join(result, fmt.Errorf("history %s/%s: %w", q.Seat, q.SessionID, queryErr))
		}
	}
	var unresolved int
	if err := c.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_replay_errors`).Scan(&unresolved); err != nil {
		return errors.Join(result, err)
	}
	if unresolved != 0 {
		result = errors.Join(result, fmt.Errorf("%d histories still have replay errors", unresolved))
	}
	return result
}

func (s *Store) rememberUsageGenerations(ctx context.Context, generations []claudia.GenerationIdentity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, g := range generations {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO usage_cursors (seat, session_id, seq) VALUES (?, ?, 0)`, g.Seat, g.SessionID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) nextUsageQueries(ctx context.Context) ([]claudia.UsageQuery, error) {
	// Two bounded reads permit wraparound without holding a connection while
	// making provider-neutral RPCs. New identities are visited on later laps.
	for lap := 0; lap < 2; lap++ {
		rows, err := s.db.QueryContext(ctx, `SELECT c.seat, c.session_id, c.seq
			FROM usage_cursors AS c
			JOIN usage_replay_position AS p ON p.id = 1
			WHERE (c.seat, c.session_id) > (p.seat, p.session_id)
			ORDER BY c.seat, c.session_id LIMIT ?`, usageReplayBatch)
		if err != nil {
			return nil, err
		}
		var queries []claudia.UsageQuery
		for rows.Next() {
			q := claudia.UsageQuery{Limit: claudia.UsagePageLimit}
			if err := rows.Scan(&q.Seat, &q.SessionID, &q.After); err != nil {
				rows.Close()
				return nil, err
			}
			queries = append(queries, q)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(queries) > 0 {
			return queries, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE usage_replay_position SET seat = '', session_id = '' WHERE id = 1`); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (s *Store) finishUsageQuery(ctx context.Context, q claudia.UsageQuery, queryErr error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if queryErr != nil {
		message := queryErr.Error()
		const maxErrorBytes = 1024
		if len(message) > maxErrorBytes {
			message = message[:maxErrorBytes]
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO usage_replay_errors (seat, session_id, message) VALUES (?, ?, ?)
			ON CONFLICT(seat, session_id) DO UPDATE SET message = excluded.message`, q.Seat, q.SessionID, message)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM usage_replay_errors WHERE seat = ? AND session_id = ?`, q.Seat, q.SessionID)
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_replay_position SET seat = ?, session_id = ? WHERE id = 1`, q.Seat, q.SessionID); err != nil {
		return err
	}
	return tx.Commit()
}
