package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/chatlog"
)

func admissionFixture(t *testing.T) (*Server, *chatlog.Log) {
	t.Helper()
	dir := t.TempDir()
	s := New("test", dir)
	log, err := chatlog.Open(filepath.Join(dir, "owner.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s.chatLog = log
	if err := s.EnableOverseerAdmission(filepath.Join(dir, "restricted")); err != nil {
		t.Fatal(err)
	}
	return s, log
}
func auditAndChat(t *testing.T, s *Server, log *chatlog.Log, body string, wantChat bool) {
	t.Helper()
	var lines []string
	if err := log.Replay(func(line string) error { lines = append(lines, line); return nil }); err != nil {
		t.Fatal(err)
	}
	chat := strings.Join(lines, "\n")
	if strings.Contains(chat, body) != wantChat {
		t.Fatalf("chat body presence=%t want %t: %s", strings.Contains(chat, body), wantChat, chat)
	}
}
func TestT1054PreJournalAdmissionAndReorderedEvidence(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	if err := s.BindOwnerAdmission("turn-1", "owner-request-1"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-1", Text: "I will answer your question."})
	auditAndChat(t, s, log, "I will answer", false)
	if err := s.RegisterOwnerIncident("incident-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitOwnerCandidate("turn-1", "owner-request-other", AdmissionAllowed); err == nil {
		t.Fatal("mismatched owner request admitted")
	}
	if err := s.AdmitOwnerCandidate("turn-1", "invented-incident", AdmissionAllowed); err == nil {
		t.Fatal("invented incident admitted")
	}
	if err := s.AdmitOwnerCandidate("turn-other", "owner-request-1", AdmissionAllowed); err == nil {
		t.Fatal("reordered turn admitted")
	}
	auditAndChat(t, s, log, "I will answer", false)
	if err := s.AdmitOwnerCandidate("turn-1", "owner-request-1", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	auditAndChat(t, s, log, "I will answer", true)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-1", StopReason: "end_turn"})
}
func TestT1054SilentToolContinuationNeedsFreshClaim(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	if err := s.BindOwnerAdmission("turn-2", "request-2"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-2", Text: "routine worker finished", StopReason: "tool_use"})
	if err := s.AdmitOwnerCandidate("turn-2", "", AdmissionSilent); err != nil {
		t.Fatal(err)
	}
	auditAndChat(t, s, log, "routine worker finished", false)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-2", Text: "Here is the owner answer"})
	auditAndChat(t, s, log, "Here is the owner answer", false)
	if err := s.AdmitOwnerCandidate("turn-2", "request-2", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	auditAndChat(t, s, log, "Here is the owner answer", true)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-2", StopReason: "end_turn"})
}
func TestT1054MissingIDFailsOpenWithDaemonIndicatorAndRestrictedAudit(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	if err := s.BindOwnerAdmission("turn-3", "request-3"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-3", Text: "novel safety incident"})
	ch := make(chan string, 10)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "routine"})
	auditAndChat(t, s, log, "novel safety incident", false)
	select {
	case line := <-ch:
		if !strings.Contains(line, "admission evidence unavailable") {
			t.Fatal(line)
		}
	case <-time.After(time.Second):
		t.Fatal("no daemon escalation")
	}
	path := filepath.Join(s.ownerAdmission.auditDir, "owner-admission.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "novel safety incident") {
		t.Fatalf("audit missing held body: %s", b)
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("audit permissions: %v %v", st, err)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(string(b)), "\n")[0]), &row); err != nil {
		t.Fatal(err)
	}
	if err := s.BindOwnerAdmission("turn-4", "request-3"); err == nil {
		t.Fatal("pending request reused as if answered")
	}
}
func TestT1054BoundedTimeoutAndPreviewNeverJournaled(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	if err := s.BindOwnerAdmission("turn-5", "request-5"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressTUIPreview, TurnID: "turn-5", Text: "private preview"})
	s.ownerAdmission.mu.Lock()
	c := s.ownerAdmission.candidate
	s.ownerAdmission.mu.Unlock()
	s.expireOwnerAdmission(c, "test timeout")
	auditAndChat(t, s, log, "private preview", false)
	if err := s.AdmitOwnerCandidate("turn-5", "request-5", AdmissionAllowed); err == nil {
		t.Fatal("late claim admitted")
	}
}

func TestT1054JournalFailureDoesNotAnswerOrBroadcast(t *testing.T) {
	s, log := admissionFixture(t)
	if err := s.BindOwnerAdmission("turn-6", "request-6"); err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 10)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-6", Text: "answer only if durable"})
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitOwnerCandidate("turn-6", "request-6", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	s.ownerAdmission.mu.Lock()
	pending := s.ownerAdmission.requests["request-6"]
	s.ownerAdmission.mu.Unlock()
	if !pending {
		t.Fatal("journal failure incorrectly marked owner answered")
	}
	for len(ch) > 0 {
		line := <-ch
		if strings.Contains(line, "answer only if durable") {
			t.Fatalf("undurable body reached WS: %s", line)
		}
	}
}

func TestT1054RestartReplayDoesNotReadRestrictedAudit(t *testing.T) {
	s, log := admissionFixture(t)
	if err := s.BindOwnerAdmission("turn-7", "request-7"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-7", Text: "novel safety disclosure"})
	path := log.Path()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := chatlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	auditAndChat(t, s, reopened, "novel safety disclosure", false)
	audit, err := os.ReadFile(filepath.Join(s.ownerAdmission.auditDir, "owner-admission.jsonl"))
	if err != nil || !strings.Contains(string(audit), "novel safety disclosure") {
		t.Fatalf("restricted audit not durable: %v %s", err, audit)
	}
	s.ownerAdmission.mu.Lock()
	s.ownerAdmission.candidate.timer.Stop()
	s.ownerAdmission.mu.Unlock()
}
