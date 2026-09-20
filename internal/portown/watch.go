// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package portown

import (
	"context"
	"log/slog"
	"time"
)

// Notifier writes one notice to the owner's journal. server.NotifyOwnerNote
// satisfies it — the 🎯T415 path, no agent in the way.
type Notifier func(subject, kind, text string) bool

// InspectArgs is one observation of the supervised port.
type InspectArgs struct {
	Port   int
	List   Lister
	Prober func(port int) Fleet
}

// Inspect lists listeners and probes both owner URLs.
func Inspect(args InspectArgs) (Conflict, Fleet) {
	list := args.List
	if list == nil {
		list = ListLSOF
	}
	prober := args.Prober
	if prober == nil {
		prober = ProbeAgents
	}
	listeners, err := list(args.Port)
	if err != nil {
		slog.Warn("portown: list listeners", "port", args.Port, "err", err)
	}
	return Classify(listeners), prober(args.Port)
}

// WatchLoop inspects the port on a ticker and tells the owner when a
// squatter appears. Repeats of the same text are skipped; going quiet
// resets so a later squatter is news again. It never restarts anything.
func WatchLoop(ctx context.Context, port int, every time.Duration, notify Notifier) {
	WatchLoopInspect(ctx, InspectArgs{Port: port}, every, notify)
}

// WatchLoopInspect is the test seam.
func WatchLoopInspect(ctx context.Context, args InspectArgs, every time.Duration, notify Notifier) {
	if every <= 0 {
		every = 30 * time.Second
	}
	var last string
	report := func() {
		c, fleet := Inspect(args)
		text := AlarmText(c, fleet)
		if text == "" {
			last = ""
			return
		}
		if text == last {
			return
		}
		if notify != nil && notify(NoticeSubject, NoticeKind, text) {
			last = text
			slog.Warn("portown: owner notified", "port", args.Port)
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
