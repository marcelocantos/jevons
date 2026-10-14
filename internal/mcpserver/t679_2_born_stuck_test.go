// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// 🎯T679.2 oracle: fake clock + real temporary transcript path through the
// scout's eight cases (docs/audits/t679-born-stuck-scout.md).

const t679_2SID = "019f4f4b-945a-7a23-ba4c-51a0c26e0fd0"

type t679Clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *t679Clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *t679Clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type t679_2Env struct {
	t          *testing.T
	dir        string
	home       string
	work       string
	s          *Server
	reg        *claudia.Registry
	clock      *t679Clock
	child      *fakeSender
	parent     *fakeSender
	name       string
	parentName string
	sid        string
}

func t679_2Harness(t *testing.T, provider claudia.Provider, sid string) *t679_2Env {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const parentName = "jevons-po"
	const name = "jv-t679.2-seat"
	if err := reg.Register(claudia.AgentDef{
		Name: parentName, WorkDir: work, SessionID: "s-po",
		Purpose: claudia.PurposeWork, Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: work, SessionID: sid,
		Purpose: claudia.PurposeWork, Parent: parentName, Provider: provider,
	}); err != nil {
		t.Fatal(err)
	}
	clock := &t679Clock{t: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)}
	child := &fakeSender{alive: true}
	parent := &fakeSender{alive: true}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSendQueueDir(dir)
	s.SetBirthClock(clock.now)
	s.SetSeatAliveFn(func(n string) bool { return n == name })
	setObservedSenderResolver(s, func(n string) (agentSender, bool, error) {
		switch n {
		case name:
			return child, false, nil
		case parentName:
			return parent, false, nil
		default:
			return nil, false, fmt.Errorf("unknown %s", n)
		}
	})
	return &t679_2Env{
		t: t, dir: dir, home: home, work: work, s: s, reg: reg, clock: clock,
		child: child, parent: parent, name: name, parentName: parentName, sid: sid,
	}
}

func (e *t679_2Env) accept() {
	e.t.Helper()
	res, err := e.s.deliverByName(e.name, "Execute 🎯T679.2.", OriginAgent, false)
	if !birthPromptAccepted(res.Status, err) {
		e.t.Fatalf("opening prompt not accepted: status=%q err=%v", res.Status, err)
	}
	l := e.s.births()
	l.mu.Lock()
	_, ok := l.births[birthKey(e.name, e.sid)]
	l.mu.Unlock()
	if !ok {
		e.t.Fatal("accepted prompt was not recorded against the current session")
	}
}

func (e *t679_2Env) list() string {
	e.t.Helper()
	// The periodic health pass owns notice delivery; agent_list is read-only.
	sweepObservedBirths(e.s)
	res, err := observedAgentList(e.s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		e.t.Fatal(err)
	}
	return toolText(res)
}

func (e *t679_2Env) plantTranscript(body string) string {
	e.t.Helper()
	path := claudia.SessionJSONLPath(e.sid, e.work)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return path
}

func (e *t679_2Env) parentNotices() []string {
	var out []string
	for _, msg := range e.parent.sent {
		if strings.Contains(msg, "born-stuck:") && strings.Contains(msg, e.name) {
			out = append(out, msg)
		}
	}
	return out
}

func TestT679_2BirthPromptAccepted(t *testing.T) {
	t.Parallel()
	if !birthPromptAccepted("queued", nil) || !birthPromptAccepted("delivered_unconfirmed", nil) {
		t.Fatal("queued / delivered_unconfirmed are accepted prompts")
	}
	if birthPromptAccepted("queued", fmt.Errorf("boom")) {
		t.Fatal("a send error is not acceptance")
	}
	if birthPromptAccepted("suppressed_chatter", nil) {
		t.Fatal("a dropped send is not acceptance")
	}
	if noticeSubmitted("", fmt.Errorf("nope")) || noticeSubmitted("not_submitted", nil) {
		t.Fatal("unsubmitted notice must not count as success")
	}
}

func TestT679_2BeforeGraceNoClaimOrNotice(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-before")
	e.accept()
	e.clock.add(BornStuckGrace - time.Second)
	body := e.list()
	if strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("accused before the deadline:\n%s", body)
	}
	if n := e.parentNotices(); len(n) != 0 {
		t.Fatalf("notice before the deadline: %v", n)
	}
}

func TestT679_2AtDeadlineMarksAndNotifiesParentOnce(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-deadline")
	e.accept()
	childSends := len(e.child.sent)
	e.clock.add(BornStuckGrace)
	body := e.list()
	if !strings.Contains(body, e.name) || !strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("list did not mark born-stuck:\n%s", body)
	}
	notices := e.parentNotices()
	if len(notices) != 1 {
		t.Fatalf("want one parent notice, got %d: %v", len(notices), notices)
	}
	got := notices[0]
	for _, want := range []string{"provider=claude", "session=t6792-deadline", "elapsed=2m0s", "no transcript ever appeared"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice missing %q:\n%s", want, got)
		}
	}
	if len(e.child.sent) != childSends {
		t.Fatalf("diagnosis resent the opening prompt: child sends %d → %d", childSends, len(e.child.sent))
	}

	body2 := e.list()
	sweepObservedBirths(e.s)
	if n := e.parentNotices(); len(n) != 1 {
		t.Fatalf("repeated list/sweep resent the notice: %d\n%s", len(n), body2)
	}
}

func TestT679_2PeriodicHookRunsWithoutList(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-hook")
	e.accept()
	e.clock.add(BornStuckGrace)
	sweepObservedBirths(e.s)
	if n := e.parentNotices(); len(n) != 1 {
		t.Fatalf("periodic sweep did not notify: %v", n)
	}
	if !e.s.bornStuck(*e.reg.Def(e.name)) {
		t.Fatal("periodic sweep did not diagnose born-stuck")
	}
}

func TestT679_2LateTranscriptClearsEvenQueueAttachmentOnly(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-late")
	e.accept()
	e.clock.add(BornStuckGrace)
	if body := e.list(); !strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("setup: want born-stuck before the file appears:\n%s", body)
	}
	e.plantTranscript(`{"type":"attachment","attachment":{"type":"queued_command","prompt":"hi"}}` + "\n")
	body := e.list()
	if strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("queue-attachment-only file must clear born-stuck:\n%s", body)
	}
}

func TestT679_2RemintCannotUseOldEvidence(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-old")
	e.accept()
	e.clock.add(BornStuckGrace)
	if body := e.list(); !strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("setup: old session should be born-stuck:\n%s", body)
	}
	oldNotices := len(e.parentNotices())
	e.plantTranscript(`{"type":"user","message":{"content":"old session"}}` + "\n")

	const newSID = "t6792-new"
	if err := e.reg.Register(claudia.AgentDef{
		Name: e.name, WorkDir: e.work, SessionID: newSID,
		Purpose: claudia.PurposeWork, Parent: e.parentName, Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	e.sid = newSID
	e.s.noteTurnEnded(e.name)
	e.s.clearUnconfirmedSend(e.name)
	e.child.inFlight = false
	e.parent.inFlight = false
	if body := e.list(); strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("remint with no accepted prompt must not inherit old born-stuck:\n%s", body)
	}
	e.accept()
	if body := e.list(); strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("new birth still inside grace:\n%s", body)
	}
	e.clock.add(BornStuckGrace)
	body := e.list()
	if !strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("new session past grace with no transcript must be born-stuck:\n%s", body)
	}
	if n := e.parentNotices(); len(n) != oldNotices+1 {
		t.Fatalf("new birth must mint a new notice, got %d want %d: %v", len(n), oldNotices+1, n)
	}
	last := e.parentNotices()[len(e.parentNotices())-1]
	if !strings.Contains(last, "session=t6792-new") {
		t.Fatalf("new notice must name the new session:\n%s", last)
	}
}

func TestT679_2UnobservableNeverAbsence(t *testing.T) {
	t.Run("cursor phantom JSONL", func(t *testing.T) {
		e := t679_2Harness(t, claudia.ProviderCursor, t679_2SID)
		e.s.cursorSubmit = func(name, text string) error {
			if name != e.name || !strings.Contains(text, "Execute 🎯T679.2") {
				t.Fatalf("cursor submit name=%q text=%q", name, text)
			}
			return nil
		}
		if err := e.s.submitCursorStartBrief(e.name, "Execute 🎯T679.2."); err != nil {
			t.Fatal(err)
		}
		phantom := claudia.SessionJSONLPath(e.sid, e.work)
		if err := os.MkdirAll(filepath.Dir(phantom), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(phantom, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		store := claudia.CursorACPStorePath(e.sid)
		if store != "" {
			if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store, []byte("sqlite"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		e.clock.add(BornStuckGrace)
		body := e.list()
		if strings.Contains(body, AgentStatusBornStuck) {
			t.Fatalf("cursor phantom JSONL / store.db must not be absence:\n%s", body)
		}
		if n := e.parentNotices(); len(n) != 0 {
			t.Fatalf("cursor unobservable produced a notice: %v", n)
		}
	})

	t.Run("grok unresolved roots", func(t *testing.T) {
		e := t679_2Harness(t, claudia.ProviderGrok, t679_2SID)
		e.s.SetBirthRoots(discovery.Roots{})
		e.accept()
		e.clock.add(BornStuckGrace)
		body := e.list()
		if strings.Contains(body, AgentStatusBornStuck) {
			t.Fatalf("unresolved grok roots must not be absence:\n%s", body)
		}
		if n := e.parentNotices(); len(n) != 0 {
			t.Fatalf("unobservable grok produced a notice: %v", n)
		}
	})

	t.Run("claude permission denied", func(t *testing.T) {
		e := t679_2Harness(t, claudia.ProviderClaude, "t6792-perm")
		e.accept()
		path := e.plantTranscript(`{"type":"user"}` + "\n")
		bucket := filepath.Dir(path)
		if err := os.Chmod(bucket, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(bucket, 0o755) })
		if _, err := os.Stat(path); err == nil {
			t.Skip("chmod 000 did not deny stat; cannot exercise permission failure")
		}
		e.clock.add(BornStuckGrace)
		body := e.list()
		if strings.Contains(body, AgentStatusBornStuck) {
			t.Fatalf("permission-denied lookup must not be absence:\n%s", body)
		}
	})
}

func TestT679_2NoAcceptedPromptNeverAccusedAndNudgesKeepDeadline(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-nudge")
	e.clock.add(BornStuckGrace * 2)
	if body := e.list(); strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("no accepted prompt must never be accused:\n%s", body)
	}

	e.accept()
	e.clock.add(BornStuckGrace / 2)
	if _, err := e.s.deliverByName(e.name, "nudge: still waiting", OriginAgent, false); err != nil {
		t.Fatalf("nudge: %v", err)
	}
	if body := e.list(); strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("nudge restarted the clock or accused early:\n%s", body)
	}
	e.clock.add(BornStuckGrace / 2)
	body := e.list()
	if !strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("original deadline must still fire after nudges:\n%s", body)
	}
}

func TestT679_2NoticeFailureRetriesAfterRestartWithoutManufacturingSuccess(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-retry")
	e.accept()
	e.parent.sendErr = fmt.Errorf("parent unreachable")
	e.clock.add(BornStuckGrace)
	sweepObservedBirths(e.s)
	if n := e.parentNotices(); len(n) != 0 {
		t.Fatalf("failed notice was treated as submitted: %v", n)
	}
	l := e.s.births()
	l.mu.Lock()
	rec := l.notices[bornStuckNoticeKey(e.name, e.sid)]
	l.mu.Unlock()
	if rec.Submitted {
		t.Fatal("manufactured success on a failed notice")
	}

	s2 := New(e.dir, nil, nil)
	reg2, err := claudia.NewRegistry(filepath.Join(e.dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s2.SetRegistry(reg2)
	s2.SetSendQueueDir(e.dir)
	s2.SetBirthClock(e.clock.now)
	s2.SetSeatAliveFn(func(n string) bool { return n == e.name })
	parent2 := &fakeSender{alive: true}
	setObservedSenderResolver(s2, func(n string) (agentSender, bool, error) {
		if n == e.parentName {
			return parent2, false, nil
		}
		return &fakeSender{alive: true}, false, nil
	})
	sweepObservedBirths(s2)
	var notices []string
	for _, msg := range parent2.sent {
		if strings.Contains(msg, "born-stuck:") {
			notices = append(notices, msg)
		}
	}
	if len(notices) != 1 {
		t.Fatalf("restart must retry the unsubmitted notice once, got %d: %v", len(notices), notices)
	}
	sweepObservedBirths(s2)
	count := 0
	for _, msg := range parent2.sent {
		if strings.Contains(msg, "born-stuck:") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("submitted notice was resent after restart: %d", count)
	}
}

func TestT679_2DiagnosisDoesNotStopKillMigrateOrWeakenT664(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-t664")
	e.accept()
	e.s.noteUnconfirmedSend(e.name, "Execute 🎯T679.2.")
	e.clock.add(BornStuckGrace)
	sweepObservedBirths(e.s)
	if e.reg.Def(e.name) == nil {
		t.Fatal("diagnosis killed or removed the seat")
	}
	if got := e.reg.Def(e.name).Provider; got != claudia.ProviderClaude {
		t.Fatalf("diagnosis migrated the seat: provider=%s", got)
	}
	res, err := e.s.handleAgentStop(context.Background(), t664Call(e.name, "jevons", false))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(res), "refusing to stop") {
		t.Fatalf("diagnosis weakened 🎯T664: %s", toolText(res))
	}
}

func TestT679_2PinnedStaysVisibleAlongsideBornStuck(t *testing.T) {
	e := t679_2Harness(t, claudia.ProviderClaude, "t6792-pin")
	e.accept()
	e.clock.add(BornStuckGrace)
	body := e.list()
	if !strings.Contains(body, AgentStatusBornStuck) {
		t.Fatalf("want born-stuck:\n%s", body)
	}
	if !strings.Contains(body, "no transcript ever appeared") {
		t.Fatalf("want birth annotation:\n%s", body)
	}
}
