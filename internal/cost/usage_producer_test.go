// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/omp"
)

// This is explicit paired-source integration evidence, not a normal hermetic
// pass when skipped. Use the same frozen Claudia checkout as the Go module
// replacement, with its sidecar dependencies installed from the frozen lock:
// JEVONS_USAGE_INTEGRATION_CLAUDIA=/path/to/claudia bin/gate -- go test
// -mod=readonly -modfile=/tmp/paired.mod -race ./internal/cost
// -run '^TestUsageProducerCanonicalHistoryCatchUp$' -count=1
// The provider fixtures execute the real seat, maintenance, and provider parsers with fake fetch;
// the actual sidecar only handles provider-free discovery and history queries.
func TestUsageProducerCanonicalHistoryCatchUp(t *testing.T) {
	claudiaRoot := os.Getenv("JEVONS_USAGE_INTEGRATION_CLAUDIA")
	if claudiaRoot == "" {
		t.Skip("paired-source integration not enabled; set JEVONS_USAGE_INTEGRATION_CLAUDIA to the exact Claudia module replacement checkout")
	}
	claudiaRoot, err := filepath.Abs(claudiaRoot)
	if err != nil {
		t.Fatal(err)
	}
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("enabled integration requires Bun: ", err)
	}
	for _, fixture := range []canonicalUsageFixture{
		{name: "responses_retry", script: "sidecar/testdata/provider-usage-producer.ts", provider: "xai-oauth", input: 38, output: 5, fresh: 31, cache: 7, consumption: 43, invocations: 1, kinds: []string{"model", "model"}},
		{name: "openai_compaction", script: "sidecar/testdata/native-usage-producer.ts", args: []string{"success", "openai"}, provider: "openai", input: 128, output: 11, fresh: 81, cache: 47, consumption: 139, invocations: 2, kinds: []string{"compaction", "model"}},
		{name: "codex_compaction", script: "sidecar/testdata/native-usage-producer.ts", args: []string{"success", "codex"}, provider: "openai-codex", input: 128, output: 11, fresh: 81, cache: 47, consumption: 139, invocations: 2, kinds: []string{"compaction", "model"}},
	} {
		t.Run(fixture.name, func(t *testing.T) { checkCanonicalUsageProducer(t, claudiaRoot, bun, fixture) })
	}
}

type canonicalUsageFixture struct {
	name, script, provider                   string
	args                                     []string
	input, output, fresh, cache, consumption int64
	invocations                              int
	kinds                                    []string
}

func checkCanonicalUsageProducer(t *testing.T, claudiaRoot, bun string, want canonicalUsageFixture) {
	t.Helper()
	// macOS Unix-domain socket names must fit its short pathname limit.
	root, err := os.MkdirTemp("/tmp", "jv-usage-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	spoolRoot := filepath.Join(root, "lost-spool")
	fixture := exec.CommandContext(ctx, bun, append([]string{want.script, root}, want.args...)...)
	fixture.Dir = claudiaRoot
	// Whitelist process setup only: no inherited credentials, proxy settings,
	// provider switches, or home configuration can affect the fake transport.
	fixture.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "TMPDIR=" + root, "JEVONS_SPOOL_DIR=" + spoolRoot, "CLAUDIA_OMP_SPOOL_DIR=" + spoolRoot}
	if output, err := fixture.CombinedOutput(); err != nil {
		t.Fatalf("actual usage producer: %v\n%s", err, output)
	}
	// Lose every emitted event and spool mirror. Only canonical History may
	// recover the usage; no producer output is passed to Jevons below.
	if err := os.RemoveAll(spoolRoot); err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(root, "omp-seats", "seat", "generation.jsonl")
	originalHistory, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}

	socket := filepath.Join(root, "sidecar.sock")
	t.Setenv(omp.SocketEnv, socket)
	logPath := filepath.Join(root, "sidecar.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	server := exec.CommandContext(ctx, bun, "sidecar/server.ts", socket)
	server.Dir, server.Env = claudiaRoot, fixture.Env
	server.Stdout, server.Stderr = log, log
	if err := server.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait(); _ = log.Close() })
	ready, stopReady := context.WithTimeout(ctx, 5*time.Second)
	defer stopReady()
	for {
		conn, err := (&net.Dialer{}).DialContext(ready, "unix", socket)
		if err == nil {
			_ = conn.Close()
			break
		}
		select {
		case <-ready.Done():
			output, _ := os.ReadFile(logPath)
			t.Fatalf("isolated sidecar did not start: %v\n%s", err, output)
		case <-time.After(10 * time.Millisecond):
		}
	}

	var queries []claudia.UsageQuery
	discoveries := 0
	now := time.Now()
	source := &UsageSource{
		Discover: func(ctx context.Context, q claudia.GenerationQuery) (*claudia.GenerationPage, error) {
			discoveries++
			return claudia.DiscoverGenerationsDirect(ctx, q)
		},
		Query: func(ctx context.Context, q claudia.UsageQuery) (*claudia.UsagePage, error) {
			queries = append(queries, q)
			return claudia.QueryUsageDirect(ctx, q)
		},
	}
	path := filepath.Join(root, "cost.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRequestMission(ctx, "seat", "logical-A", "T1074"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRequestMission(ctx, "seat", "logical-A", "T9999"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if store != nil {
			_ = store.Close()
		}
	}()
	assertUsage := func() {
		t.Helper()
		got, err := store.StructuredUsage(ctx, time.Unix(0, 0), time.Now().Add(time.Hour))
		// Frozen wire specimens: retries consume 38 inclusive input + 5 output;
		// native compaction plus an ordinary turn consume 128 + 11. The
		// separately exposed cache buckets must not be added to inclusive input.
		if err != nil || got.Calls != 2 || got.KnownInputTotal != want.input || got.Known.Output != want.output || got.Known.Input != want.fresh || got.Known.CacheRead != want.cache || got.KnownConsumptionTokens() != want.consumption {
			t.Fatalf("actual producer usage lost or double-counted: %+v, %v", got, err)
		}
		if got.IncompleteCalls != 0 || got.UnknownInputTotal != 0 || got.UnknownOutput != 0 || got.UnknownCost != 2 {
			t.Fatalf("availability changed: %+v", got)
		}
		// Freeze the report clock to the observed facts, not host scheduling.
		// A fixture spanning local midnight legitimately has a smaller today.
		var last string
		if err := store.db.QueryRowContext(ctx, `SELECT MAX(ts) FROM usage_calls`).Scan(&last); err != nil {
			t.Fatal(err)
		}
		asOf, err := time.Parse(usageTimeLayout, last)
		if err != nil {
			t.Fatal(err)
		}
		asOf = asOf.In(time.Local).Add(time.Second)
		midnight := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, asOf.Location())
		today, err := store.StructuredUsage(ctx, midnight, asOf)
		if err != nil {
			t.Fatal(err)
		}
		monitor := NewMonitor(&MonitorArgs{Store: store, Config: DefaultBudgetConfig, Now: func() time.Time { return asOf }})
		snap, err := monitor.Snapshot()
		if err != nil || snap.StructuredUsage != got || snap.StructuredUsageToday != today {
			t.Fatalf("cost API did not expose canonical provider facts: %+v, %v", snap, err)
		}
		if cursor, err := store.UsageCursor(ctx, "seat", "generation"); err != nil || cursor != 4 {
			t.Fatalf("canonical cursor=%d err=%v", cursor, err)
		}
	}
	collector := NewCollector(&CollectorArgs{Store: store, UsageSource: source, Now: func() time.Time { return now }})
	if err := collector.CatchUpOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertUsage()
	if len(queries) != 1 || queries[0].Seat != "seat" || queries[0].SessionID != "generation" || queries[0].After != 0 {
		t.Fatalf("unregistered history was not discovered: %+v", queries)
	}
	// A healthy collector polls usage every three seconds without allocating a
	// fresh discovery scan each time. The real sidecar retains only four scans
	// for sixty seconds; this used to hit DiscoveryBusy on the fifth pass.
	for range 5 {
		now = now.Add(3 * time.Second)
		if err := collector.CatchUpOnce(ctx); err != nil {
			t.Fatal(err)
		}
		assertUsage()
	}
	if discoveries != 1 || len(queries) != 6 {
		t.Fatalf("replay cadence started extra retained scans: discovery=%d usage=%d", discoveries, len(queries))
	}
	now = now.Add(16 * time.Second)
	if err := collector.CatchUpOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if discoveries != 2 || len(queries) != 7 {
		t.Fatalf("inventory did not refresh after cadence: discovery=%d usage=%d", discoveries, len(queries))
	}
	assertUsage()
	rows, err := store.db.QueryContext(ctx, `SELECT fact FROM usage_calls ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	invocations := map[string]bool{}
	var kinds []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		var fact claudia.UsageFact
		if err := json.Unmarshal([]byte(raw), &fact); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		wantRefs := []claudia.UsageRequestRef{{RequestID: "logical-A", AttemptID: "attempt-A"}}
		if fact.Coverage != "provider_attempt" || fact.CallID == "" || fact.CallID != fact.ProviderAttemptID || fact.InvocationID == "" || fact.Seat != "seat" || fact.SessionID != "generation" || fact.TurnID != "turn-A" || fact.Provider != want.provider || fact.State != "completed" || !reflect.DeepEqual(fact.Requests, wantRefs) {
			rows.Close()
			t.Fatalf("producer attribution lost: %+v", fact)
		}
		invocations[fact.InvocationID] = true
		kinds = append(kinds, fact.Kind)
		ids[fact.CallID] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(ids) != 2 || len(invocations) != want.invocations || !reflect.DeepEqual(kinds, want.kinds) {
		t.Fatalf("physical attempts, invocation grouping, or compaction attribution changed: ids=%v invocations=%v kinds=%v", ids, invocations, kinds)
	}
	mission, ok, err := store.UsageMission(ctx, &claudia.UsageFact{Seat: "seat", Requests: []claudia.UsageRequestRef{{RequestID: "logical-A", AttemptID: "attempt-A"}}})
	if err != nil || !ok || mission != "T1074" {
		t.Fatalf("producer request was not attributed to the frozen mission: %q %v %v", mission, ok, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	collector = NewCollector(&CollectorArgs{Store: store, UsageSource: source, Now: func() time.Time { return now }})
	for range 2 {
		if err := collector.CatchUpOnce(ctx); err != nil {
			t.Fatal(err)
		}
		assertUsage()
	}
	if len(queries) != 9 || queries[7].After != 4 || queries[8].After != 4 || discoveries != 3 {
		t.Fatalf("database reopen lost durable replay position: %+v", queries)
	}
	// Query replies may leave usage_page diagnostic rows in the fresh spool,
	// but there must be no replayed usage facts or snapshots. CatchUpOnce never
	// polls this directory: its only input above is the canonical query API.
	entries, err := os.ReadDir(spoolRoot)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(spoolRoot, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var record struct {
				UsageFact json.RawMessage `json:"usage_fact"`
				Snapshot  json.RawMessage `json:"snapshot"`
			}
			if err := json.Unmarshal(line, &record); err != nil {
				t.Fatal(err)
			}
			if len(record.UsageFact) != 0 || len(record.Snapshot) != 0 {
				t.Fatal("canonical query regenerated accounting events in spool")
			}
		}
	}
	currentHistory, err := os.ReadFile(historyPath)
	if err != nil || string(currentHistory) != string(originalHistory) {
		t.Fatalf("read-only recovery changed canonical history: %v", err)
	}
}
