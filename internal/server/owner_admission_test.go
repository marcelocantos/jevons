package server

import (
	"context"
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
	if err := s.enableOverseerAdmissionForTest(filepath.Join(dir, "restricted")); err != nil {
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
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-1", "owner-request-1"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-1", Text: "I will answer your question."})
	auditAndChat(t, s, log, "I will answer", false)
	if err := s.registerOwnerIncident(s.ownerAdmission.authority, "turn-1", "incident-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.admitOwnerCandidate(s.ownerAdmission.authority, "turn-1", "owner-request-other", AdmissionAllowed); err == nil {
		t.Fatal("mismatched owner request admitted")
	}
	if err := s.admitOwnerCandidate(s.ownerAdmission.authority, "turn-1", "invented-incident", AdmissionAllowed); err == nil {
		t.Fatal("invented incident admitted")
	}
	if err := s.admitOwnerCandidate(s.ownerAdmission.authority, "turn-other", "owner-request-1", AdmissionAllowed); err == nil {
		t.Fatal("reordered turn admitted")
	}
	auditAndChat(t, s, log, "I will answer", false)
	if err := s.admitOwnerCandidate(s.ownerAdmission.authority, "turn-1", "owner-request-1", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	auditAndChat(t, s, log, "I will answer", true)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-1", StopReason: "end_turn"})
}
func TestT1054SilentToolContinuationNeedsFreshClaim(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-2", "request-2"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-2", Text: "routine worker finished", StopReason: "tool_use"})
	if err := s.admitOwnerCandidate(s.ownerAdmission.authority, "turn-2", "", AdmissionSilent); err != nil {
		t.Fatal(err)
	}
	auditAndChat(t, s, log, "routine worker finished", false)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-2", Text: "Here is the owner answer"})
	auditAndChat(t, s, log, "Here is the owner answer", false)
	if err := s.admitOwnerCandidate(s.ownerAdmission.authority, "turn-2", "request-2", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	auditAndChat(t, s, log, "Here is the owner answer", true)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-2", StopReason: "end_turn"})
}
func TestT1054MissingIDFailsOpenWithDaemonIndicatorAndRestrictedAudit(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-3", "request-3"); err != nil {
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
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-4", "request-3"); err != nil {
		t.Fatalf("pending owner request must survive reissue: %v", err)
	}
}
func TestT1054BoundedTimeoutAndPreviewNeverJournaled(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-5", "request-5"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressTUIPreview, TurnID: "turn-5", Text: "private preview"})
	s.ownerAdmission.mu.Lock()
	c := s.ownerAdmission.candidate
	s.ownerAdmission.mu.Unlock()
	s.expireOwnerAdmission(c, "test timeout")
	auditAndChat(t, s, log, "private preview", false)
	if err := s.admitOwnerCandidate(s.ownerAdmission.authority, "turn-5", "request-5", AdmissionAllowed); err == nil {
		t.Fatal("late claim admitted")
	}
}

func TestT1054JournalFailureDoesNotAnswerOrBroadcast(t *testing.T) {
	s, log := admissionFixture(t)
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-6", "request-6"); err != nil {
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
	if err := s.admitOwnerCandidate(s.ownerAdmission.authority, "turn-6", "request-6", AdmissionAllowed); err != nil {
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
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-7", "request-7"); err != nil {
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

func TestT1054AuditFailureIsVisibleLossNotSilentBody(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-loss", "request-loss"); err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 10)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	// Simulate the audit mount disappearing before the first body arrives.
	s.ownerAdmission.auditDir = filepath.Join(t.TempDir(), "missing")
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-loss", Text: "unretained safety report"})
	auditAndChat(t, s, log, "unretained safety report", false)
	found := false
	for len(ch) > 0 {
		if strings.Contains(<-ch, "candidate content may be lost") {
			found = true
		}
	}
	if !found {
		t.Fatal("no explicit audit-loss marker")
	}
	s.ownerAdmission.mu.Lock()
	pending := s.ownerAdmission.requests["request-loss"]
	open := s.ownerAdmission.candidate != nil
	s.ownerAdmission.mu.Unlock()
	if !pending || open {
		t.Fatal("audit failure answered or left candidate open")
	}
}

func TestT1054PublicationSerializesHeldBeforeLaterFragment(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	cap := s.ownerAdmission.authority
	if err := s.bindOwnerAdmission(cap, "turn-order", "request-order"); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-order", Text: "first-fragment"})
	entered := make(chan struct{})
	release := make(chan struct{})
	s.ownerAdmission.beforePublish = func(ev claudia.Event) {
		if ev.Text == "first-fragment" {
			close(entered)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() { done <- s.admitOwnerCandidate(cap, "turn-order", "request-order", AdmissionAllowed) }()
	<-entered
	late := make(chan struct{})
	go func() {
		s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-order", Text: "later-fragment", StopReason: "end_turn"})
		close(late)
	}()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-late
	var lines []string
	if err := log.Replay(func(line string) error { lines = append(lines, line); return nil }); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if i, j := strings.Index(joined, "first-fragment"), strings.Index(joined, "later-fragment"); i < 0 || j < i {
		t.Fatalf("out-of-order publish: %s", joined)
	}
}

func TestT1054IncidentEvidenceTurnBoundSingleUse(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	cap := s.ownerAdmission.authority
	if err := s.bindOwnerAdmission(cap, "turn-incident", "request-incident"); err != nil {
		t.Fatal(err)
	}
	if err := s.registerOwnerIncident(cap, "other-turn", "incident-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.admitOwnerCandidate(cap, "turn-incident", "incident-1", AdmissionAllowed); err == nil {
		t.Fatal("cross-turn incident admitted")
	}
	if err := s.registerOwnerIncident(cap, "turn-incident", "incident-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.admitOwnerCandidate(cap, "turn-incident", "incident-2", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-incident", Text: "material novel incident", StopReason: "tool_use"})
	if err := s.admitOwnerCandidate(cap, "turn-incident", "incident-2", AdmissionAllowed); err == nil {
		t.Fatal("communicated incident reauthorized")
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-incident", StopReason: "end_turn"})
	if err := s.admitOwnerCandidate(cap, "turn-incident", "request-incident", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
}

func TestT1054CapabilityAndAuditPathRefusal(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	log, err := chatlog.Open(filepath.Join(dir, "owner.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	s.chatLog = log
	if err := s.enableOverseerAdmissionForTest(dir); err == nil {
		t.Fatal("audit shares chat journal directory")
	}
	link := filepath.Join(dir, "restricted-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := s.enableOverseerAdmissionForTest(link); err == nil {
		t.Fatal("symlink audit accepted")
	}
	public := filepath.Join(dir, "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	if err := s.enableOverseerAdmissionForTest(public); err == nil {
		t.Fatal("world-readable audit directory accepted")
	}
	if err := s.enableOverseerAdmissionForTest(filepath.Join(dir, "private")); err != nil {
		t.Fatal(err)
	}
	if err := s.bindOwnerAdmission(nil, "turn", "request"); err == nil {
		t.Fatal("nil capability accepted")
	}
	if err := s.registerOwnerIncident(&admissionAuthority{nonce: "forged"}, "turn", "incident"); err == nil {
		t.Fatal("forged capability accepted")
	}
	if err := s.admitOwnerCandidate(nil, "turn", "request", AdmissionAllowed); err == nil {
		t.Fatal("untrusted claim accepted")
	}
}

func TestT1054LegacyJournalFailureStillBroadcastsVisibleAnswer(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	log, err := chatlog.Open(filepath.Join(dir, "owner.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s.chatLog = log
	ch := make(chan string, 10)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "legacy answer"})
	s.ownerMu.Lock()
	accounted := s.ownerHealthLocked().turnVisible
	s.ownerMu.Unlock()
	if !accounted {
		t.Fatal("nil-admission live answer no longer counts in owner-question ledger")
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	visible := false
	for len(ch) > 0 {
		if strings.Contains(<-ch, "legacy answer") {
			visible = true
		}
	}
	if !visible {
		t.Fatal("nil-admission legacy journal failure no longer broadcasts")
	}
}

func TestT1054ActiveAdmissionDoesNotGloballyGateOwnerEcho(t *testing.T) {
	s, log := admissionFixture(t)
	ch := make(chan string, 10)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	s.BroadcastChat(`{"type":"send_error","text":"owner delivery failed"}`)
	select {
	case line := <-ch:
		if !strings.Contains(line, "owner delivery failed") {
			t.Fatalf("not send_error: %s", line)
		}
	default:
		t.Fatal("active admission gated unrelated owner notice")
	}
}

func TestT1054AuditCapacityAndDirectoryReplacementFailClosed(t *testing.T) {
	for _, kind := range []string{"capacity", "replacement"} {
		t.Run(kind, func(t *testing.T) {
			s, log := admissionFixture(t)
			defer log.Close()
			cap := s.ownerAdmission.authority
			if err := s.bindOwnerAdmission(cap, "turn-guard", "request-guard"); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "capacity":
				path := filepath.Join(s.ownerAdmission.auditDir, "owner-admission.jsonl")
				f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Truncate(admissionMaxAuditBytes); err != nil {
					t.Fatal(err)
				}
				f.Close()
			case "replacement":
				original := s.ownerAdmission.auditDir
				if err := os.Rename(original, original+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(original, 0700); err != nil {
					t.Fatal(err)
				}
			}
			ch := make(chan string, 10)
			s.mu.Lock()
			s.chatListeners = append(s.chatListeners, ch)
			s.mu.Unlock()
			s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-guard", Text: "unsafe disclosure"})
			auditAndChat(t, s, log, "unsafe disclosure", false)
			found := false
			for len(ch) > 0 {
				if strings.Contains(<-ch, "candidate content may be lost") {
					found = true
				}
			}
			if !found {
				t.Fatal("audit rejection did not surface loss marker")
			}
		})
	}
}

type admissionRemoteRecorder struct{ lines [][]byte }

func (r *admissionRemoteRecorder) WriteText(_ context.Context, b []byte) error {
	r.lines = append(r.lines, append([]byte(nil), b...))
	return nil
}
func (*admissionRemoteRecorder) WriteBinary(context.Context, []byte) error { return nil }
func (*admissionRemoteRecorder) Close() error                              { return nil }

func TestT1054JournalFailureCannotLeakViaMuxOrRemote(t *testing.T) {
	s, log := admissionFixture(t)
	cap := s.ownerAdmission.authority
	if err := s.bindOwnerAdmission(cap, "turn-all", "request-all"); err != nil {
		t.Fatal(err)
	}
	sess := &muxSession{send: make(chan []byte, 32), transcripts: map[string]*muxWatch{"jevons": {subscribed: true}}}
	s.mux.add(sess)
	defer s.mux.remove(sess)
	remote := &admissionRemoteRecorder{}
	s.mu.Lock()
	s.remotes[1] = remoteConn{writer: remote, ctx: context.Background()}
	s.mu.Unlock()
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-all", Text: "secret novel safety", StopReason: "end_turn"})
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.admitOwnerCandidate(cap, "turn-all", "request-all", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	for _, line := range remote.lines {
		if strings.Contains(string(line), "secret novel safety") {
			t.Fatalf("remote body leak: %s", line)
		}
	}
	for len(sess.send) > 0 {
		if line := <-sess.send; strings.Contains(string(line), "secret novel safety") {
			t.Fatalf("mux WS body leak: %s", line)
		}
	}
	for _, ev := range s.mux.eventsFor("jevons") {
		if strings.Contains(string(ev.Body), "secret novel safety") {
			t.Fatalf("mux cache body leak: %+v", ev)
		}
	}
	s.ownerAdmission.mu.Lock()
	pending := s.ownerAdmission.requests["request-all"]
	s.ownerAdmission.mu.Unlock()
	if !pending {
		t.Fatal("undurable body answered request")
	}
}

func TestT1054RestartExposesCurrentLossRecoveryGapNotFalseDurability(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	log, err := chatlog.Open(filepath.Join(dir, "owner.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s.chatLog = log
	if err := s.enableOverseerAdmissionForTest(filepath.Join(dir, "restricted")); err != nil {
		t.Fatal(err)
	}
	if err := s.bindOwnerAdmission(s.ownerAdmission.authority, "turn-loss-restart", "request-loss-restart"); err != nil {
		t.Fatal(err)
	}
	s.ownerAdmission.auditDir = filepath.Join(dir, "missing")
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-loss-restart", Text: "unsafe if lost"})
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	// The indicator is deliberately ephemeral and the obligation is currently
	// memory-only. Assert the gap, rather than calling this a recoverable gate.
	reloaded := New("test", dir)
	if reloaded.ownerAdmission != nil {
		t.Fatal("test seam unexpectedly auto-activated on restart")
	}
	reopened, err := chatlog.Open(filepath.Join(dir, "owner.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	auditAndChat(t, reloaded, reopened, "candidate content may be lost", false)
	auditAndChat(t, reloaded, reopened, "unsafe if lost", false)
}

func TestT1054ProgressPreviewJournalFailureNeverLeaksToAnyFanout(t *testing.T) {
	s, log := admissionFixture(t)
	cap := s.ownerAdmission.authority
	if err := s.bindOwnerAdmission(cap, "turn-preview", "request-preview"); err != nil {
		t.Fatal(err)
	}
	sess := &muxSession{send: make(chan []byte, 16), transcripts: map[string]*muxWatch{"jevons": {subscribed: true}}}
	s.mux.add(sess)
	defer s.mux.remove(sess)
	remote := &admissionRemoteRecorder{}
	s.mu.Lock()
	s.remotes[1] = remoteConn{writer: remote, ctx: context.Background()}
	s.mu.Unlock()
	ev := claudia.Event{Type: "progress", ProgressType: claudia.ProgressTUIPreview, TurnID: "turn-preview", Text: "private candidate preview", Raw: []byte(`{"content":"private raw preview"}`)}
	s.DeliverOverseerEvent(ev)
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.admitOwnerCandidate(cap, "turn-preview", "request-preview", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	for _, line := range remote.lines {
		if strings.Contains(string(line), "private") {
			t.Fatalf("remote preview leak: %s", line)
		}
	}
	for len(sess.send) > 0 {
		if line := <-sess.send; strings.Contains(string(line), "private") {
			t.Fatalf("mux preview leak: %s", line)
		}
	}
	for _, item := range s.mux.eventsFor("jevons") {
		if strings.Contains(string(item.Body), "private") {
			t.Fatalf("mux cache preview leak: %+v", item)
		}
	}
}

func TestT1054AllowedPreviewFollowsDurableAdmissionOnly(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	cap := s.ownerAdmission.authority
	remote := &admissionRemoteRecorder{}
	s.mu.Lock()
	s.remotes[1] = remoteConn{writer: remote, ctx: context.Background()}
	s.mu.Unlock()
	if err := s.bindOwnerAdmission(cap, "turn-allowed-preview", "request-allowed-preview"); err != nil {
		t.Fatal(err)
	}
	ev := claudia.Event{Type: "progress", ProgressType: claudia.ProgressTUIPreview, TurnID: "turn-allowed-preview", Text: "admitted preview"}
	s.DeliverOverseerEvent(ev)
	auditAndChat(t, s, log, "admitted preview", false)
	if err := s.admitOwnerCandidate(cap, "turn-allowed-preview", "request-allowed-preview", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	// A TUI preview is phase-only even when the candidate is allowed; it
	// never becomes a durable owner answer or a text bubble.
	auditAndChat(t, s, log, "admitted preview", false)
	for _, line := range remote.lines {
		if strings.Contains(string(line), "admitted preview") {
			t.Fatalf("phase-only preview reached remote as prose: %s", line)
		}
	}
}

func TestT1054UndurableToolTitleDoesNotLeakThroughPhaseOrSeat(t *testing.T) {
	s, log := admissionFixture(t)
	cap := s.ownerAdmission.authority
	if err := s.bindOwnerAdmission(cap, "turn-step", "request-step"); err != nil {
		t.Fatal(err)
	}
	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	s.beginOverseerPhase(nil)
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressToolUse, TurnID: "turn-step", ToolTitle: "private-title", ToolCallID: "secret-call", Text: `{"private":"args"}`})
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.admitOwnerCandidate(cap, "turn-step", "request-step", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	for len(ch) > 0 {
		line := <-ch
		if strings.Contains(line, "private-title") || strings.Contains(line, "secret-call") || strings.Contains(line, "private") {
			t.Fatalf("phase/chat leaked tool metadata: %s", line)
		}
	}
	phase := s.OverseerPhase()
	if strings.Contains(phase.Step, "private-title") {
		t.Fatalf("phase model leaked tool title: %+v", phase)
	}
}

func TestT1054PriorPrefixDurabilityCannotAuthorizeFailedCurrentFragment(t *testing.T) {
	s, log := admissionFixture(t)
	cap := s.ownerAdmission.authority
	if err := s.bindOwnerAdmission(cap, "turn-prefix", "request-prefix"); err != nil {
		t.Fatal(err)
	}
	remote := &admissionRemoteRecorder{}
	s.mu.Lock()
	s.remotes[1] = remoteConn{writer: remote, ctx: context.Background()}
	s.mu.Unlock()
	sess := &muxSession{send: make(chan []byte, 32), transcripts: map[string]*muxWatch{"jevons": {subscribed: true}}}
	s.mux.add(sess)
	defer s.mux.remove(sess)
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-prefix", Text: "["})
	// The next fragment proves the stream visible. DeliverOverseerEvent first
	// flushes the held "[" (successful journal write), then writes the CURRENT
	// fragment. Close the journal exactly between these writes.
	writes := 0
	s.admissionJournalHook = func(_ string, durable bool) {
		writes++
		if writes == 1 {
			if !durable {
				t.Error("prefix did not persist")
			}
			_ = log.Close()
		}
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-prefix", Text: "private safety disclosure"})
	if err := s.admitOwnerCandidate(cap, "turn-prefix", "request-prefix", AdmissionAllowed); err != nil {
		t.Fatal(err)
	}
	if writes < 2 {
		t.Fatalf("did not exercise two writes: %d", writes)
	}
	for _, line := range remote.lines {
		if strings.Contains(string(line), "private safety disclosure") {
			t.Fatalf("current undurable text leaked remote: %s", line)
		}
	}
	for len(sess.send) > 0 {
		if line := <-sess.send; strings.Contains(string(line), "private safety disclosure") {
			t.Fatalf("current undurable text leaked mux: %s", line)
		}
	}
	s.ownerAdmission.mu.Lock()
	pending := s.ownerAdmission.requests["request-prefix"]
	s.ownerAdmission.mu.Unlock()
	if !pending {
		t.Fatal("current undurable fragment marked owner answered")
	}
}

func TestT1054SilentTerminalBodyCannotRideBodylessDurability(t *testing.T) {
	s, log := admissionFixture(t)
	defer log.Close()
	cap := s.ownerAdmission.authority
	if err := s.bindOwnerAdmission(cap, "turn-silent-body", "request-silent-body"); err != nil {
		t.Fatal(err)
	}
	remote := &admissionRemoteRecorder{}
	s.mu.Lock()
	s.remotes[1] = remoteConn{writer: remote, ctx: context.Background()}
	s.mu.Unlock()
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", TurnID: "turn-silent-body", Text: "[silent] private routine", StopReason: "end_turn"})
	if err := s.admitOwnerCandidate(cap, "turn-silent-body", "", AdmissionSilent); err != nil {
		t.Fatal(err)
	}
	for _, line := range remote.lines {
		if strings.Contains(string(line), "private routine") {
			t.Fatalf("silent body leaked remote: %s", line)
		}
	}
	s.ownerAdmission.mu.Lock()
	pending := s.ownerAdmission.requests["request-silent-body"]
	s.ownerAdmission.mu.Unlock()
	if !pending {
		t.Fatal("silent body marked owner answered")
	}
}
