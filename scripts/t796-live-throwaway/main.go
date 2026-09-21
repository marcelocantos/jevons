// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command t796-live-throwaway drives a THROWAWAY seat on the real claudia
// broker to show 🎯T796's jevons-side guard: with the seat's grant held by
// another connection, a jevonsd-style adopt is refused rather than stacking or
// stopping the broker's client. Never names the overseer.
//
//	hold      grant the seat on this connection and keep it until killed
//	takeover  adopt as a restarted daemon would (guard installed), then send
//	guard-hung  the same hung broker and load, but call the one-client launch guard
//	          directly (claudia's own adopt blocks on a hung socket for the whole
//	          context and never reaches it): expect ErrClaudeHeldByBroker, no launch,
//	          no signal, the holder's pid alive after.
//	hung      takeover while the broker "does not answer" (🎯T796.1): the driver
//	          points CLAUDIA_BROKER_SOCKET at a listener that accepts and never
//	          replies, and burns every core, so claudia.BrokerAvailable times out
//	          exactly as on a loaded host while the real broker's client (from a
//	          prior hold) still holds the session. Expect a refusal, an owner
//	          notice and the client's pid alive afterwards.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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
	if os.Args[1] == "hung" || os.Args[1] == "guard-hung" {
		hungBroker()
	}
	if os.Args[1] == "guard-hung" {
		guardHung()
		return
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
	case "takeover", "hung":
		upgrade.LaunchRefusedNotifier = func(agent string, err error) {
			fmt.Printf("OWNER NOTICE (seat %s): %v\n", agent, err)
		}
		fmt.Println("holders before:", holderPIDs())
		names := upgrade.ReattachSeatsContext(ctx, reg, func(n string) bool { return n == seatName }, 1)
		_ = names
		a := reg.Get(seatName)
		fmt.Println("holders after:", holderPIDs())
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

// hungBroker makes claudia see a broker that accepts and never answers, and
// loads the host. The hung socket is only visible to this process.
func hungBroker() {
	dir, err := os.MkdirTemp("/tmp", "hb")
	if err != nil {
		panic(err)
	}
	sock := filepath.Join(dir, "b.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		panic(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c // never read, never reply
		}
	}()
	os.Setenv("CLAUDIA_BROKER_SOCKET", sock)
	os.Setenv("CLAUDIA_NO_BROKER", "0")
	for range runtime.NumCPU() {
		go func() {
			for {
			}
		}()
	}
	fmt.Println("hung broker at", sock, "and", runtime.NumCPU(), "busy loops")
}

func holderPIDs() string {
	out, _ := exec.Command("pgrep", "-f", "claude.*"+sessionID).Output()
	return string(out)
}

func guardHung() {
	before := holderPIDs()
	fmt.Println("holders before:", before)
	start := upgrade.GuardCursorStart(func(context.Context, claudia.Config) (*claudia.Agent, error) {
		fmt.Println("FAIL: the guard let a second client launch")
		os.Exit(1)
		return nil, nil
	})
	t0 := time.Now()
	_, err := start(context.Background(), claudia.Config{Provider: claudia.ProviderClaude, SessionID: sessionID})
	fmt.Printf("guard returned after %s: %v\n", time.Since(t0).Round(time.Millisecond), err)
	fmt.Println("refused with ErrClaudeHeldByBroker:", errors.Is(err, upgrade.ErrClaudeHeldByBroker))
	fmt.Println("holders after:", holderPIDs())
}
