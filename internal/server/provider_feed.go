// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"

	"github.com/marcelocantos/jevons/internal/provider"
)

// Provider feed channel (🎯T27.5): /ws/provider is the dedicated
// contract §4.1 socket a provider dials to deliver its data feeds. The
// hub side (provider.FeedHub) drives describe → subscribe and folds
// events into the aggregated live model; this file is only transport
// (WS ↔ FrameConn) plus the client-facing surfaces:
//
//	provider_event  — one folded feed event, broadcast as it lands
//	provider_status — feed-channel status transition (ok/degraded/…)
//	provider_model  — aggregated snapshot, sent on /ws/remote init
//	GET /api/providers — lifecycle health + feed statuses (observability)

// SetProviderFeeds attaches the feed hub. Nil leaves /ws/provider
// answering with a legible error instead of a silent close.
func (s *Server) SetProviderFeeds(h *provider.FeedHub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providerFeeds = h
}

// SetProviderHealth attaches the lifecycle health snapshot source for
// GET /api/providers (🎯T27.3 phases alongside 🎯T27.5 feed statuses).
func (s *Server) SetProviderHealth(fn func() []provider.Health) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providerHealth = fn
}

// wsFrameConn adapts a coder/websocket connection to provider.FrameConn.
type wsFrameConn struct{ conn *websocket.Conn }

func (c wsFrameConn) ReadFrame(ctx context.Context) ([]byte, error) {
	for {
		mt, data, err := c.conn.Read(ctx)
		if err != nil {
			return nil, err
		}
		if mt == websocket.MessageText {
			return data, nil
		}
	}
}

func (c wsFrameConn) WriteFrame(ctx context.Context, data []byte) error {
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func (c wsFrameConn) Close() error { return c.conn.CloseNow() }

// handleProviderFeed accepts one provider feed socket and serves it to
// completion via FeedHub.Attach. Each connection lives on its own
// handler goroutine, so a stalled provider only ever wedges itself.
func (s *Server) handleProviderFeed(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	hub := s.providerFeeds
	s.mu.RUnlock()

	conn, err := websocket.Accept(w, r, wsAcceptOptions())
	if err != nil {
		slog.Error("provider feed accept failed", "err", err)
		return
	}
	if hub == nil {
		data, _ := json.Marshal(map[string]any{
			"type":  "error",
			"error": "provider feed hub not configured",
		})
		_ = conn.Write(r.Context(), websocket.MessageText, data)
		conn.Close(websocket.StatusNormalClosure, "no feed hub")
		return
	}
	conn.SetReadLimit(1 << 20) // 1 MB
	if err := hub.Attach(r.Context(), wsFrameConn{conn: conn}); err != nil {
		// Normal detach path logs inside the hub; nothing further here.
		_ = err
	}
}

// handleProviders reports provider observability: lifecycle health
// (process/attachment phases) and feed-channel statuses.
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	hub := s.providerFeeds
	healthFn := s.providerHealth
	s.mu.RUnlock()

	health := []provider.Health{}
	if healthFn != nil {
		if h := healthFn(); h != nil {
			health = h
		}
	}
	feeds := []provider.FeedStatus{}
	var model map[string]map[string]provider.FeedSnapshot
	if hub != nil {
		if f := hub.Statuses(); f != nil {
			feeds = f
		}
		model = hub.Snapshot()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"providers": health,
		"feeds":     feeds,
		"model":     model,
	})
}
