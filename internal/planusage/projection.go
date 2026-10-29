// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// ProjectionQuery produces the complete owner payload. No Go field assembly
// sits between this query subscription and the HTTP/WebSocket transports.
//
//go:embed projection.sql
var ProjectionQuery string

// Replica is the shared control-plane connection, not a second usage link.
type Replica interface {
	Read(func(*sqlpipe.Database) error) error
	Write(func(*sqlpipe.Database) error) error
	SubscribeQuery(context.Context, string) (<-chan sqlpipe.QueryResult, error)
}

// Projection retains only the opaque result of the standing SQL subscription.
// Policy consumers decode those exact bytes; no vendor polling happens here.
type Projection struct {
	mu      sync.RWMutex
	payload json.RawMessage
}

func SubscribeProjection(ctx context.Context, replica Replica, onUpdate func()) (*Projection, error) {
	results, err := replica.SubscribeQuery(ctx, ProjectionQuery)
	if err != nil {
		return nil, err
	}
	p := &Projection{}
	// The replica's first result is its persisted state, even with no broker.
	first, ok := <-results
	if !ok {
		return nil, fmt.Errorf("plan usage subscription closed before initial result")
	}
	if err := p.accept(first); err != nil {
		return nil, err
	}
	go func() {
		for result := range results {
			if err := p.accept(result); err != nil {
				slog.Error("plan usage subscription result rejected", "err", err)
				continue
			}
			if onUpdate != nil {
				onUpdate()
			}
		}
		if ctx.Err() == nil {
			slog.Error("plan usage subscription closed unexpectedly; last observation retained")
		}
	}()
	return p, nil
}

func (p *Projection) accept(q sqlpipe.QueryResult) error {
	if len(q.Rows) != 1 || len(q.Rows[0]) != 1 {
		return fmt.Errorf("plan usage query returned %d rows; expected one JSON cell", len(q.Rows))
	}
	var raw []byte
	switch v := q.Rows[0][0].(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = append([]byte(nil), v...)
	default:
		return fmt.Errorf("plan usage query returned %T instead of JSON", v)
	}
	var validation Snapshot
	if err := json.Unmarshal(raw, &validation); err != nil {
		return fmt.Errorf("plan usage query JSON: %w", err)
	}
	p.mu.Lock()
	p.payload = raw
	p.mu.Unlock()
	return nil
}

func (p *Projection) JSON() json.RawMessage {
	if p == nil {
		return json.RawMessage(`{"pending":true,"error":"plan usage replica unavailable"}`)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append(json.RawMessage(nil), p.payload...)
}

func (p *Projection) Snapshot() Snapshot {
	var snap Snapshot
	if err := json.Unmarshal(p.JSON(), &snap); err != nil {
		return Snapshot{Pending: true, Error: err.Error()}
	}
	// Age is evaluated for policy, without republishing or rewriting the
	// subscribed JSON. A disconnected provider never stays fresh forever.
	now := time.Now()
	for i := range snap.Backends {
		be := &snap.Backends[i]
		if be.StaleAt != nil && !now.Before(*be.StaleAt) {
			be.Stale = true
		}
	}
	return snap
}

func (p *Projection) CapacityRemaining(load map[string]int) (*float64, string) {
	snap := p.Snapshot()
	for i := range snap.Backends {
		snap.Backends[i].FleetAgents = load[snap.Backends[i].Provider]
		if snap.Backends[i].Stale {
			snap.Backends[i].Status = StatusUnavailable
		}
	}
	f, source, ok := snap.TightestRemaining()
	if !ok {
		return nil, ""
	}
	return &f, source
}
