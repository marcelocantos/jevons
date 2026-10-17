// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatpair is the per-seat sqlpipe pair for one MCP tool invocation.
// The MCP side commits the intent before it waits. Jevons writes the outcome.
// A shared pair for fleet desired-state is not this package.
package seatpair

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

const (
	// DirName is the directory under a Jevons state dir that holds one
	// subdirectory per seat.
	DirName = "seatpairs"
	// MCPFile is the MCP-owned database inside a seat directory.
	MCPFile = "mcp.db"
	// JevonsFile is the Jevons-owned database inside a seat directory.
	JevonsFile = "jevons.db"
	// GenerationFile is the current seat generation. A missing or different
	// value makes an intent stale.
	GenerationFile = "generation"

	// IntentTable is the MCP-owned tool intent. One row per operation.
	IntentTable = "mcp_tool_intent"
	// OutcomeTable is the Jevons-owned tool outcome. One row per operation.
	OutcomeTable = "jevons_tool_outcome"

	// ToolPing is the internal completion probe. It is not an owner-facing tool.
	ToolPing = "seatpair.ping"

	OutcomeAccepted  = "accepted"
	OutcomeStale     = "stale"
	OutcomeUncertain = "uncertain"
	OutcomeRejected  = "rejected"
)

const schema = `
CREATE TABLE IF NOT EXISTS mcp_tool_intent (
  id INTEGER PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE,
  seat TEXT NOT NULL,
  generation INTEGER NOT NULL,
  tool TEXT NOT NULL,
  args_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS jevons_tool_outcome (
  id INTEGER PRIMARY KEY,
  operation_id TEXT NOT NULL UNIQUE,
  seat TEXT NOT NULL,
  generation INTEGER NOT NULL,
  outcome TEXT NOT NULL,
  reason TEXT NOT NULL,
  body TEXT NOT NULL
);
`

// Intent is one committed tool call.
type Intent struct {
	OperationID string
	Seat        string
	Generation  int64
	Tool        string
	ArgsJSON    string
}

// Result is the durable outcome of one operation id.
type Result struct {
	Outcome string
	Reason  string
	Body    string
}

// Effect runs one intent. A nil effect records uncertainty and is not retried.
type Effect func(context.Context, Intent) Result

// CommitArgs is one intent to commit. An empty OperationID is minted here.
type CommitArgs struct {
	OperationID string
	Generation  int64
	Tool        string
	ArgsJSON    string
}

// Caller commits intents and waits on the same operation id.
type Caller struct {
	root string
	seat string
}

// OpenCaller creates the seat directory and rejects a name that is not a
// single path base.
func OpenCaller(root, seat string) (*Caller, error) {
	if err := checkSeat(seat); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, seat), 0o755); err != nil {
		return nil, err
	}
	return &Caller{root: root, seat: seat}, nil
}

// Commit writes the intent before any wait. The Jevons file may be absent.
func (c *Caller) Commit(_ context.Context, args *CommitArgs) (string, error) {
	if c == nil || args == nil {
		return "", fmt.Errorf("seat pair: nil commit")
	}
	if args.Generation <= 0 || strings.TrimSpace(args.Tool) == "" {
		return "", fmt.Errorf("seat pair: invalid intent")
	}
	body := args.ArgsJSON
	if body == "" {
		body = "{}"
	}
	if err := rejectSecrets(body); err != nil {
		return "", err
	}
	id := args.OperationID
	if id == "" {
		var err error
		id, err = newOperationID()
		if err != nil {
			return "", err
		}
	}
	db, err := openDB(c.mcpPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := db.Exec(schema); err != nil {
		return "", err
	}
	if err := db.Exec(`INSERT INTO mcp_tool_intent
		(operation_id, seat, generation, tool, args_json) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(operation_id) DO UPDATE SET
		  seat = excluded.seat, generation = excluded.generation,
		  tool = excluded.tool, args_json = excluded.args_json`,
		id, c.seat, args.Generation, args.Tool, body); err != nil {
		return "", err
	}
	return id, nil
}

// Wait polls the original operation id until its outcome is stored or ctx ends.
// It does not commit another intent.
func (c *Caller) Wait(ctx context.Context, operationID string) (Result, error) {
	if c == nil || strings.TrimSpace(operationID) == "" {
		return Result{}, fmt.Errorf("seat pair: nil wait")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		res, ok, err := c.lookup(operationID)
		if err != nil || ok {
			return res, err
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Call commits args and waits on the id Commit stored.
func (c *Caller) Call(ctx context.Context, args *CommitArgs) (Result, error) {
	id, err := c.Commit(ctx, args)
	if err != nil {
		return Result{}, err
	}
	return c.Wait(ctx, id)
}

// CatchUpDir completes intents under root. A missing or empty directory does
// not open a peer.
func CatchUpDir(ctx context.Context, root string, effect Effect) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			continue
		}
		if err := catchUpSeat(ctx, root, entry.Name(), effect); err != nil {
			return err
		}
	}
	return nil
}

func catchUpSeat(ctx context.Context, root, seat string, effect Effect) error {
	if err := checkSeat(seat); err != nil {
		return err
	}
	mcpPath := filepath.Join(root, seat, MCPFile)
	if _, err := os.Stat(mcpPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	mcpDB, err := openDB(mcpPath)
	if err != nil {
		return err
	}
	defer mcpDB.Close()
	jevDB, err := openDB(filepath.Join(root, seat, JevonsFile))
	if err != nil {
		return err
	}
	defer jevDB.Close()
	if err := mcpDB.Exec(schema); err != nil {
		return err
	}
	if err := jevDB.Exec(schema); err != nil {
		return err
	}
	if err := syncPair(mcpDB, jevDB); err != nil {
		return err
	}
	intents, err := readIntents(mcpDB)
	if err != nil {
		return err
	}
	outcomes, err := readOutcomes(jevDB)
	if err != nil {
		return err
	}
	current, haveGen, err := readGeneration(filepath.Join(root, seat, GenerationFile))
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, done := outcomes[intent.OperationID]; done {
			continue
		}
		var res Result
		if !haveGen || intent.Generation != current {
			res = Result{Outcome: OutcomeStale, Reason: "generation"}
		} else if effect == nil {
			res = Result{Outcome: OutcomeUncertain, Reason: "no effect"}
		} else {
			res = effect(ctx, intent)
			if res.Outcome == "" {
				res.Outcome = OutcomeUncertain
				if res.Reason == "" {
					res.Reason = "no effect"
				}
			}
		}
		if err := writeOutcome(jevDB, intent, res); err != nil {
			return err
		}
		outcomes[intent.OperationID] = res
	}
	return syncPair(mcpDB, jevDB)
}

func (c *Caller) lookup(operationID string) (Result, bool, error) {
	jevPath := c.jevonsPath()
	if _, err := os.Stat(jevPath); err != nil {
		if os.IsNotExist(err) {
			return Result{}, false, nil
		}
		return Result{}, false, err
	}
	mcpDB, err := openDB(c.mcpPath())
	if err != nil {
		return Result{}, false, err
	}
	defer mcpDB.Close()
	jevDB, err := openDB(jevPath)
	if err != nil {
		return Result{}, false, err
	}
	defer jevDB.Close()
	if err := mcpDB.Exec(schema); err != nil {
		return Result{}, false, err
	}
	if err := jevDB.Exec(schema); err != nil {
		return Result{}, false, err
	}
	if err := syncPair(mcpDB, jevDB); err != nil {
		return Result{}, false, err
	}
	outcomes, err := readOutcomes(jevDB)
	if err != nil {
		return Result{}, false, err
	}
	res, ok := outcomes[operationID]
	return res, ok, nil
}

func syncPair(mcpDB, jevonsDB *sqlpipe.Database) error {
	client, err := sqlpipe.NewPeer(mcpDB, sqlpipe.PeerConfig{OwnedTables: map[string]bool{IntentTable: true}})
	if err != nil {
		return err
	}
	defer client.Close()
	server, err := sqlpipe.NewPeer(jevonsDB, sqlpipe.PeerConfig{ApproveOwnership: approveMCPIntent})
	if err != nil {
		return err
	}
	defer server.Close()
	if err := sqlpipe.SyncPeerHandshake(client, server); err != nil {
		return err
	}
	if client.State() != sqlpipe.PeerLive || server.State() != sqlpipe.PeerLive {
		return fmt.Errorf("seat pair handshake not live: mcp=%d jevons=%d", client.State(), server.State())
	}
	for range 8 {
		n, err := flushInto(client, server)
		if err != nil {
			return err
		}
		m, err := flushInto(server, client)
		if err != nil {
			return err
		}
		if n == 0 && m == 0 {
			return nil
		}
	}
	return fmt.Errorf("seat pair: sync did not converge")
}

func approveMCPIntent(tables map[string]bool) bool {
	if len(tables) == 0 {
		return false
	}
	for name := range tables {
		if name != IntentTable {
			return false
		}
	}
	return true
}

func flushInto(from, to *sqlpipe.Peer) (int, error) {
	msgs, err := from.Flush()
	if err != nil {
		return 0, err
	}
	for _, om := range msgs {
		if _, err := to.HandleMessage(om.Msg); err != nil {
			return 0, err
		}
	}
	return len(msgs), nil
}

func readIntents(db *sqlpipe.Database) ([]Intent, error) {
	qr, err := db.Query("SELECT operation_id, seat, generation, tool, args_json FROM " + IntentTable + " ORDER BY operation_id")
	if err != nil {
		return nil, err
	}
	out := make([]Intent, 0, len(qr.Rows))
	for _, row := range qr.Rows {
		id, err := cellText(qr.Columns, row, "operation_id")
		if err != nil {
			return nil, err
		}
		seat, err := cellText(qr.Columns, row, "seat")
		if err != nil {
			return nil, err
		}
		generation, err := cellInt(qr.Columns, row, "generation")
		if err != nil {
			return nil, err
		}
		tool, err := cellText(qr.Columns, row, "tool")
		if err != nil {
			return nil, err
		}
		args, err := cellText(qr.Columns, row, "args_json")
		if err != nil {
			return nil, err
		}
		out = append(out, Intent{OperationID: id, Seat: seat, Generation: generation, Tool: tool, ArgsJSON: args})
	}
	return out, nil
}

func readOutcomes(db *sqlpipe.Database) (map[string]Result, error) {
	qr, err := db.Query("SELECT operation_id, outcome, reason, body FROM " + OutcomeTable)
	if err != nil {
		return nil, err
	}
	out := map[string]Result{}
	for _, row := range qr.Rows {
		id, err := cellText(qr.Columns, row, "operation_id")
		if err != nil {
			return nil, err
		}
		outcome, err := cellText(qr.Columns, row, "outcome")
		if err != nil {
			return nil, err
		}
		reason, err := cellText(qr.Columns, row, "reason")
		if err != nil {
			return nil, err
		}
		body, err := cellText(qr.Columns, row, "body")
		if err != nil {
			return nil, err
		}
		out[id] = Result{Outcome: outcome, Reason: reason, Body: body}
	}
	return out, nil
}

func writeOutcome(db *sqlpipe.Database, intent Intent, res Result) error {
	return db.Exec(`INSERT INTO jevons_tool_outcome
		(operation_id, seat, generation, outcome, reason, body) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(operation_id) DO UPDATE SET
		  outcome = excluded.outcome, reason = excluded.reason, body = excluded.body`,
		intent.OperationID, intent.Seat, intent.Generation, res.Outcome, res.Reason, res.Body)
}

func readGeneration(path string) (int64, bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("seat pair: generation: %w", err)
	}
	return n, true, nil
}

func openDB(path string) (*sqlpipe.Database, error) {
	return sqlpipe.OpenDatabase(path)
}

func (c *Caller) mcpPath() string {
	return filepath.Join(c.root, c.seat, MCPFile)
}

func (c *Caller) jevonsPath() string {
	return filepath.Join(c.root, c.seat, JevonsFile)
}

func newOperationID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func checkSeat(seat string) error {
	if seat == "" || seat == "." || seat == ".." || filepath.Base(seat) != seat {
		return fmt.Errorf("seat pair: seat %q is not a single path base", seat)
	}
	return nil
}

func rejectSecrets(body string) error {
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return fmt.Errorf("seat pair: args are not json: %w", err)
	}
	if secretKey(v) {
		return fmt.Errorf("seat pair: credential payload rejected")
	}
	return nil
}

func secretKey(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			switch strings.ToLower(k) {
			case "token", "access_token", "refresh_token", "api_key", "apikey", "secret", "password", "credential", "credentials", "authorization":
				return true
			}
			if secretKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if secretKey(child) {
				return true
			}
		}
	}
	return false
}

func cellText(cols []string, row []sqlpipe.Value, name string) (string, error) {
	v, err := cell(cols, row, name)
	if err != nil {
		return "", err
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case []byte:
		return string(t), nil
	default:
		return "", fmt.Errorf("seat pair: text column %s has type %T", name, v)
	}
}

func cellInt(cols []string, row []sqlpipe.Value, name string) (int64, error) {
	v, err := cell(cols, row, name)
	if err != nil {
		return 0, err
	}
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("seat pair: integer column %s has type %T", name, v)
	}
}

func cell(cols []string, row []sqlpipe.Value, name string) (any, error) {
	for i, c := range cols {
		if c == name {
			if i >= len(row) {
				return nil, fmt.Errorf("seat pair: column %s short", name)
			}
			return row[i], nil
		}
	}
	return nil, fmt.Errorf("seat pair: column %s missing", name)
}
