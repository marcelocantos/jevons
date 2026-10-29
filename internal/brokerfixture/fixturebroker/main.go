// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command fixturebroker is a Claudia broker for Jevons tests: the real
// daemon.New, in its own process, with its own state directory and socket,
// so the broker owns claudia-control.db and serves the control socket
// exactly as `claudia broker serve` does. It resumes no seats, hosts no MCP
// and fetches no plan usage. The test drives it over stdin, one JSON
// command per line, and reads one reply line per command:
//
//	{"op":"usage","usage":{...claudia.UsageFact...}}       → OK
//	{"op":"receipt","receipt":{...claudia.RequestReceipt...}} → OK
//
// The facts go through claudia.RecordObservedUsage and
// RecordObservedReceipt, the seat pump's own publish path. EOF on stdin, or
// SIGINT/SIGTERM, closes the daemon, which settles started obligations and
// releases the file and the socket. It prints "READY <pid>" once serving.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marcelocantos/claudia"
	_ "github.com/marcelocantos/claudia/controlplane" // links the broker's control-file publisher
	"github.com/marcelocantos/claudia/daemon"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

type command struct {
	Op        string                  `json:"op"`
	Usage     *claudia.UsageFact      `json:"usage,omitempty"`
	Receipt   *claudia.RequestReceipt `json:"receipt,omitempty"`
	Remaining *float64                `json:"remaining,omitempty"`
}

func main() {
	stateDir := flag.String("state-dir", "", "broker state directory")
	socket := flag.String("socket", "", "broker socket path")
	flag.Parse()
	if *stateDir == "" || *socket == "" {
		fmt.Fprintln(os.Stderr, "fixturebroker: -state-dir and -socket are required")
		os.Exit(2)
	}
	d, err := daemon.New(daemon.Options{
		SocketPath:     *socket,
		StateDir:       *stateDir,
		UsageFetch:     func(context.Context) ([]claudia.PlanUsage, error) { return nil, nil },
		DisableResume:  true,
		DisableIntel:   true,
		DisableMCPHost: true,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "fixturebroker:", err)
		os.Exit(1)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	lines := make(chan string)
	go func() {
		in := bufio.NewScanner(os.Stdin)
		in.Buffer(make([]byte, 1<<20), 1<<20)
		for in.Scan() {
			lines <- in.Text()
		}
		close(lines)
	}()
	fmt.Printf("READY %d\n", os.Getpid())
	for {
		select {
		case <-signals:
			exit(d)
		case line, ok := <-lines:
			if !ok {
				exit(d)
			}
			fmt.Println(run(line))
		}
	}
}

func run(line string) string {
	var cmd command
	if err := json.Unmarshal([]byte(line), &cmd); err != nil {
		return "ERR " + err.Error()
	}
	switch {
	case cmd.Op == "usage" && cmd.Usage != nil:
		claudia.RecordObservedUsage(cmd.Usage)
	case cmd.Op == "receipt" && cmd.Receipt != nil:
		claudia.RecordObservedReceipt(cmd.Receipt)
	case cmd.Op == "plan" && cmd.Remaining != nil:
		left := *cmd.Remaining
		used := 100 - left
		reset := time.Now().UTC().Add(7 * 24 * time.Hour)
		_, err := claudia.PollBrokerPlanUsage(context.Background(), claudia.SystemClock{}, func(_ context.Context, p claudia.Provider) (claudia.PlanUsage, error) {
			if seatprovider.Resolve(p).Plan != seatprovider.Claude {
				return claudia.PlanUsage{Status: claudia.PlanUsageUnavailable, Reason: "fixture"}, nil
			}
			return claudia.PlanUsage{Status: claudia.PlanUsageAvailable, Windows: []claudia.PlanWindow{{Name: claudia.PlanWindowWeekly, RemainingPercent: &left, UsedPercent: &used, ResetsAt: &reset, LimitWindow: 7 * 24 * time.Hour}}}, nil
		})
		if err != nil {
			return "ERR " + err.Error()
		}
	default:
		return "ERR unknown command " + cmd.Op
	}
	return "OK"
}

func exit(d *daemon.Daemon) {
	if err := d.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "fixturebroker: close:", err)
		os.Exit(1)
	}
	os.Exit(0)
}
