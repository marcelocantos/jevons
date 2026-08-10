// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/discovery"
)

func writeGrokSession(t *testing.T, root, workdir, sid string, turns ...string) {
	t.Helper()
	dir := filepath.Join(root, discovery.EncodeCWDBucket(workdir), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range turns {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func turn(in, calls int) string {
	return fmt.Sprintf(`{"timestamp":1786154000,"method":"_x.ai/session/update","params":{"sessionId":"s","update":{"sessionUpdate":"turn_completed","prompt_id":"p","stop_reason":"end_turn","usage":{"inputTokens":%d,"outputTokens":10,"cachedReadTokens":0,"modelCalls":%d,"costUsdTicks":1,"modelUsage":{"grok-4.5-build":{"totalTokens":%d}}}}}}`, in, calls, in)
}

// Context is per call, not per turn: a turn that billed 4 calls at 90k
// each is a 90k context, and treating its 360k total as the context would
// compact an agent that is nowhere near the ceiling.
func TestObserveReadsContextPerCallFromTheLastTurn(t *testing.T) {
	root := t.TempDir()
	wd := "/tmp/work"
	writeGrokSession(t, root, wd, "s", turn(50_000, 1), turn(360_000, 4))
	o := Observer{Roots: discovery.Roots{GrokSessions: root}}
	obs := o.Observe(AgentRef{Name: "a", Provider: "grok", WorkDir: wd, SessionID: "s"})
	if !obs.HasContext || obs.Context != 90_000 {
		t.Fatalf("context=%d has=%v want 90000/true", obs.Context, obs.HasContext)
	}
	if v := (Policy{}).Evaluate(obs).Verdict; v != VerdictOK {
		t.Fatalf("verdict=%s want ok — 90k is under the 100k default", v)
	}
}

// A session with no readable usage yields "unknown", never a zero that
// would read as a small context.
func TestObserveMissingSessionIsUnknownNotZero(t *testing.T) {
	o := Observer{Roots: discovery.Roots{GrokSessions: t.TempDir()}}
	obs := o.Observe(AgentRef{Name: "cold", Provider: "grok", WorkDir: "/nope", SessionID: "missing"})
	if obs.HasContext {
		t.Fatal("missing session reported a context")
	}
	if v := (Policy{}).Evaluate(obs).Verdict; v != VerdictUnknown {
		t.Fatalf("verdict=%s want unknown", v)
	}
}

// The tail window is bounded, so a huge log must still yield the last
// frame rather than falling back to unknown.
func TestObserveFindsLastFrameInALargeLog(t *testing.T) {
	root := t.TempDir()
	wd := "/tmp/big"
	lines := make([]string, 0, 400)
	for i := 0; i < 380; i++ {
		lines = append(lines, `{"noise":"`+string(make([]byte, 1024))+`"}`)
	}
	lines = append(lines, turn(250_000, 1))
	writeGrokSession(t, root, wd, "s", lines...)
	o := Observer{Roots: discovery.Roots{GrokSessions: root}}
	obs := o.Observe(AgentRef{Name: "a", Provider: "grok", WorkDir: wd, SessionID: "s"})
	if !obs.HasContext || obs.Context != 250_000 {
		t.Fatalf("context=%d has=%v want 250000/true", obs.Context, obs.HasContext)
	}
	if v := (Policy{}).Evaluate(obs).Verdict; v != VerdictCompact {
		t.Fatalf("verdict=%s want compact", v)
	}
}

// The bug that made 🎯T392.1 inert on Claude for its entire first day.
//
// Claude reports input_tokens FRESH-ONLY with cache reads alongside, so
// reading Usage.Input directly makes an agent carrying 300k of cached
// conversation look like it is carrying 12 tokens. It never crosses the
// ceiling, never compacts, and the lever silently does nothing — which is
// exactly what the post-change measurement found: mean context RISING to
// 205k under a supposedly-enforced 100k cap.
func TestObserveCountsClaudeCacheReadsAsContext(t *testing.T) {
	root := t.TempDir()
	wd := "/tmp/claudework"
	dir := filepath.Join(root, discovery.EncodeClaudeProject(wd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real Claude assistant frame: tiny fresh input, the conversation
	// itself served from cache.
	line := `{"timestamp":"2026-08-10T02:00:00Z","sessionId":"s","requestId":"r1","type":"assistant","message":{"id":"m1","model":"claude-opus-5","stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":400,"cache_read_input_tokens":300000,"cache_creation_input_tokens":5000}}}`
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	o := Observer{Roots: discovery.Roots{ClaudeProjects: root}}
	obs := o.Observe(AgentRef{Name: "jevons", Provider: "claude", WorkDir: wd, SessionID: "s"})
	if !obs.HasContext {
		t.Fatal("no context observed from a Claude frame")
	}
	if obs.Context != 305_012 {
		t.Fatalf("context=%d want 305012 (12 fresh + 300000 cached + 5000 created)", obs.Context)
	}
	if v := (Policy{}).Evaluate(obs).Verdict; v != VerdictCompact {
		t.Fatalf("verdict=%s want compact — 305k is far over the 100k default", v)
	}
}
