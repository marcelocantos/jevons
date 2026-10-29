// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"sort"
	"time"

	"github.com/marcelocantos/pigeon"
)

// pigeonWriter adapts a pigeon.Conn to the remoteWriter interface.
type pigeonWriter struct{ conn *pigeon.Conn }

func (w pigeonWriter) WriteText(ctx context.Context, data []byte) error {
	return w.conn.Send(ctx, data)
}
func (w pigeonWriter) WriteBinary(ctx context.Context, data []byte) error {
	return w.conn.Send(ctx, data)
}
func (w pigeonWriter) Close() error { return w.conn.Close() }

// ConnectRelay registers with a pigeon relay server and bridges traffic.
// Returns the instance ID.
func (s *Server) ConnectRelay(ctx context.Context, relayURL, token, instanceID string) (string, error) {
	slog.Info("connecting to relay", "url", relayURL)
	// Re-read disk at the activation boundary. An invalid store must never
	// silently downgrade this connection to plaintext or erase the old peer.
	if _, err := s.creds.Load(); err != nil {
		return "", fmt.Errorf("load relay credentials: %w", err)
	}

	lanSrv, err := pigeon.NewLANServer("", nil) // random port, self-signed TLS
	if err != nil {
		return "", fmt.Errorf("LAN server: %w", err)
	}
	s.lanSrv = lanSrv

	cfg := pigeon.Config{
		TLS:        &tls.Config{InsecureSkipVerify: true},
		Token:      token,
		InstanceID: instanceID,
		LANServer:  lanSrv,
	}
	conn, err := pigeon.Register(ctx, relayURL, cfg)
	if err != nil {
		return "", err
	}

	instanceID = conn.InstanceID()
	slog.Info("registered with relay", "instance_id", instanceID)

	// If we have a paired credential, derive the server-side channel
	// from its PairingRecord. The client side derives the matching
	// channel from its PairingArtifact in pigeon.ConnectWithArtifact;
	// info strings are swapped (the client's send is the server's
	// recv and vice versa).
	paired := false
	if rec := s.creds.Get(); rec != nil {
		ch, err := rec.DeriveChannel([]byte("server-to-client"), []byte("client-to-server"))
		if err != nil {
			conn.Close()
			return "", fmt.Errorf("derive channel from credential: %w", err)
		}
		conn.SetChannel(ch)
		conn.SetPairingRecord(rec)
		paired = true // per-connection channel, not the mere presence of a stored key
		slog.Info("relay channel encrypted", "peer", rec.PeerInstanceID)
	} else {
		slog.Warn("relay registered without credential — no application traffic accepted; pair a device with `jevonsd --pair`")
	}

	// A plaintext relay registration keeps the stable instance ID alive but is
	// NOT a virtual client: even the init frame contains private host details.
	remoteID := 0
	if paired {
		remoteID, _ = s.registerRemote(remoteConn{writer: pigeonWriter{conn: conn}, ctx: ctx})
		s.sendJSON(ctx, conn, map[string]any{
			"type": "init", "version": s.version, "home": os.Getenv("HOME"),
		})
	}

	// Every extra peer gets its own relay registration and its own channel.
	// A Conn has exactly one crypto.Channel; sharing a Conn across devices
	// would decrypt the other device under the wrong peer key. The original
	// registration remains unchanged for the original iOS peer.
	ids := s.creds.PeerIDs()
	sort.Strings(ids)
	var extras []*pigeon.Conn
	closeOnFailure := func() {
		for _, extra := range extras {
			_ = extra.Close()
		}
		if paired {
			s.unregisterRemote(remoteID)
		}
		_ = conn.Close()
	}
	for _, peerID := range ids {
		if rec := s.creds.GetPeer(peerID); rec != nil && peerID != s.creds.Get().PeerInstanceID {
			if peerID == instanceID {
				closeOnFailure()
				return "", fmt.Errorf("secondary peer %q collides with primary relay registration", peerID)
			}
			extra, err := pigeon.Register(ctx, relayURL, pigeon.Config{
				TLS: &tls.Config{InsecureSkipVerify: true}, Token: token, InstanceID: peerID,
			})
			if err != nil {
				closeOnFailure()
				return "", fmt.Errorf("register peer %q: %w", peerID, err)
			}
			if extra.InstanceID() != peerID {
				extra.Close()
				closeOnFailure()
				return "", fmt.Errorf("relay assigned %q, expected peer %q", extra.InstanceID(), peerID)
			}
			ch, err := rec.DeriveChannel([]byte("server-to-client"), []byte("client-to-server"))
			if err != nil {
				extra.Close()
				closeOnFailure()
				return "", fmt.Errorf("derive channel for %q: %w", peerID, err)
			}
			extras = append(extras, extra)
			extra.SetChannel(ch)
			extra.SetPairingRecord(rec)
			remoteID, _ := s.registerRemote(remoteConn{writer: pigeonWriter{conn: extra}, ctx: ctx})
			s.sendJSON(ctx, extra, map[string]any{"type": "init", "version": s.version, "home": os.Getenv("HOME")})
			go s.readPairedRelay(ctx, extra, remoteID, peerID)
			slog.Info("relay peer channel encrypted", "peer", peerID)
		}
	}

	// Read loop: process messages from the relay.
	go func() {
		defer func() {
			if paired {
				s.unregisterRemote(remoteID)
			}
			conn.Close()
			slog.Info("relay connection closed", "instance_id", instanceID)
		}()

		for {
			data, err := conn.Recv(ctx)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("relay read error", "err", err)
				}
				return
			}

			s.handleRelayInbound(data, paired, func(v any) { s.sendJSON(ctx, conn, v) })

		}
	}()

	return instanceID, nil
}

// handleRelayInbound is the actual read-loop dispatch. Only a connection
// whose channel was installed after successful pairing may reach ANY action.
// A credential in the store alone is not authentication for this connection.
func (s *Server) handleRelayInbound(data []byte, paired bool, send func(any)) {
	if !paired {
		return
	}
	var msg struct {
		Type   string `json:"type"`
		Action string `json:"action"`
		Value  string `json:"value"`
		Text   string `json:"text"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Debug("relay: non-JSON message, skipping")
		return
	}
	switch msg.Type {
	case "action":
		s.HandleAction(msg.Action, msg.Value)
	case "user_message":
		s.HandleUserMessage(msg.Text)
	case "plan_usage_request":
		if validRelayRequestID(msg.ID) {
			send(s.relayPlanUsageResponse(msg.ID))
		}
	}
}

func (s *Server) sendJSON(ctx context.Context, conn *pigeon.Conn, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("relay: marshal failed", "err", err)
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.Send(sendCtx, data); err != nil {
		slog.Error("relay: send failed", "err", err)
	}
}

// validRelayRequestID bounds reflected identifiers in responses and logs.
func validRelayRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// relayPlanUsageResponse serializes the same handler used by GET /api/plan-usage;
// it does not route through public HTTP or bypass the loopback bind.
func (s *Server) relayPlanUsageResponse(id string) any {
	w := httptest.NewRecorder()
	s.handlePlanUsage(w, httptest.NewRequest("GET", "/api/plan-usage", nil))
	var payload json.RawMessage = w.Body.Bytes()
	return struct {
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		Payload json.RawMessage `json:"payload"`
	}{"plan_usage_response", id, payload}
}

// readPairedRelay owns exactly one secondary registration's channel.
func (s *Server) readPairedRelay(ctx context.Context, conn *pigeon.Conn, remoteID int, peerID string) {
	defer s.unregisterRemote(remoteID)
	defer conn.Close()
	for {
		data, err := conn.Recv(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("relay peer read error", "peer", peerID, "err", err)
			}
			return
		}
		s.handleRelayInbound(data, true, func(v any) { s.sendJSON(ctx, conn, v) })
	}
}
