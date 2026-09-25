//go:build sibling_claudia

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command jevons-broker is the seat broker process (🎯T866.7).
// It stays a separate process so a jevonsd bounce reclaims seats by name.
// Subscription seats go through the Oh My Pi sidecar; the sidecar is
// left running when this process exits.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/daemon"

	"github.com/marcelocantos/jevons/internal/seatreg"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: jevons-broker serve [flags]")
		return 2
	}
	if args[0] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: jevons-broker serve [flags]")
		return 2
	}
	if err := serve(args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "jevons-broker:", err)
		return 1
	}
	return 0
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "grants directory (default: ~/.jevons)")
	socket := fs.String("socket", "", "listen socket (default: CLAUDIA_BROKER_SOCKET or ~/.jevons/broker.sock)")
	logLevel := fs.String("log", "info", "log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(*logLevel)); err != nil {
		return fmt.Errorf("--log: %w", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
	slog.SetDefault(log)

	if *stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*stateDir = filepath.Join(home, ".jevons")
	}
	if *socket == "" {
		if env := os.Getenv("CLAUDIA_BROKER_SOCKET"); env != "" {
			*socket = env
		} else {
			*socket = filepath.Join(*stateDir, "broker.sock")
		}
	}

	if sock, err := claudia.EnsureOMPSidecar(context.Background()); err != nil {
		log.Warn("omp sidecar not ready; subscription seats will retry on Launch", "err", err)
	} else {
		log.Info("omp sidecar listening", "socket", sock)
	}
	claudia.SetOMPToolExec(claudia.DefaultOMPToolExec)
	if reg, err := seatreg.New(seatreg.Path(*stateDir)); err != nil {
		log.Warn("sidecar remint skipped", "err", err)
	} else if n, err := seatreg.RemintRegistry(reg, ""); err != nil {
		log.Warn("sidecar remint failed", "err", err)
	} else if n > 0 {
		log.Info("sidecar remint", "seats", n)
	}

	d, err := daemon.New(daemon.Options{
		SocketPath: *socket,
		StateDir:   *stateDir,
		Logger:     log,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	err = d.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	log.Info("jevons-broker stopped")
	return nil
}
