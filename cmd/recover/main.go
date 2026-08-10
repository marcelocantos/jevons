// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// recover diagnoses a stuck fleet agent, outliving the daemon (🎯T415.1).
//
//	recover -stuck <agent> [-max 5] [-budget 20m] [-state ~/.jevons]
//
// Launched DETACHED by jevonsd, in its own session, so that killing,
// restarting or crashing the daemon does not kill it. That is the whole
// point: a recovery agent parented to jevonsd cannot diagnose or repair
// "the daemon is broken", which is among the failures it most needs to
// handle. On 2026-08-09/10 exactly that shape took the daemon down twice
// — restart-daily-jevonsd killed the daemon, the shutdown stopped every
// agent including the one that had invoked the script, and the script
// died before starting the replacement.
//
// SURVIVING THE DAEMON MEANS LOSING ITS TOOLS. jevonsmcp is served by
// jevonsd, so this process cannot use jevons_agent_list,
// jevons_transcript_read, or anything else routed through the daemon —
// they are gone precisely when they are needed. The brief therefore names
// filesystem paths and shell primitives, the same toolset a human uses to
// diagnose this.
//
// The loop is deliberately dumb (a "ralph" loop): re-run the same brief
// until resolved. No orchestration, no state machine — the complexity of
// a recovery protocol is itself a failure mode. But it is BOUNDED, in
// iterations and wall-clock, because an unbounded agent loop is how 907M
// tokens went in 16 hours, and quota exhaustion is a fault the loop would
// worsen while being unable to fix it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("recover", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	stuck := fs.String("stuck", "", "name of the stuck agent to diagnose (required)")
	maxIter := fs.Int("max", 5, "iteration cap")
	budget := fs.Duration("budget", 20*time.Minute, "wall-clock budget")
	stateDir := fs.String("state", "", "jevons state dir (default ~/.jevons)")
	repo := fs.String("repo", "", "jevons repo root, for scripts")
	agentCmd := fs.String("agent", "claude", "agent binary to drive")
	dry := fs.Bool("dry-run", false, "print the brief and the plan, run nothing")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *stuck == "" {
		fmt.Fprintln(os.Stderr, "recover: -stuck is required")
		return 2
	}
	if *stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "recover: %v\n", err)
			return 2
		}
		*stateDir = filepath.Join(home, ".jevons")
	}

	brief := Brief(*stuck, *stateDir, *repo)
	if *dry {
		fmt.Println(brief)
		return 0
	}

	deadline := time.Now().Add(*budget)
	for i := 1; i <= *maxIter; i++ {
		if ok, why := Resolved(*stateDir, *stuck, time.Now()); ok {
			fmt.Fprintf(os.Stderr, "recover: %s resolved (%s) after %d iteration(s)\n", *stuck, why, i-1)
			return 0
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "recover: budget exhausted after %d iteration(s)\n", i-1)
			return exhausted(*stateDir, *stuck, "wall-clock budget exhausted")
		}
		cmd := exec.Command(*agentCmd, "--permission-mode", "bypassPermissions", "-p", brief)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			// A failed iteration is ordinary: the agent may be rate
			// limited, the binary missing, the fleet mid-restart. Keep
			// going until the bound, then say so.
			fmt.Fprintf(os.Stderr, "recover: iteration %d failed: %v\n", i, err)
		}
	}
	if ok, why := Resolved(*stateDir, *stuck, time.Now()); ok {
		fmt.Fprintf(os.Stderr, "recover: %s resolved (%s)\n", *stuck, why)
		return 0
	}
	return exhausted(*stateDir, *stuck, fmt.Sprintf("iteration cap (%d) reached", *maxIter))
}

// exhausted records that the loop gave up, and TELLS THE OWNER ITSELF.
//
// The trigger is this code, not the agent's judgement — an agent deciding
// whether to raise the alarm can decline to, and the whole design rests
// on the alarm being unconditional. It appends straight to the owner's
// chat journal rather than asking the daemon to, because the daemon may
// be the thing that is broken; that is the same rule as the parent
// target's deterministic notice, applied to a process that has outlived
// the daemon.
//
// The marker file is written too, so a live daemon can reconcile its own
// notice, but nothing depends on it being read.
func exhausted(stateDir, stuck, why string) int {
	text := fmt.Sprintf(
		"⚠️ Recovery gave up on %s: %s.\n\n"+
			"The agent is still stuck and has been left that way deliberately, so the cause is"+
			" still there to be found. Automated diagnosis did not reach a conclusion — that is"+
			" itself a finding, and it now needs a human.",
		stuck, why)
	if err := appendOwnerNote(stateDir, stuck, "recovery-gave-up", text); err != nil {
		// Loud, and still not fatal: stderr goes to the recovery log.
		fmt.Fprintf(os.Stderr, "recover: OWNER NOT NOTIFIED (%v) — gave up on %s (%s)\n", err, stuck, why)
	}

	dir := filepath.Join(stateDir, "recovery")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		body, _ := json.Marshal(map[string]string{
			"stuck":  stuck,
			"gaveUp": why,
			"at":     time.Now().UTC().Format(time.RFC3339),
		})
		_ = os.WriteFile(filepath.Join(dir, stuck+".gaveup.json"), body, 0o644)
	}
	fmt.Fprintf(os.Stderr, "recover: gave up on %s (%s)\n", stuck, why)
	return 1
}

// appendOwnerNote writes one owner-visible line to the durable chat
// journal, the same file jevonsd appends to and replays from.
//
// O_APPEND on a single small write is atomic on POSIX, so racing the
// daemon is safe. Writing here rather than calling the daemon is the
// point: this runs when the daemon may be gone.
func appendOwnerNote(stateDir, subject, kind, text string) error {
	line, err := json.Marshal(map[string]any{
		"type":      "agent_note",
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"text":      text,
		"notice":    map[string]string{"kind": kind, "subject": subject},
	})
	if err != nil {
		return err
	}
	path := filepath.Join(stateDir, "chatlog", "jevons.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Resolved reports whether the stuck agent is demonstrably working again.
//
// EVIDENCE, NOT ASSESSMENT. The recovery agent's own opinion that it
// fixed something does not end the loop — judgement fails in both
// directions, and declaring victory early is the worse error because it
// leaves a broken fleet under a resolved notice and nothing else will
// look. The evidence is that the agent's session transcript has been
// written recently: it is running AND has produced a turn.
func Resolved(stateDir, stuck string, now time.Time) (bool, string) {
	sid, ok := sessionOf(stateDir, stuck)
	if !ok {
		return false, "no registry row"
	}
	path, ok := transcriptOf(sid)
	if !ok {
		// No transcript at all is the phantom-session condition (🎯T409),
		// which is precisely a fault rather than a recovery.
		return false, "no transcript"
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false, "transcript unreadable"
	}
	if age := now.Sub(fi.ModTime()); age < ProducedWithin {
		return true, fmt.Sprintf("produced a turn %s ago", age.Round(time.Second))
	}
	return false, "no recent turn"
}

// ProducedWithin is how fresh a turn must be to count as working. Long
// enough to survive a slow turn, short enough that a stale transcript
// from before the incident cannot read as recovery.
const ProducedWithin = 3 * time.Minute

func sessionOf(stateDir, name string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(stateDir, "agents.json"))
	if err != nil {
		return "", false
	}
	var defs []struct {
		Name      string `json:"name"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(data, &defs) != nil {
		return "", false
	}
	for _, d := range defs {
		if d.Name == name {
			return d.SessionID, d.SessionID != ""
		}
	}
	return "", false
}

func transcriptOf(sessionID string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	hits, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl"))
	if len(hits) > 0 {
		return hits[0], true
	}
	hits, _ = filepath.Glob(filepath.Join(home, ".grok", "sessions", "*", sessionID, "chat_history.jsonl"))
	if len(hits) > 0 {
		return hits[0], true
	}
	return "", false
}

// Brief is the one-shot instruction, re-issued each iteration.
//
// It names paths rather than tools on purpose: this process outlives the
// daemon, so every jevonsmcp tool may be unavailable exactly when it is
// running.
func Brief(stuck, stateDir, repo string) string {
	return fmt.Sprintf(`You are a disposable recovery agent running OUTSIDE the jevons daemon.
Work out WHY %q stopped making progress. Report what you find.

DO NOT restart, kill, or otherwise unstick %[1]q. It has been left in its failed
state deliberately so the cause is still visible. Repairing it first destroys the
evidence — that is the single most important instruction here.

The daemon may be down. Do NOT rely on jevons MCP tools (jevons_agent_list,
jevons_transcript_read and friends are served BY the daemon and vanish with it).
Use the filesystem and shell:

  %s/agents.json            registry: names, sessions, materialized, parents
  %s/daily-jevonsd.log      daemon log: launches, failures, idle nudges
  %s/restart-daily.log      restart history
  ~/.claude/projects/*/     Claude session transcripts, one per session id
  ~/.grok/sessions/*/       Grok session transcripts
  %s                        repo root, for scripts and code

Decide whether the cause is the agent, its session, its mission, or the machinery
around it. If the daemon itself is down and that is the cause, you MAY restart it
(scripts/restart-daily-jevonsd.sh) — you are detached from it, so it cannot take
you with it.

File a bullseye target for what you find (name + acceptance), then report in one
paragraph. If you cannot determine a cause, say so plainly: an honest "unknown" is
worth more than a plausible guess.`, stuck, stateDir, stateDir, stateDir, orDot(repo))
}

func orDot(s string) string {
	if s == "" {
		return "(unknown — find it via `git rev-parse --show-toplevel`)"
	}
	return s
}
