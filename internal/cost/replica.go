// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// ReplicaSource is the Jevons side of the Claudia-owned usage and receipt
// tables. The seat pump writes the Claudia file. This side handshakes and
// reads the replica. It does not query the usage socket.
type ReplicaSource struct {
	mu      sync.Mutex
	plane   *controlplane.Plane
	path    string
	replica *sqlpipe.Database
}

// OpenReplica opens the Claudia-owned control file under stateDir, binds it
// as the seat-pump publisher, and names the Jevons replica beside it.
func OpenReplica(stateDir string) (*ReplicaSource, error) {
	plane, err := controlplane.Open(filepath.Join(stateDir, "claudia-control.db"))
	if err != nil {
		return nil, err
	}
	controlplane.Bind(plane)
	return &ReplicaSource{
		plane: plane,
		path:  filepath.Join(stateDir, "control-replica.db"),
	}, nil
}

// Close releases the replica file and the Claudia-owned file.
func (r *ReplicaSource) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var err error
	if r.replica != nil {
		err = r.replica.Close()
		r.replica = nil
	}
	if r.plane != nil {
		if closeErr := r.plane.Close(); err == nil {
			err = closeErr
		}
	}
	controlplane.Bind(nil)
	return err
}

func (r *ReplicaSource) ensureReplica() error {
	if r.replica != nil {
		return nil
	}
	db, err := sqlpipe.OpenDatabase(r.path)
	if err != nil {
		return err
	}
	if err := controlplane.EnsureSchema(db); err != nil {
		_ = db.Close()
		return err
	}
	r.replica = db
	return nil
}

// sync copies committed Claudia rows into the replica. Publishes wait for
// the handshake so the two sides do not share one sqlite connection unsafely.
func (r *ReplicaSource) sync() error {
	if err := r.ensureReplica(); err != nil {
		return err
	}
	return r.plane.WithDB(func(claudiaDB *sqlpipe.Database) error {
		client, err := sqlpipe.NewPeer(claudiaDB, sqlpipe.PeerConfig{OwnedTables: map[string]bool{
			controlplane.UsageTable:   true,
			controlplane.ReceiptTable: true,
		}})
		if err != nil {
			return err
		}
		defer client.Close()
		server, err := sqlpipe.NewPeer(r.replica, sqlpipe.PeerConfig{ApproveOwnership: approveClaudiaOwnership})
		if err != nil {
			return err
		}
		defer server.Close()
		if err := sqlpipe.SyncPeerHandshake(client, server); err != nil {
			return err
		}
		if client.State() != sqlpipe.PeerLive || server.State() != sqlpipe.PeerLive {
			return fmt.Errorf("control plane handshake not live: writer=%d reader=%d", client.State(), server.State())
		}
		return nil
	})
}

func approveClaudiaOwnership(tables map[string]bool) bool {
	if len(tables) == 0 {
		return false
	}
	for name := range tables {
		if !controlplane.Owns(name) {
			return false
		}
	}
	return true
}

func sqlText(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case []byte:
		return string(t), true
	default:
		return "", false
	}
}

// CatchUpReplicas ingests usage rows from the Jevons replica. Replaying a
// fact does not add usage. The usage socket is not read.
func (c *Collector) CatchUpReplicas(ctx context.Context) (result error) {
	if c == nil || c.replica == nil {
		return nil
	}
	c.replayMu.Lock()
	defer c.replayMu.Unlock()
	c.mu.Lock()
	c.health.HistoryStarted = c.now()
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.health.HistoryStarted = time.Time{}
		c.health.LastHistoryErr = errString(result)
		if result == nil {
			c.health.LastHistoryPass = c.now()
		}
		c.mu.Unlock()
	}()

	c.replica.mu.Lock()
	defer c.replica.mu.Unlock()
	if err := c.replica.sync(); err != nil {
		return err
	}
	qr, err := c.replica.replica.Query("SELECT fact_json FROM " + controlplane.UsageTable + " ORDER BY seat, session_id, seq")
	if err != nil {
		return err
	}
	if len(qr.Rows) == 0 {
		return nil
	}
	factCol := -1
	for i, col := range qr.Columns {
		if col == "fact_json" {
			factCol = i
			break
		}
	}
	if factCol < 0 {
		return fmt.Errorf("replica usage rows have no fact_json")
	}
	for _, row := range qr.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if factCol >= len(row) {
			return fmt.Errorf("replica usage row is short")
		}
		body, ok := sqlText(row[factCol])
		if !ok {
			return fmt.Errorf("replica fact_json has type %T", row[factCol])
		}
		if err := ApplyReplicatedUsage(ctx, c.store, body); err != nil {
			return err
		}
	}
	return nil
}

// ReplicatedReceipt reads one receipt from the replica after a catch-up.
// The held-receipt sweep uses this instead of the usage socket.
func (c *Collector) ReplicatedReceipt(ctx context.Context, q claudia.ReceiptQuery) (*claudia.RequestReceipt, error) {
	if c == nil || c.replica == nil {
		return nil, fmt.Errorf("replicated receipt: no replica")
	}
	if err := c.CatchUpReplicas(ctx); err != nil {
		return nil, err
	}
	c.replica.mu.Lock()
	defer c.replica.mu.Unlock()
	if c.replica.replica == nil {
		return nil, fmt.Errorf("replicated receipt: replica is closed")
	}
	qr, err := c.replica.replica.Query(
		"SELECT receipt_json FROM "+controlplane.ReceiptTable+" WHERE seat = ? AND session_id = ? AND request_id = ?",
		q.Seat, q.SessionID, q.RequestID)
	if err != nil {
		return nil, err
	}
	if len(qr.Rows) == 0 {
		return nil, fmt.Errorf("replicated receipt absent")
	}
	col := -1
	for i, name := range qr.Columns {
		if name == "receipt_json" {
			col = i
			break
		}
	}
	if col < 0 || col >= len(qr.Rows[0]) {
		return nil, fmt.Errorf("replicated receipt row has no receipt_json")
	}
	body, ok := sqlText(qr.Rows[0][col])
	if !ok {
		return nil, fmt.Errorf("replicated receipt_json has type %T", qr.Rows[0][col])
	}
	receipt, err := ApplyReplicatedReceipt(body)
	if err != nil {
		return nil, err
	}
	if err := receipt.ValidateIdentity(q.Seat, q.SessionID, q.RequestID); err != nil {
		return nil, err
	}
	return &receipt, nil
}
