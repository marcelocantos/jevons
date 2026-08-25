// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package statedb

import (
	"database/sql"
	"fmt"
	"time"
)

// Agent is the product projection of one fleet seat. Claudia's
// agents.json remains the harness session map (🎯T548.4 residual).
type Agent struct {
	Name      string
	Parent    string
	Purpose   string
	TargetID  string
	SessionID string
	Provider  string
	Model     string
	WorkDir   string
	Status    string
	UpdatedAt string
}

// UpsertAgent inserts or replaces one fleet row.
func (s *Store) UpsertAgent(a Agent) error {
	if s == nil || a.Name == "" {
		return nil
	}
	if a.UpdatedAt == "" {
		a.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO agents
		 (name, parent, purpose, target_id, session_id, provider, model, workdir, status, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.Name, a.Parent, a.Purpose, a.TargetID, a.SessionID,
		a.Provider, a.Model, a.WorkDir, a.Status, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("statedb: upsert agent %s: %w", a.Name, err)
	}
	return nil
}

// DeleteAgent removes a fleet row.
func (s *Store) DeleteAgent(name string) error {
	if s == nil || name == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM agents WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("statedb: delete agent %s: %w", name, err)
	}
	return nil
}

// GetAgent returns one row, or nil when missing.
func (s *Store) GetAgent(name string) (*Agent, error) {
	if s == nil || name == "" {
		return nil, nil
	}
	var a Agent
	err := s.db.QueryRow(
		`SELECT name, parent, purpose, target_id, session_id, provider, model, workdir, status, updated_at
		 FROM agents WHERE name = ?`, name,
	).Scan(&a.Name, &a.Parent, &a.Purpose, &a.TargetID, &a.SessionID,
		&a.Provider, &a.Model, &a.WorkDir, &a.Status, &a.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("statedb: get agent: %w", err)
	}
	return &a, nil
}

// ListAgents returns every projected seat, name-sorted.
func (s *Store) ListAgents() ([]Agent, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT name, parent, purpose, target_id, session_id, provider, model, workdir, status, updated_at
		 FROM agents ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("statedb: list agents: %w", err)
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(&a.Name, &a.Parent, &a.Purpose, &a.TargetID, &a.SessionID,
			&a.Provider, &a.Model, &a.WorkDir, &a.Status, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("statedb: list scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReplaceAgents is list-refresh: upsert every row and delete names not in live.
func (s *Store) ReplaceAgents(live []Agent) error {
	if s == nil {
		return nil
	}
	keep := make(map[string]struct{}, len(live))
	for _, a := range live {
		if a.Name == "" {
			continue
		}
		if err := s.UpsertAgent(a); err != nil {
			return err
		}
		keep[a.Name] = struct{}{}
	}
	have, err := s.ListAgents()
	if err != nil {
		return err
	}
	for _, a := range have {
		if _, ok := keep[a.Name]; !ok {
			if err := s.DeleteAgent(a.Name); err != nil {
				return err
			}
		}
	}
	return nil
}
