// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command t796-live-throwaway drives a THROWAWAY seat on the real claudia
// broker to show 🎯T796's jevons-side guard: with the seat's grant held by
// another connection, a jevonsd-style adopt is refused rather than stacking or
// stopping the broker's client. Never names the overseer.
//
//	hold      grant the seat on this connection and keep it until killed
//	takeover  adopt as a restarted daemon would (guard installed), then send
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

const (
	seatName  = "jv-t796-throwaway"
	sessionID = "ee0e1d94-a7bd-4a9b-bf1d-66f2665c588b"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: t796-live-throwaway hold|takeover")
		os.Exit(2)
	}
	work := "/Users/marcelo/work/github.com/marcelocantos/jevons"
	_ = os.MkdirAll(os.TempDir(), 0o755)
	reg, err := claudia.NewRegistry(filepath.Join(os.TempDir(), "t796-throwaway-agents.json"))
	if err != nil {
		panic(err)
	}
	upgrade.InstallCursorLaunchGuard(reg)
	def := claudia.AgentDef{Name: seatName, WorkDir: work, SessionID: sessionID,
		Provider: claudia.ProviderClaude, AutoStart: true, TermLogPath: "-"}
	if err := reg.Register(def); err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch os.Args[1] {
	case "hold":
		a, err := reg.AdoptOrLaunchContext(ctx, seatName)
		if err != nil {
			panic(err)
		}
		fmt.Printf("hold: granted window=%s pid=%d\n", a.WindowID(), a.PID())
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		os.Exit(0) // no Stop: the broker keeps the seat running, like an upgrade exit
	case "takeover":
		names := upgrade.ReattachSeatsContext(ctx, reg, func(n string) bool { return n == seatName }, 1)
		_ = names
		a := reg.Get(seatName)
		if a == nil {
			fmt.Println("takeover: no agent (launch refused) — the broker's client was left alone")
			return
		}
		fmt.Printf("takeover: adopted window=%s pid=%d\n", a.WindowID(), a.PID())
		if err := a.Send("Reply with the single word ok."); err != nil {
			fmt.Println("takeover: send FAILED:", err)
			os.Exit(1)
		}
		fmt.Println("takeover: send accepted")
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}
}
