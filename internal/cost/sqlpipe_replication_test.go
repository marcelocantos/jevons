// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/claudia/omp"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// TestT1074SqlpipeUsageAndReceiptReplication drives the real native usage
// producer through Claudia's usage and receipt queries, file-backed sqlpipe
// replication, and the Jevons accounting consumer. A missing Bun or toolchain
// is a failure. This does not achieve T1074.2 or T1074.3: the daemon still
// polls the socket, and the seat reconciler and per-seat MCP pairs are absent.
func TestT1074SqlpipeUsageAndReceiptReplication(t *testing.T) {
	t.Run("rejects foreign ownership", func(t *testing.T) {
		dir := t.TempDir()
		clientDB := openControlFile(t, filepath.Join(dir, "client.db"))
		serverDB := openControlFile(t, filepath.Join(dir, "server.db"))
		if err := clientDB.Exec("CREATE TABLE jevons_secret (id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
			t.Fatal(err)
		}
		if err := serverDB.Exec("CREATE TABLE jevons_secret (id INTEGER PRIMARY KEY, val TEXT)"); err != nil {
			t.Fatal(err)
		}
		client, server := newFactPeers(t, clientDB, serverDB, map[string]bool{
			controlplane.UsageTable: true, controlplane.ReceiptTable: true, "jevons_secret": true,
		})
		err := sqlpipe.SyncPeerHandshake(client, server)
		if client.State() == sqlpipe.PeerLive || server.State() == sqlpipe.PeerLive {
			t.Fatalf("foreign table reached live: err=%v client=%d server=%d", err, client.State(), server.State())
		}
		if server.State() != sqlpipe.PeerError {
			t.Fatalf("foreign table server state=%d err=%v", server.State(), err)
		}
	})

	t.Run("rejects empty ownership", func(t *testing.T) {
		dir := t.TempDir()
		clientDB := openControlFile(t, filepath.Join(dir, "client.db"))
		serverDB := openControlFile(t, filepath.Join(dir, "server.db"))
		if approveClaudiaTables(nil) || approveClaudiaTables(map[string]bool{}) {
			t.Fatal("empty ownership was approved")
		}
		client, server := newFactPeers(t, clientDB, serverDB, map[string]bool{})
		err := sqlpipe.SyncPeerHandshake(client, server)
		if client.State() == sqlpipe.PeerLive || server.State() == sqlpipe.PeerLive {
			t.Fatalf("empty ownership reached live: err=%v client=%d server=%d", err, client.State(), server.State())
		}
		// An empty claim never becomes a handshake the server can reject.
		// sqlpipe refuses it before Live; a claim that does arrive must
		// still land the server in PeerError.
		if err == nil && server.State() != sqlpipe.PeerError {
			t.Fatalf("empty ownership server state=%d err=%v", server.State(), err)
		}
	})

	t.Run("rejects invalid usage fact", func(t *testing.T) {
		db := openControlFile(t, filepath.Join(t.TempDir(), "claudia.db"))
		if err := controlplane.PublishUsageFacts(db, []claudia.UsageFact{{Seat: "seat", SessionID: "generation"}}); err == nil {
			t.Fatal("invalid usage fact was published")
		}
		qr, err := db.Query("SELECT COUNT(*) FROM " + controlplane.UsageTable)
		if err != nil {
			t.Fatal(err)
		}
		if len(qr.Rows) != 1 || qr.Rows[0][0] != int64(0) {
			t.Fatalf("invalid fact rows = %+v", qr.Rows)
		}
	})

	t.Run("replicates producer facts across reconnect", func(t *testing.T) {
		replicateProducerFacts(t)
	})
}

func replicateProducerFacts(t *testing.T) {
	t.Helper()
	claudiaRoot := claudiaCleanupRoot(t)
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("sqlpipe replication requires Bun: ", err)
	}
	root, err := os.MkdirTemp("/tmp", "jv-sqlp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	producer := exec.CommandContext(ctx, bun, "sidecar/testdata/native-usage-producer.ts", root, "success", "openai")
	producer.Dir = claudiaRoot
	spoolRoot := filepath.Join(root, "lost-spool")
	producer.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + root,
		"TMPDIR=" + root,
		"JEVONS_SPOOL_DIR=" + spoolRoot,
		"CLAUDIA_OMP_SPOOL_DIR=" + spoolRoot,
	}
	if output, err := producer.CombinedOutput(); err != nil {
		t.Fatalf("native usage producer: %v\n%s", err, output)
	}

	socket := filepath.Join(root, "sidecar.sock")
	t.Setenv(omp.SocketEnv, socket)
	logPath := filepath.Join(root, "sidecar.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	server := exec.CommandContext(ctx, bun, "sidecar/server.ts", socket)
	server.Dir, server.Env = claudiaRoot, producer.Env
	server.Stdout, server.Stderr = logFile, logFile
	if err := server.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if server.Process != nil {
			_ = server.Process.Kill()
			_, _ = server.Process.Wait()
		}
		_ = logFile.Close()
	})
	waitForSocket(t, ctx, socket, logPath)

	var facts []claudia.UsageFact
	after := uint64(0)
	for {
		page, err := claudia.QueryUsageDirect(ctx, claudia.UsageQuery{
			Seat: "seat", SessionID: "generation", After: after, Limit: claudia.UsagePageLimit,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := page.Validate("seat", "generation", after, claudia.UsagePageLimit); err != nil {
			t.Fatal(err)
		}
		facts = append(facts, page.Facts...)
		after = page.Next
		if !page.More {
			break
		}
	}
	if len(facts) < 2 {
		t.Fatalf("producer page has %d facts", len(facts))
	}
	receipt, err := claudia.QueryReceiptDirect(ctx, claudia.ReceiptQuery{
		Seat: "seat", SessionID: "generation", RequestID: "logical-A",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State == claudia.ReceiptUnknown || receipt.RequestID != "logical-A" {
		t.Fatalf("producer receipt is not a durable logical request: %+v", receipt)
	}

	claudiaPath := filepath.Join(root, "claudia.db")
	jevonsPath := filepath.Join(root, "jevons.db")
	claudiaDB := openControlFile(t, claudiaPath)
	jevonsDB := openControlFile(t, jevonsPath)
	writer, reader := newFactPeers(t, claudiaDB, jevonsDB, map[string]bool{
		controlplane.UsageTable: true, controlplane.ReceiptTable: true,
	})
	if err := sqlpipe.SyncPeerHandshake(writer, reader); err != nil {
		t.Fatal(err)
	}
	if writer.State() != sqlpipe.PeerLive || reader.State() != sqlpipe.PeerLive {
		t.Fatalf("fact peers not live: writer=%d reader=%d", writer.State(), reader.State())
	}
	if _, err := reader.Subscribe("SELECT seat, session_id, seq, call_id, kind, fact_json FROM " + controlplane.UsageTable + " ORDER BY seat, session_id, seq"); err != nil {
		t.Fatal(err)
	}

	if err := controlplane.PublishUsageFacts(claudiaDB, facts[:1]); err != nil {
		t.Fatal(err)
	}
	liveRows := subscriptionRows(t, writer, reader)
	if len(liveRows) != 1 {
		t.Fatalf("live prefix delivered %d subscription results", len(liveRows))
	}

	storePath := filepath.Join(root, "cost.db")
	store, err := OpenStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			_ = store.Close()
		}
	})
	if err := store.RecordRequestMission(ctx, "seat", "logical-A", "T1074"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRequestMission(ctx, "seat", "logical-A", "T9999"); err != nil {
		t.Fatal(err)
	}
	ingestFactRows(t, ctx, store, liveRows)

	closePeers(t, writer, reader)
	closeDBs(t, claudiaDB, jevonsDB)

	claudiaDB = openControlFile(t, claudiaPath)
	if err := controlplane.PublishUsageFacts(claudiaDB, facts[1:]); err != nil {
		t.Fatal(err)
	}
	if err := controlplane.PublishReceipt(claudiaDB, receipt); err != nil {
		t.Fatal(err)
	}
	closeDBs(t, claudiaDB)

	claudiaDB = openControlFile(t, claudiaPath)
	jevonsDB = openControlFile(t, jevonsPath)
	writer, reader = newFactPeers(t, claudiaDB, jevonsDB, map[string]bool{
		controlplane.UsageTable: true, controlplane.ReceiptTable: true,
	})
	if err := sqlpipe.SyncPeerHandshake(writer, reader); err != nil {
		t.Fatal(err)
	}
	if writer.State() != sqlpipe.PeerLive || reader.State() != sqlpipe.PeerLive {
		t.Fatalf("reconnect peers not live: writer=%d reader=%d", writer.State(), reader.State())
	}
	usageQR, err := jevonsDB.Query("SELECT seat, session_id, seq, call_id, kind, fact_json FROM " + controlplane.UsageTable + " ORDER BY seat, session_id, seq")
	if err != nil {
		t.Fatal(err)
	}
	if len(usageQR.Rows) != len(facts) {
		t.Fatalf("reconnect replica has %d usage rows, producer has %d", len(usageQR.Rows), len(facts))
	}
	receiptQR, err := jevonsDB.Query("SELECT seat, session_id, request_id, state, receipt_json FROM " + controlplane.ReceiptTable)
	if err != nil {
		t.Fatal(err)
	}
	if len(receiptQR.Rows) != 1 {
		t.Fatalf("reconnect replica receipts = %+v", receiptQR.Rows)
	}
	receiptJSON, ok := valueString(receiptQR.Rows[0][columnIndex(t, receiptQR.Columns, "receipt_json")])
	if !ok {
		t.Fatalf("receipt_json type %T", receiptQR.Rows[0][columnIndex(t, receiptQR.Columns, "receipt_json")])
	}
	gotReceipt, err := ApplyReplicatedReceipt(receiptJSON)
	if err != nil {
		t.Fatal(err)
	}
	if gotReceipt.RequestID != receipt.RequestID || gotReceipt.State != receipt.State || gotReceipt.AttemptID != receipt.AttemptID || gotReceipt.Seat != "seat" || gotReceipt.SessionID != "generation" {
		t.Fatalf("replicated receipt %+v, queried %+v", gotReceipt, receipt)
	}

	bodies := factBodies(t, usageQR)
	for _, body := range bodies {
		if err := ApplyReplicatedUsage(ctx, store, body); err != nil {
			t.Fatal(err)
		}
	}
	assertCanonicalOpenAI(t, ctx, store)
	for _, body := range bodies {
		if err := ApplyReplicatedUsage(ctx, store, body); err != nil {
			t.Fatal(err)
		}
	}
	assertCanonicalOpenAI(t, ctx, store)

	if _, err := reader.Subscribe("SELECT seat, session_id, seq, call_id, kind, fact_json FROM " + controlplane.UsageTable + " ORDER BY seat, session_id, seq"); err != nil {
		t.Fatal(err)
	}
	if err := controlplane.PublishUsageFacts(claudiaDB, facts[len(facts)-1:]); err != nil {
		t.Fatal(err)
	}
	again := subscriptionRows(t, writer, reader)
	ingestFactRows(t, ctx, store, again)
	assertCanonicalOpenAI(t, ctx, store)
}

func assertCanonicalOpenAI(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	got, err := store.StructuredUsage(ctx, time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil || got.Calls != 2 || got.KnownInputTotal != 128 || got.Known.Output != 11 || got.Known.Input != 81 || got.Known.CacheRead != 47 || got.KnownConsumptionTokens() != 139 {
		t.Fatalf("replicated usage lost or double-counted: %+v %v", got, err)
	}
	if got.IncompleteCalls != 0 || got.UnknownInputTotal != 0 || got.UnknownOutput != 0 || got.UnknownCost != 2 {
		t.Fatalf("replicated availability changed: %+v", got)
	}
	rows, err := store.db.QueryContext(ctx, `SELECT fact FROM usage_calls ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var kinds []string
	ids := map[string]bool{}
	invocations := map[string]bool{}
	var attributed *claudia.UsageFact
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var fact claudia.UsageFact
		if err := json.Unmarshal([]byte(raw), &fact); err != nil {
			t.Fatal(err)
		}
		wantRefs := []claudia.UsageRequestRef{{RequestID: "logical-A", AttemptID: "attempt-A"}}
		if fact.Coverage != "provider_attempt" || fact.CallID == "" || fact.CallID != fact.ProviderAttemptID || fact.InvocationID == "" || fact.Seat != "seat" || fact.SessionID != "generation" || fact.TurnID != "turn-A" || fact.Provider != "openai" || fact.State != "completed" || !reflect.DeepEqual(fact.Requests, wantRefs) {
			t.Fatalf("replicated attribution lost: %+v", fact)
		}
		if fact.Kind != "compaction" && fact.Kind != "model" {
			t.Fatalf("replicated kind %q", fact.Kind)
		}
		kinds = append(kinds, fact.Kind)
		ids[fact.CallID] = true
		invocations[fact.InvocationID] = true
		attributed = &fact
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || len(invocations) != 2 || !reflect.DeepEqual(kinds, []string{"compaction", "model"}) {
		t.Fatalf("replicated attempts or compaction changed: ids=%v invocations=%v kinds=%v", ids, invocations, kinds)
	}
	mission, ok, err := store.UsageMission(ctx, attributed)
	if err != nil || !ok || mission != "T1074" {
		t.Fatalf("frozen mission = %q ok=%v err=%v", mission, ok, err)
	}
}

func claudiaCleanupRoot(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("JEVONS_USAGE_INTEGRATION_CLAUDIA"); p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() {
			t.Fatalf("JEVONS_USAGE_INTEGRATION_CLAUDIA is not a directory: %s", abs)
		}
		return abs
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("build info unavailable; sqlpipe replication needs the cleanup Claudia module")
	}
	for _, m := range info.Deps {
		if m.Path != "github.com/marcelocantos/claudia" {
			continue
		}
		if m.Replace == nil || m.Replace.Path == "" {
			t.Fatal("claudia module has no local replace; run with the paired modfile")
		}
		st, err := os.Stat(m.Replace.Path)
		if err != nil || !st.IsDir() {
			t.Fatalf("claudia replace path is not a directory: %s", m.Replace.Path)
		}
		return m.Replace.Path
	}
	t.Fatal("claudia module missing from build info")
	return ""
}

func openControlFile(t *testing.T, path string) *sqlpipe.Database {
	t.Helper()
	db, err := sqlpipe.OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := controlplane.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func newFactPeers(t *testing.T, clientDB, serverDB *sqlpipe.Database, owned map[string]bool) (*sqlpipe.Peer, *sqlpipe.Peer) {
	t.Helper()
	client, err := sqlpipe.NewPeer(clientDB, sqlpipe.PeerConfig{OwnedTables: owned})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server, err := sqlpipe.NewPeer(serverDB, sqlpipe.PeerConfig{ApproveOwnership: approveClaudiaTables})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return client, server
}

func approveClaudiaTables(tables map[string]bool) bool {
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

func subscriptionRows(t *testing.T, writer, reader *sqlpipe.Peer) []sqlpipe.QueryResult {
	t.Helper()
	msgs, err := writer.Flush()
	if err != nil {
		t.Fatal(err)
	}
	var subs []sqlpipe.QueryResult
	for _, om := range msgs {
		hr, err := reader.HandleMessage(om.Msg)
		if err != nil {
			t.Fatal(err)
		}
		subs = append(subs, hr.Subscriptions...)
	}
	return subs
}

func ingestFactRows(t *testing.T, ctx context.Context, store *Store, results []sqlpipe.QueryResult) {
	t.Helper()
	for _, qr := range results {
		for _, body := range factBodies(t, qr) {
			if err := ApplyReplicatedUsage(ctx, store, body); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func factBodies(t *testing.T, qr sqlpipe.QueryResult) []string {
	t.Helper()
	seqIdx := columnIndex(t, qr.Columns, "seq")
	factIdx := columnIndex(t, qr.Columns, "fact_json")
	type item struct {
		seq  int64
		body string
	}
	items := make([]item, 0, len(qr.Rows))
	for _, row := range qr.Rows {
		seq, ok := row[seqIdx].(int64)
		if !ok {
			t.Fatalf("seq type %T", row[seqIdx])
		}
		body, ok := valueString(row[factIdx])
		if !ok {
			t.Fatalf("fact_json type %T", row[factIdx])
		}
		items = append(items, item{seq: seq, body: body})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].seq < items[j].seq })
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].body
	}
	return out
}

func columnIndex(t *testing.T, cols []string, name string) int {
	t.Helper()
	for i, col := range cols {
		if col == name {
			return i
		}
	}
	t.Fatalf("column %s missing from %v", name, cols)
	return -1
}

func valueString(v sqlpipe.Value) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	default:
		return "", false
	}
}

func waitForSocket(t *testing.T, ctx context.Context, socket, logPath string) {
	t.Helper()
	ready, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	for {
		conn, err := (&net.Dialer{}).DialContext(ready, "unix", socket)
		if err == nil {
			_ = conn.Close()
			return
		}
		select {
		case <-ready.Done():
			output, _ := os.ReadFile(logPath)
			t.Fatalf("isolated sidecar did not start: %v\n%s", err, output)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func closePeers(t *testing.T, peers ...*sqlpipe.Peer) {
	t.Helper()
	for _, p := range peers {
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func closeDBs(t *testing.T, dbs ...*sqlpipe.Database) {
	t.Helper()
	for _, db := range dbs {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
