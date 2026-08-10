// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T416 clause 9, EXERCISED INSTRUMENT (A).
//
// The handover dispatcher is the fourth caller of the stuck send path, and the
// only one that was already honest. deliverToSender, deliverToOverseer and
// drainAgentSendQueue inferred success from proc.Send() returning nil and said
// "Message sent" for a paste that never left the composer. This one waits for
// the reply, so an unsubmitted paste times out and is reported as a delivery
// failure — which is exactly what it did at 18:21 on 2026-08-10 for
// jv-t416-send-turn-begin, unread.
//
// That asymmetry is why this test exists at all. The three instruments that
// were consulted that day all lied; the one that was truthful was consulted by
// nobody. A suite that only kills bad oracles does not tell the next reader
// which good one to reach for, so the honest line is asserted on rather than
// merely left in place — an instrument nothing asserts on is one the next
// refactor drops without noticing.

// logSink captures records so a log line can be asserted on as the instrument
// it is.
type logSink struct{ records []slog.Record }

func (h *logSink) Enabled(context.Context, slog.Level) bool { return true }
func (h *logSink) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *logSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logSink) WithGroup(string) slog.Handler      { return h }

func (h *logSink) find(level slog.Level, msg string) (slog.Record, bool) {
	for _, r := range h.records {
		if r.Level == level && r.Message == msg {
			return r, true
		}
	}
	return slog.Record{}, false
}

// A handover seed that never becomes a turn must produce the daemon's
// fail-closed line and leave the record pending — never a silent success.
func TestHandoverHandOffFailsClosedAndSaysSo(t *testing.T) {
	const session = "019fd13d-e500-7913-b96c-981e50aa2e26"
	f, store, _ := migrateFixture(t, session, true)
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	pending, ok, err := store.Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("no pending record: ok=%v err=%v", ok, err)
	}

	// The stuck paste, as Deliver actually experiences it: the send call
	// succeeds, the composer holds the text, no turn ever begins, and the wait
	// for the reply expires. This is the 18:21 failure verbatim.
	f.seedDeliver = func(string, string) (string, error) {
		return "", fmt.Errorf("deliver turn to agent %q: %w", "jevons-po", context.DeadlineExceeded)
	}

	sink := &logSink{}
	prev := slog.Default()
	slog.SetDefault(slog.New(sink))
	f.handOffSeed("jevons-po", pending)
	slog.SetDefault(prev)

	rec, found := sink.find(slog.LevelError, "handover hand-off failed; it stays pending for the next launch")
	if !found {
		t.Fatal("the one instrument that told the truth is gone: no fail-closed ERROR for an undelivered seed")
	}
	var detail string
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == "err" {
			detail = fmt.Sprint(a.Value.Any())
		}
		return true
	})
	if !strings.Contains(detail, context.DeadlineExceeded.Error()) {
		t.Errorf("the line does not carry why it failed: err=%q", detail)
	}

	// Fail-closed means the record survives. A hand-off that reported success
	// here would consume the pointer and the successor would come up cold with
	// nobody able to tell.
	saved, ok, err := store.Get("jevons-po")
	if err != nil || !ok {
		t.Fatalf("record lost after a failed hand-off: ok=%v err=%v", ok, err)
	}
	if !saved.Usable() {
		t.Fatal("record marked delivered despite the seed never becoming a turn")
	}
}

// The other direction, so the assertion above is a discriminator and not a
// constant: a seed that IS delivered marks the record and logs no failure.
func TestHandoverHandOffMarksDeliveredWhenTheTurnHappens(t *testing.T) {
	const session = "019fd13d-e500-7913-b96c-981e50aa2e27"
	f, store, _ := migrateFixture(t, session, true)
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	pending, _, err := store.Get("jevons-po")
	if err != nil {
		t.Fatal(err)
	}

	var seen string
	f.seedDeliver = func(_, seed string) (string, error) {
		seen = seed
		return "read my predecessor's transcript; resuming", nil
	}

	sink := &logSink{}
	prev := slog.Default()
	slog.SetDefault(slog.New(sink))
	f.handOffSeed("jevons-po", pending)
	slog.SetDefault(prev)

	if _, found := sink.find(slog.LevelError, "handover hand-off failed; it stays pending for the next launch"); found {
		t.Fatal("a delivered seed reported as failed")
	}
	if seen == "" {
		t.Fatal("nothing was handed to the successor")
	}
	saved, ok, err := store.Get("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	if ok && saved.Usable() {
		t.Fatal("a delivered seed is still pending — the successor will be seeded twice")
	}
}
