// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// Link retry bounds. A refused or failed dial is retried after
// linkRetryMin, doubling to linkRetryMax while the broker stays away. A
// link that stayed up for linkUpAfter counts as established: it is logged
// once, and the next failure starts the backoff again from linkRetryMin.
const (
	linkRetryMin = 100 * time.Millisecond
	linkRetryMax = 5 * time.Second
	linkUpAfter  = 500 * time.Millisecond
)

// ReplicaSource is Jevons' half of the control-plane pair (🎯T1074.9): its
// own file, controlplane.JevonsDBFile in the Jevons state directory, which
// jevonsd alone opens. Jevons writes desired seats, grants and control
// intents there. The Claudia broker owns its own file and writes usage,
// receipts, seat status and control outcomes there. Changesets cross
// between the two processes over the broker's control socket; neither
// process opens the other's file. ReplicaSource does not reconcile seats
// and does not query the usage socket.
type ReplicaSource struct {
	replica *controlplane.Replica
	cancel  context.CancelFunc
	done    chan struct{}

	stateMu sync.Mutex
	attempt int
	linked  bool
	epoch   uint64
	ended   uint64
	downErr string
}

// LinkState is the broker link as this replica last observed it. Linked is
// true once a link has stayed up for linkUpAfter (the consumer's Link call
// gives no handshake-done signal); Epoch counts those establishments, so a
// caller can wait for the link that follows a restart; Ended counts link
// attempts that ended (failed dials included); DownReason is why the last
// reported link ended, empty while linked.
type LinkState struct {
	Linked     bool
	Epoch      uint64
	Ended      uint64
	DownReason string
}

// LinkState reports the broker link's current state.
func (r *ReplicaSource) LinkState() LinkState {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return LinkState{Linked: r.linked, Epoch: r.epoch, Ended: r.ended, DownReason: r.downErr}
}

// OpenReplica opens Jevons' own control file under stateDir and keeps it
// linked to the control socket of the broker this process would dial
// (claudia.ControlSocket) until ctx ends or Close. With the broker
// switched off for this process (CLAUDIA_NO_BROKER), the file is opened and
// nothing is dialled: a hermetic or direct-mode daemon must not reach a
// broker it was told to stay away from.
func OpenReplica(ctx context.Context, stateDir string) (*ReplicaSource, error) {
	socket := ""
	if brokerDisabled() {
		slog.Info("control plane: broker switched off for this process; replica is not linked", "env", "CLAUDIA_NO_BROKER")
	} else {
		s, err := claudia.ControlSocket()
		if err != nil {
			return nil, fmt.Errorf("control plane: broker control socket: %w", err)
		}
		socket = s
	}
	return OpenReplicaLinked(ctx, stateDir, socket)
}

// OpenReplicaLinked is OpenReplica with the control socket named. An empty
// socket opens the file and links nothing. Accepts commit locally either
// way; a link that comes up later delivers them in its handshake.
func OpenReplicaLinked(ctx context.Context, stateDir, socket string) (*ReplicaSource, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("control plane: empty state dir")
	}
	replica, err := controlplane.OpenReplica(filepath.Join(stateDir, controlplane.JevonsDBFile))
	if err != nil {
		return nil, err
	}
	linkCtx, cancel := context.WithCancel(ctx)
	r := &ReplicaSource{replica: replica, cancel: cancel, done: make(chan struct{})}
	if socket == "" {
		close(r.done)
		return r, nil
	}
	go r.keepLinked(linkCtx, socket)
	return r, nil
}

// brokerDisabled mirrors the claudia library's reading of
// CLAUDIA_NO_BROKER: unset, "", 0, false, no and off leave the broker on;
// any other value switches it off.
func brokerDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLAUDIA_NO_BROKER"))) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// keepLinked runs Replica.Link until ctx ends, dialling again after each
// link ends. Each fresh link starts with sqlpipe's handshake, which is what
// delivers whatever either side committed while no link was up.
func (r *ReplicaSource) keepLinked(ctx context.Context, socket string) {
	defer close(r.done)
	wait := linkRetryMin
	for ctx.Err() == nil {
		r.stateMu.Lock()
		r.attempt++
		attempt := r.attempt
		r.stateMu.Unlock()
		up := time.AfterFunc(linkUpAfter, func() { r.markLinked(attempt, socket) })
		err := r.replica.Link(ctx, socket)
		wasUp := !up.Stop()
		if ctx.Err() != nil {
			return
		}
		r.markDown(socket, err)
		if wasUp {
			wait = linkRetryMin
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if !wasUp {
			wait = min(2*wait, linkRetryMax)
		}
	}
}

func (r *ReplicaSource) markLinked(attempt int, socket string) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if attempt != r.attempt || r.linked {
		return
	}
	r.linked, r.downErr = true, ""
	r.epoch++
	slog.Info("control plane: linked to the broker", "socket", socket)
}

func (r *ReplicaSource) markDown(socket string, err error) {
	reason := "broker closed the link"
	if err != nil {
		reason = err.Error()
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.attempt++ // a late markLinked for the ended attempt is ignored
	r.ended++
	if !r.linked && r.downErr != "" {
		return // already reported down; retries stay quiet
	}
	r.linked, r.downErr = false, reason
	slog.Warn("control plane: broker link down; retrying", "socket", socket, "reason", reason)
}

// Close stops the link and releases Jevons' file.
func (r *ReplicaSource) Close() error {
	if r == nil {
		return nil
	}
	r.cancel()
	<-r.done
	return r.replica.Close()
}

// Read runs fn against Jevons' own file. The Claudia tables there are what
// the broker last delivered over the link.
func (r *ReplicaSource) Read(fn func(*sqlpipe.Database) error) error {
	if r == nil {
		return fmt.Errorf("control plane: no replica")
	}
	return r.replica.Read(fn)
}

// AcceptDesiredSeat commits one desired seat on Jevons' file. No live link
// does not fail the accept; the next link's handshake delivers the row.
func (r *ReplicaSource) AcceptDesiredSeat(seat controlplane.DesiredSeat) error {
	return r.replica.Write(func(db *sqlpipe.Database) error {
		return controlplane.PublishDesiredSeat(db, seat)
	})
}

// AcceptGrant commits one grant on Jevons' file.
func (r *ReplicaSource) AcceptGrant(grant controlplane.Grant) error {
	return r.replica.Write(func(db *sqlpipe.Database) error {
		return controlplane.PublishGrant(db, grant)
	})
}

// AcceptControlIntent commits one control intent on Jevons' file.
func (r *ReplicaSource) AcceptControlIntent(intent controlplane.ControlIntent) error {
	return r.replica.Write(func(db *sqlpipe.Database) error {
		return controlplane.PublishControlIntent(db, intent)
	})
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

// columnText returns column name of every row as text.
func columnText(qr sqlpipe.QueryResult, name string) ([]string, error) {
	if len(qr.Rows) == 0 {
		return nil, nil
	}
	col := -1
	for i, c := range qr.Columns {
		if c == name {
			col = i
			break
		}
	}
	if col < 0 {
		return nil, fmt.Errorf("replica rows have no %s", name)
	}
	out := make([]string, 0, len(qr.Rows))
	for _, row := range qr.Rows {
		if col >= len(row) {
			return nil, fmt.Errorf("replica %s row is short", name)
		}
		body, ok := sqlText(row[col])
		if !ok {
			return nil, fmt.Errorf("replica %s has type %T", name, row[col])
		}
		out = append(out, body)
	}
	return out, nil
}

// CatchUpReplicas ingests the usage rows the broker has delivered into
// Jevons' file. Replaying a fact does not add usage. The usage socket is
// not read, and nothing is synced here: the link delivers on its own.
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

	// Copy the rows out under the replica lock and ingest after it, so a
	// slow usage store never holds the link.
	var facts []string
	var statusErr error
	if err := c.replica.Read(func(db *sqlpipe.Database) error {
		qr, err := db.Query("SELECT fact_json FROM " + controlplane.UsageTable + " ORDER BY seat, session_id, seq")
		if err != nil {
			return err
		}
		if facts, err = columnText(qr, "fact_json"); err != nil {
			return err
		}
		// Status is checked so a corrupt projection fails catch-up. It is
		// not applied to the fleet registry: Claudia remains the seat
		// authority.
		statusErr = validateReplicatedStatus(db)
		return nil
	}); err != nil {
		return err
	}
	for _, body := range facts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ApplyReplicatedUsage(ctx, c.store, body); err != nil {
			return err
		}
	}
	return statusErr
}

func validateReplicatedStatus(db *sqlpipe.Database) error {
	qr, err := db.Query("SELECT status_json FROM " + controlplane.StatusTable)
	if err != nil {
		return err
	}
	bodies, err := columnText(qr, "status_json")
	if err != nil {
		return err
	}
	for _, body := range bodies {
		if !json.Valid([]byte(body)) {
			return fmt.Errorf("replica status_json is not json")
		}
	}
	return nil
}

// ReplicatedReceipt reads one receipt from Jevons' file, as the broker last
// delivered it. The held-receipt sweep uses this instead of the usage
// socket.
func (c *Collector) ReplicatedReceipt(ctx context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
	if c == nil || c.replica == nil {
		return nil, fmt.Errorf("replicated receipt: no replica")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var bodies []string
	if err := c.replica.Read(func(db *sqlpipe.Database) error {
		qr, err := db.Query(
			"SELECT receipt_json FROM "+controlplane.ReceiptTable+" WHERE seat = ? AND session_id = ? AND request_id = ?",
			q.Seat, q.SessionID, q.RequestID)
		if err != nil {
			return err
		}
		bodies, err = columnText(qr, "receipt_json")
		return err
	}); err != nil {
		return nil, err
	}
	if len(bodies) == 0 {
		return nil, fmt.Errorf("replicated receipt absent")
	}
	receipt, err := ApplyReplicatedReceipt(bodies[0])
	if err != nil {
		return nil, err
	}
	if err := receipt.ValidateIdentity(q.Seat, q.SessionID, q.RequestID); err != nil {
		return nil, err
	}
	return &receipt, nil
}
