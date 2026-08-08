// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// jevons-head is the desktop menu-bar/tray head for Jevons (🎯T27.7).
// It launches, connects to jevonsd over /ws/remote, and maintains a
// MenuModel with one section per enabled provider. Platform chrome
// (NSStatusItem / system tray) is optional; the decidable product is
// the model — use -json-once for probes and hermetics.
//
//	jevons-head -url http://127.0.0.1:13705 -json-once
//	jevons-head -url http://127.0.0.1:13705          # run until signal
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marcelocantos/jevons/internal/desktop"
)

func main() {
	url := flag.String("url", desktop.DefaultBaseURL, "jevonsd origin (http://host:port)")
	jsonOnce := flag.Bool("json-once", false, "connect, fetch sections once, print JSON, exit")
	inventory := flag.Bool("inventory", false, "print mnemo T85/T86/T89 supersession inventory and exit")
	flag.Parse()

	if *inventory {
		out := map[string]any{
			"supersedes": desktop.SupersededMnemoTargets,
			"signals":    desktop.MnemoSupersessionInventory(),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		if miss := desktop.InventoryComplete(desktop.MnemoSupersessionInventory()); len(miss) > 0 {
			fmt.Fprintf(os.Stderr, "inventory incomplete: %v\n", miss)
			os.Exit(1)
		}
		return
	}

	head := desktop.NewHead()
	client := &desktop.Client{BaseURL: *url, Head: head}

	if *jsonOnce {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		model, err := client.FetchSectionsOnce(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "jevons-head: %v\n", err)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(model)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("jevons-head starting", "url", *url)
	// Reconnect loop: the head stays up across daemon restarts (T194).
	for {
		if ctx.Err() != nil {
			return
		}
		err := client.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		model := head.Model()
		slog.Warn("jevons-head disconnected — reconnecting",
			"err", err, "sections", len(model.Sections))
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
