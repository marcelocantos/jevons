package ownerquestions

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestQuestionOutboxInterleavingChangesRestartAndResolve(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	var sends []string
	send := func(_, body, _, _ string) (string, error) { sends = append(sends, body); return "spool/" + body, nil }
	s := New(dir)
	s.Send = send
	for _, q := range []Question{{"A", "approve A?", ""}, {"B", "approve B?", ""}, {"A", "approve A?", ""}, {"A", "approve A?", ""}} {
		e, err := s.Observe(q, now)
		if err != nil || e.Status != "spooled" || e.Delivered {
			t.Fatalf("observe %+v: %+v %v", q, e, err)
		}
	}
	if len(sends) != 2 {
		t.Fatalf("A/B/A duplicate misfired: %v", sends)
	}
	cold := New(dir)
	cold.Send = send
	if _, err := cold.Observe(Question{"A", "approve A?", ""}, now); err != nil || len(sends) != 2 {
		t.Fatalf("restart dedup %v: %v", err, sends)
	}
	cold.Observe(Question{"A", "approve revised A?", ""}, now)
	if len(sends) != 3 {
		t.Fatalf("changed wording did not notify: %v", sends)
	}
	entries, _, err := cold.Snapshot()
	if err != nil || len(entries) != 2 || entries[0].SpoolPath == "" || entries[0].Delivered {
		t.Fatalf("snapshot %+v %v", entries, err)
	}
	if err := cold.Resolve("A"); err != nil {
		t.Fatal(err)
	}
	if err := cold.Resolve("B"); err != nil {
		t.Fatal(err)
	}
	if err := cold.Remind(now.Add(48 * time.Hour)); err != nil || len(sends) != 3 {
		t.Fatalf("moot questions reminded: %v %v", sends, err)
	}
}
func TestQuestionOutboxTransportOutageAndBoundedReminder(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	s := New(dir)
	fail := true
	calls := 0
	s.Send = func(_, body, _, _ string) (string, error) {
		calls++
		if fail {
			return "", errors.New("offline")
		}
		return "spooled", nil
	}
	e, err := s.Observe(Question{"A", "question?", ""}, now)
	if err == nil || e.Status != "failed" || !strings.Contains(e.LastError, "offline") {
		t.Fatalf("outage: %+v %v", e, err)
	}
	cold := New(dir)
	cold.Send = s.Send
	entries, _, _ := cold.Snapshot()
	if len(entries) != 1 || entries[0].Status != "failed" {
		t.Fatalf("restart lost failure %+v", entries)
	}
	fail = false
	e, err = cold.Observe(Question{"A", "question?", ""}, now)
	if err != nil || e.Status != "spooled" {
		t.Fatalf("retry %+v %v", e, err)
	}
	if err := cold.Remind(now.Add(25 * time.Hour)); err != nil || calls != 3 {
		t.Fatalf("digest: calls=%d err=%v", calls, err)
	}
	cold.Remind(now.Add(26 * time.Hour))
	if calls != 3 {
		t.Fatalf("unbounded digest: %d", calls)
	}
}
