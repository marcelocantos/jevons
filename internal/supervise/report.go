// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"context"
	"log/slog"
	"time"
)

// OutageKind is the notice kind the owner's journal records.
const OutageKind = "daemon-outage"

// OutageSubject names what the notice is about.
const OutageSubject = "daily-jevonsd"

// Notifier writes one notice to the owner's own journal, returning
// whether it landed. server.Server.NotifyOwnerNote satisfies it — the
// 🎯T415 path, chosen because it is daemon code writing to the owner's
// chat log with no agent and no delivery in the way.
type Notifier func(subject, kind, text string) bool

// ReportPending tells the owner about every outage recorded but not yet
// reported, and returns how many landed.
//
// The daemon reports its own outages because the watchdog cannot: during
// the outage there is nothing to write a chat line into, and by the time
// the watchdog sees serving resume, the daemon it would have told is a
// process that started thirty seconds ago knowing nothing. The record on
// disk is the handover between them.
func ReportPending(dir string, notify Notifier) (int, error) {
	if notify == nil {
		return 0, nil
	}
	all, err := LoadOutages(dir)
	if err != nil {
		return 0, err
	}
	reported := map[string]bool{}
	for _, o := range all {
		if o.Reported || o.ID == "" {
			continue
		}
		if !notify(OutageSubject, OutageKind, o.Text()) {
			// No journal to write to. Leave the record unreported so a
			// later, better-configured run still tells the owner.
			continue
		}
		reported[o.ID] = true
	}
	if len(reported) == 0 {
		return 0, nil
	}
	if err := MarkReported(dir, reported); err != nil {
		// The owner has been told; failing to record that risks a
		// duplicate notice, which is the better of the two failures.
		return len(reported), err
	}
	return len(reported), nil
}

// ReportLoop reports pending outages now and on every tick until ctx
// ends.
//
// A ticker rather than a startup-only sweep because the ordering runs
// the wrong way: the watchdog's restart brings the daemon up first and
// only then, once the probe sees serving, does it write the record. A
// daemon that only looked at boot would miss the very outage it was
// restarted for.
func ReportLoop(ctx context.Context, dir string, every time.Duration, notify Notifier) {
	if every <= 0 {
		every = 15 * time.Second
	}
	report := func() {
		n, err := ReportPending(dir, notify)
		if err != nil {
			slog.Warn("supervise: outage report", "err", err)
		}
		if n > 0 {
			slog.Warn("supervise: outage reported to the owner", "count", n)
		}
	}
	report()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			report()
		}
	}
}
