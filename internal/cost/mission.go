// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

// RecordRequestMission freezes the host mission for one transport request.
// The first write wins, so a later seat target cannot rewrite a request that
// already has a mission. An empty mission is not stored: unknown stays unknown.
// Claudia usage facts are not modified.
func (s *Store) RecordRequestMission(ctx context.Context, seat, requestID, mission string) error {
	if s == nil {
		return fmt.Errorf("usage mission: no store")
	}
	seat, requestID, mission = strings.TrimSpace(seat), strings.TrimSpace(requestID), strings.TrimSpace(mission)
	if seat == "" || requestID == "" {
		return fmt.Errorf("usage mission requires a seat and request id")
	}
	if mission == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO usage_request_missions (seat, request_id, mission) VALUES (?, ?, ?)`, seat, requestID, mission)
	return err
}

// RequestMission returns the mission frozen for one transport request.
func (s *Store) RequestMission(ctx context.Context, seat, requestID string) (string, bool, error) {
	if s == nil {
		return "", false, fmt.Errorf("usage mission: no store")
	}
	var mission string
	err := s.db.QueryRowContext(ctx, `SELECT mission FROM usage_request_missions WHERE seat = ? AND request_id = ?`, strings.TrimSpace(seat), strings.TrimSpace(requestID)).Scan(&mission)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if mission == "" {
		return "", false, nil
	}
	return mission, true, nil
}

// UsageMission is the mission shared by every request reference on fact.
// No references, a missing binding, or disagreeing missions stay unknown.
// The fact itself is left unchanged.
func (s *Store) UsageMission(ctx context.Context, fact *claudiawire.UsageFact) (string, bool, error) {
	if fact == nil || len(fact.Requests) == 0 {
		return "", false, nil
	}
	var mission string
	seen := map[string]struct{}{}
	for _, ref := range fact.Requests {
		id := strings.TrimSpace(ref.RequestID)
		if id == "" {
			return "", false, nil
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		got, ok, err := s.RequestMission(ctx, fact.Seat, id)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return "", false, nil
		}
		if mission == "" {
			mission = got
			continue
		}
		if mission != got {
			return "", false, nil
		}
	}
	return mission, mission != "", nil
}
