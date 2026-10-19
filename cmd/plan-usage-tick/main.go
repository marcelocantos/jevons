// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command plan-usage-tick records subscription plan readings and nothing
// else. It does not start the broker, seats, or policy sweeps. The cockpit
// sparklines read plan-usage-readings.db; this process appends a row when
// jevonsd is not running.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan-usage-tick:", err)
		os.Exit(1)
	}
	state := flag.String("state", filepath.Join(home, ".jevons"), "directory holding plan-usage-readings.db")
	every := flag.Duration("interval", planusage.DefaultRefresh, "how often to record a reading")
	flag.Parse()

	store, err := planusage.OpenReadingStore(planusage.DefaultReadingsPath(*state))
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan-usage-tick:", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var reader *planusage.Reader
	reader = planusage.NewReader(planusage.ReaderArgs{
		History:      store,
		Refresh:      *every,
		FetchTimeout: 45 * time.Second,
		OnUpdate:     func() { logSnapshot(reader) },
	})
	slog.Info("plan-usage-tick recording only",
		"db", planusage.DefaultReadingsPath(*state),
		"interval", every.String())
	reader.Run(ctx)
}

func logSnapshot(reader *planusage.Reader) {
	snap := reader.Snapshot()
	if snap.Pending {
		slog.Warn("plan-usage-tick: no reading yet", "err", snap.Error)
		return
	}
	for _, b := range snap.Backends {
		if b.Status != planusage.StatusAvailable {
			slog.Info("plan-usage-tick", "provider", b.Provider, "status", b.Status, "reason", b.Reason)
			continue
		}
		attrs := []any{"provider", b.Provider, "age", time.Duration(b.AgeSeconds) * time.Second, "stale", b.Stale}
		for _, w := range b.Windows {
			if w.RemainingPercent == nil {
				continue
			}
			attrs = append(attrs, w.Name, fmt.Sprintf("%.0f%%", *w.RemainingPercent))
		}
		slog.Info("plan-usage-tick", attrs...)
	}
}
