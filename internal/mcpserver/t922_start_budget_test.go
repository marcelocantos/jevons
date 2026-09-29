// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

type t922Sender struct{}

func (t922Sender) Alive() bool       { return true }
func (t922Sender) Interrupt() error  { return nil }
func (t922Sender) Send(string) error { return nil }

// t922Start runs jevons_agent_start for a seat whose opening brief is
// confirmed only when release closes, with verdict as the evidence.
func t922Start(t *testing.T, verdict TurnEvidence) (*Server, *claudia.Registry, *mcp.CallToolResult, chan struct{}, time.Duration) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) { return nil, nil }
	s.SetSenderResolver(func(n string) (agentSender, bool, error) {
		if n != "jv-t922-slow" {
			return nil, false, errors.New("unknown seat")
		}
		return t922Sender{}, false, nil
	})
	release := make(chan struct{})
	s.SetTurnWitness(func(string, string) turnWatch {
		return func() TurnEvidence { <-release; return verdict }
	})
	grace := time.Duration(0)
	s.startStallGrace = &grace
	retries := 0
	s.startStallRetries = &retries

	prev := startAnswerBudget
	startAnswerBudget = 50 * time.Millisecond
	t.Cleanup(func() { startAnswerBudget = prev })

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "jv-t922-slow", "workdir": dir, "provider": string(claudia.ProviderClaude),
		"purpose": "work", "parent": "jevons-po", "prompt": "Do the slow thing.",
	}
	began := time.Now()
	res, err := s.handleAgentStart(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	return s, reg, res, release, time.Since(began)
}

// 🎯T922: a seat whose first turn takes longer than the caller can wait gets
// a start answer anyway — "brief pending" — instead of the caller's tool
// timing out on a worker that launched.
func TestT922StartAnswersBeforeASlowBriefIsConfirmed(t *testing.T) {
	_, reg, res, release, took := t922Start(t, TurnEvidence{Observed: true, SessionEvent: true, Detail: "the agent published a session event"})
	defer close(release)
	if res.IsError {
		t.Fatalf("a slow brief was reported as a failed start: %s", toolText(res))
	}
	text := toolText(res)
	if !strings.Contains(text, "not confirmed within") || !strings.Contains(text, "🎯T922") {
		t.Fatalf("the answer does not say the brief is pending: %s", text)
	}
	if strings.Contains(text, "prompt_delivered=true") {
		t.Fatalf("claimed delivery before it was confirmed: %s", text)
	}
	// 🎯T97 exemption: an upper bound far above the 50 ms budget; a slow
	// host cannot make a call that answered on its budget fail this.
	if took > 10*time.Second {
		t.Fatalf("start took %s", took)
	}
	if reg.Def("jv-t922-slow") == nil {
		t.Fatal("the seat was not left registered while its brief is confirmed")
	}
}

// A brief that finally fails after the call answered still releases the seat
// (🎯T387): the verdict is not lost because the caller has moved on.
func TestT922LateBriefFailureStillReleasesTheSeat(t *testing.T) {
	_, reg, res, release, _ := t922Start(t, TurnEvidence{Observed: true, Durable: true, TranscriptAbsent: true,
		Detail: "no transcript was ever created"})
	if res.IsError {
		t.Fatalf("answered as failed before the verdict: %s", toolText(res))
	}
	close(release)
	for reg.Def("jv-t922-slow") != nil {
		time.Sleep(10 * time.Millisecond) // `go test -timeout` is the clock
	}
}
