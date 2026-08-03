// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	jvsync "github.com/marcelocantos/jevons/internal/sync"
)

// PeerSession is the sqlpipe Peer handle for pure-transport WebSocket
// sessions (🎯T10). Implementations live with the SyncManager residual;
// nil means /ws/sqlpipe fails closed.
type PeerSession interface {
	// Hello returns initial peer bytes for a newly connected client, if any.
	Hello() ([]byte, error)
	// HandlePeerFrame processes one binary frame of PeerMessage(s) and
	// returns response bytes to write back (may be empty).
	HandlePeerFrame(data []byte) ([]byte, error)
	// Close releases session resources for this connection (optional).
	Close() error
}

// PeerSessionFactory builds a PeerSession per pure-transport connection.
// Returning (nil, err) fails the handshake; (nil, nil) means not configured.
type PeerSessionFactory func() (PeerSession, error)

// SetPeerSessionFactory attaches the pure-transport Peer factory.
// Must be called before clients connect to /ws/sqlpipe.
func (s *Server) SetPeerSessionFactory(f PeerSessionFactory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peerSessionFactory = f
}

// handleSqlpipe is the pure sqlpipe transport endpoint (🎯T10 residual).
// WebSocket carries only PeerMessage binary frames — application JSON is
// rejected and the connection closed.
func (s *Server) handleSqlpipe(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, wsAcceptOptions())
	if err != nil {
		slog.Error("sqlpipe accept failed", "err", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	ctx := r.Context()

	s.mu.RLock()
	factory := s.peerSessionFactory
	s.mu.RUnlock()

	if factory == nil {
		// Fail closed: pure endpoint exists, Peer not wired yet.
		_ = conn.Close(websocket.StatusTryAgainLater, "sqlpipe peer not configured")
		slog.Info("sqlpipe pure transport: peer not configured")
		return
	}

	session, err := factory()
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "sqlpipe peer init failed")
		slog.Error("sqlpipe peer factory failed", "err", err)
		return
	}
	if session == nil {
		_ = conn.Close(websocket.StatusTryAgainLater, "sqlpipe peer not configured")
		return
	}
	defer func() {
		if err := session.Close(); err != nil {
			slog.Debug("sqlpipe session close", "err", err)
		}
	}()

	if hello, err := session.Hello(); err != nil {
		slog.Error("sqlpipe hello failed", "err", err)
		_ = conn.Close(websocket.StatusInternalError, "sqlpipe hello failed")
		return
	} else if len(hello) > 0 {
		if err := writeBinary(ctx, conn, hello); err != nil {
			slog.Debug("sqlpipe hello write failed", "err", err)
			return
		}
	}

	slog.Info("sqlpipe pure transport connected")

	for {
		mt, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Info("sqlpipe pure transport disconnected")
			}
			return
		}

		kind, _, classErr := jvsync.ClassifyFrame(wsType(mt), data)
		if classErr != nil || jvsync.IsPureTransportViolation(kind) {
			slog.Warn("sqlpipe pure transport violation", "kind", kind, "err", classErr)
			_ = conn.Close(websocket.StatusUnsupportedData, "pure transport rejects non-peer frames")
			return
		}
		if kind == jvsync.FrameEmpty {
			continue
		}

		resp, err := session.HandlePeerFrame(data)
		if err != nil {
			slog.Error("sqlpipe handle peer frame", "err", err)
			_ = conn.Close(websocket.StatusInternalError, "peer handle failed")
			return
		}
		if len(resp) > 0 {
			if err := writeBinary(ctx, conn, resp); err != nil {
				slog.Debug("sqlpipe response write failed", "err", err)
				return
			}
		}
	}
}

func wsType(mt websocket.MessageType) jvsync.WSMessageType {
	switch mt {
	case websocket.MessageText:
		return jvsync.WSText
	case websocket.MessageBinary:
		return jvsync.WSBinary
	default:
		return jvsync.WSMessageType(mt)
	}
}

func writeBinary(ctx context.Context, conn *websocket.Conn, data []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageBinary, data)
}
