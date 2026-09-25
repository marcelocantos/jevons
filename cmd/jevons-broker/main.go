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
	"strings"
	"syscall"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/daemon"

	"github.com/marcelocantos/jevons/internal/seatreg"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: jevons-broker serve|refresh-plans|login-plans|remint|smoke [provider...]")
		return 2
	}
	var err error
	switch args[0] {
	case "serve":
		err = serve(args[1:])
	case "refresh-plans":
		err = refreshPlans(args[1:])
	case "login-plans":
		err = loginPlans(args[1:])
	case "remint":
		err = remintOnly(args[1:])
	case "smoke":
		err = smoke(args[1:])
	default:
		fmt.Fprintln(os.Stderr, "usage: jevons-broker serve|refresh-plans|login-plans|remint|smoke [provider...]")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "jevons-broker:", err)
		return 1
	}
	return 0
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "grants directory (default: claudia state dir)")
	socket := fs.String("socket", "", "listen socket (default: CLAUDIA_BROKER_SOCKET or ~/.local/state/claudia/broker.sock)")
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

	if sock, err := claudia.EnsureOMPSidecar(context.Background()); err != nil {
		log.Warn("omp sidecar not ready; subscription seats will retry on Launch", "err", err)
	} else {
		log.Info("omp sidecar listening", "socket", sock)
	}
	claudia.SetOMPToolExec(claudia.DefaultOMPToolExec)
	// One Keychain read at startup. Later gets and puts stay in memory.
	// The write, if the copy changed, happens on the way out.
	// A rebuilt broker is refused by the Keychain ACL until the owner
	// approves it again (🎯T865). Serve must not wait on that dialog:
	// the socket has to come up so seats can be reclaimed.
	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 8*time.Second)
	if err := claudia.OpenOMPPlans(refreshCtx); err != nil {
		log.Warn("plan keychain was not read; Launch will not prompt again", "err", err)
	} else {
		defer func() {
			if err := claudia.FlushOMPPlans(context.Background()); err != nil {
				log.Warn("plan keychain flush failed", "err", err)
			}
		}()
		if refreshed, skipped, err := claudia.RefreshOMPPlans(refreshCtx); err != nil {
			log.Warn("plan refresh failed; Launch will retry", "err", err, "refreshed", refreshed, "skipped", skipped)
		} else {
			log.Info("plan credentials", "refreshed", refreshed, "skipped", skipped)
		}
	}
	refreshCancel()
	if n, parked, err := remintFleet("", *stateDir); err != nil {
		log.Warn("sidecar remint failed", "err", err)
	} else {
		if n > 0 {
			log.Info("sidecar remint", "seats", n)
		}
		if parked > 0 {
			log.Info("parked leftover grants", "seats", parked)
		}
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

func loginPlans(args []string) error {
	fs := flag.NewFlagSet("login-plans", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := fs.Args()
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "jevons-broker: login-plans opens a browser for each missing subscription login (anthropic, openai-codex, cursor, xai-oauth)")
	} else {
		fmt.Fprintf(os.Stderr, "jevons-broker: login-plans for %s\n", strings.Join(ids, ", "))
	}
	if err := claudia.OpenOMPPlans(context.Background()); err != nil {
		return err
	}
	n, err := claudia.LoginOMPPlans(context.Background(), ids...)
	flushErr := claudia.FlushOMPPlans(context.Background())
	if err != nil {
		return err
	}
	if flushErr != nil {
		return flushErr
	}
	fmt.Printf("logged in %d\n", n)
	return nil
}

func refreshPlans(args []string) error {
	fs := flag.NewFlagSet("refresh-plans", flag.ContinueOnError)
	force := fs.Bool("force", false, "refresh even when the access token is still live")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *force {
		os.Setenv("OMP_FORCE_REFRESH", "1")
	}
	if err := claudia.OpenOMPPlans(context.Background()); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		os.Setenv("OMP_REFRESH_ONLY", strings.Join(fs.Args(), ","))
	}
	refreshed, skipped, err := claudia.RefreshOMPPlans(context.Background())
	flushErr := claudia.FlushOMPPlans(context.Background())
	if err != nil {
		return err
	}
	if flushErr != nil {
		return flushErr
	}
	fmt.Printf("refreshed %d skipped %d\n", len(refreshed), len(skipped))
	for _, id := range refreshed {
		fmt.Println("refreshed", id)
	}
	for _, id := range skipped {
		fmt.Println("skipped", id)
	}
	return nil
}

func remintOnly(args []string) error {
	fs := flag.NewFlagSet("remint", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	n, parked, err := remintFleet("", "")
	if err != nil {
		return err
	}
	fmt.Printf("reminted %d parked %d\n", n, parked)
	return nil
}

func remintFleet(spoolDir, stateDir string) (int, int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0, err
	}
	fleet, err := seatreg.New(seatreg.Path(filepath.Join(home, ".jevons")))
	if err != nil {
		return 0, 0, err
	}
	n, err := seatreg.RemintRegistry(fleet, spoolDir)
	if err != nil {
		return n, 0, err
	}
	if strings.TrimSpace(stateDir) == "" {
		if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" && filepath.IsAbs(xdg) {
			stateDir = filepath.Join(xdg, "claudia")
		} else {
			stateDir = filepath.Join(home, ".local", "state", "claudia")
		}
	}
	grants, err := seatreg.New(seatreg.GrantsPath(stateDir))
	if err != nil {
		return n, 0, err
	}
	gn, err := seatreg.RemintRegistry(grants, spoolDir)
	if err != nil {
		return n + gn, 0, err
	}
	parked, err := seatreg.ParkNonFleetAutoStart(grants, fleet)
	return n + gn, parked, err
}
