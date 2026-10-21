// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/seatpair"
)

// A known seat's tools/call commits on its pair and the registered handler
// runs once. A call with no seat header stays on the direct handler and
// does not write an intent. An existing generation file is not replaced.
func TestT107410KnownSeatCallCommitsAndRunsOnce(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, nil, nil)
	s.stateDir = dir
	var mu sync.Mutex
	var runs []string
	s.addTool(mcp.NewTool("t107410_probe", mcp.WithString("text")), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, _ := req.GetArguments()["text"].(string)
		mu.Lock()
		runs = append(runs, text)
		mu.Unlock()
		return mcp.NewToolResultText("ran-" + text), nil
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	root := filepath.Join(dir, seatpair.DirName)
	if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(root, "jevons", seatpair.GenerationFile)
	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paired := callSeatProbe(t, ts.URL, "jevons", "paired")
	if !strings.Contains(paired, "ran-paired") {
		t.Fatalf("paired response = %s", paired)
	}
	direct := callSeatProbe(t, ts.URL, "", "direct")
	if !strings.Contains(direct, "ran-direct") {
		t.Fatalf("direct response = %s", direct)
	}
	if err := seatpair.CatchUpDir(context.Background(), root, s.ExecuteSeatPair); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	gotRuns := append([]string(nil), runs...)
	mu.Unlock()
	if strings.Join(gotRuns, ",") != "paired,direct" {
		t.Fatalf("handler runs = %v, want paired once then the direct call", gotRuns)
	}
	intents, outcomes, err := seatpair.Inspect(root, "jevons")
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].Tool != "t107410_probe" || intents[0].Generation != 4 || !strings.Contains(intents[0].ArgsJSON, "paired") {
		t.Fatalf("intents = %+v", intents)
	}
	if strings.Contains(intents[0].ArgsJSON, "direct") {
		t.Fatalf("direct call was committed: %+v", intents[0])
	}
	res, ok := outcomes[intents[0].OperationID]
	if !ok || res.Outcome != seatpair.OutcomeAccepted || res.Reason != "ok" || res.Body != "ran-paired" {
		t.Fatalf("outcome = %+v ok=%v", res, ok)
	}
	gen, err := os.ReadFile(kept)
	if err != nil {
		t.Fatal(err)
	}
	if string(gen) != "4\n" {
		t.Fatalf("generation = %q, want the existing file", gen)
	}
}

func callSeatProbe(t *testing.T, url, seat, text string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "t107410_probe",
			"arguments": map[string]any{"text": text},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if seat != "" {
		req.Header.Set(SeatHeader, seat)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP %d: %s", resp.StatusCode, out)
	}
	return string(out)
}
