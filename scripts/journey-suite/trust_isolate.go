// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

const trustJourneyBudget = 25 * time.Minute

// withTrustJourneyIsolate runs a brokered child isolate on the Cursor
// provider with workdir set to a throwaway git clone (🎯T1183).
func (s *suite) withTrustJourneyIsolate(run func(*suite, string) error) (result error) {
	if _, err := exec.LookPath("cursor-agent"); err != nil {
		return &outageError{
			step:  "J50-trust-journey",
			class: agenterr.ClassBackendUnavailable,
			msg:   "cursor-agent not on PATH",
		}
	}
	if err := backendCLIReady(string(claudia.ProviderCursor)); err != nil {
		return asOutage("J50-trust-journey", err)
	}
	root, err := jevonsRepoRoot()
	if err != nil {
		return err
	}
	state, err := os.MkdirTemp("", "jevons-trust-journey-")
	if err != nil {
		return err
	}
	repo, err := prepareTrustJourneyRepo(root, state)
	if err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	gateDir := filepath.Join(state, "gates")
	if err := os.MkdirAll(gateDir, 0o700); err != nil {
		_ = os.RemoveAll(state)
		return err
	}

	port, err := freePort()
	if err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	if err := portguard.RefuseDevelopment(port); err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	config, err := os.ReadFile(s.cfgPath)
	if err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	configText := strings.ReplaceAll(string(config), s.stateDir, state)
	oldPort := fmt.Sprintf("port: %d\n", s.port)
	if !strings.Contains(configText, oldPort) {
		_ = os.RemoveAll(state)
		return fmt.Errorf("trust isolate config has no port %d", s.port)
	}
	configText = strings.Replace(configText, oldPort, fmt.Sprintf("port: %d\n", port), 1)
	configText = strings.Replace(configText, "workdir: "+quoteYAML(state), "workdir: "+quoteYAML(repo), 1)
	configText = strings.Replace(configText, "provider: "+string(s.provider), "provider: cursor", 1)
	configText = trustReplacePersonaNotes(configText)
	if err := os.WriteFile(filepath.Join(state, "config.yaml"), []byte(configText), 0o600); err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	if err := quietAmbient(state); err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	logFile, err := os.Create(filepath.Join(state, "jevonsd.log"))
	if err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	child := &suite{
		host: fmt.Sprintf("127.0.0.1:%d", port), stateDir: state, provider: claudia.ProviderCursor,
		port: port, cfgPath: filepath.Join(state, "config.yaml"),
		logPath: filepath.Join(state, "jevonsd.log"), logFile: logFile,
		workdir: repo, daemonBin: s.daemonBin,
		daemonEnv: append([]string(nil), s.daemonEnv...),
	}
	child.daemonEnv = append(child.daemonEnv, "JEVONS_GATE_DIR="+gateDir)

	var b *isolatedBroker
	defer func() {
		stopErr := child.signalStop(8 * time.Second)
		brokerErr := b.close()
		if result != nil {
			// Keep the evidence of a failed trust journey beside the suite's
			// own state, as the broker isolate does: J50 failed twice at the
			// owner-reply step with nothing left to read (🎯T1183).
			for _, name := range []string{"jevonsd.log", "claudia-broker.log", "omp-sidecar.log", "jevons.db", "jevons.db-wal", "jevons.db-shm"} {
				if body, err := os.ReadFile(filepath.Join(state, name)); err == nil {
					_ = os.WriteFile(filepath.Join(s.stateDir, "trust-"+name), body, 0o600)
				}
			}
			if out, err := trustGitOutput(repo, "log", "--oneline", "--decorate", "-30"); err == nil {
				_ = os.WriteFile(filepath.Join(s.stateDir, "trust-repo-log.txt"), []byte(out), 0o600)
			}
		}
		logErr := logFile.Close()
		guardErr := s.devGuard.noteStore(state)
		result = errors.Join(result, stopErr, brokerErr, logErr, guardErr, os.RemoveAll(state))
	}()
	b, err = child.startIsolatedBroker()
	if err != nil {
		return err
	}
	child.brokerSocket = b.socket
	child.broker = b
	if err := child.startDaemon(); err != nil {
		return fmt.Errorf("start trust isolate: %w", err)
	}
	return run(child, repo)
}

func quoteYAML(s string) string {
	return fmt.Sprintf("%q", s)
}

func trustReplacePersonaNotes(config string) string {
	const marker = "persona_notes: |"
	i := strings.Index(config, marker)
	if i < 0 {
		return config
	}
	// Journey config is a single persona_notes block at EOF; the default text
	// says "Do not spawn workers", which contradicts T1183. Replace the whole block.
	notes := `
  T1183 trust journey: the owner asked for real coding work on this repo (workdir).
  Route it to jevons-po with jevons_agent_send; do not implement it yourself.
  If jevons-po is not in the fleet yet, start it first with jevons_agent_start
  (name=jevons-po, role=product-owner, workdir=trust-repo, parent=jevons).
  The PO must spawn a Cursor worker in its own worktree, land via go run ./cmd/integrate
  with a GREEN bin/gate -clean on pkg/calc, then you reply once citing the landed SHA.
  Do not send interim progress or delegation updates in owner chat — exactly one final reply.
  Ignore sentinel po_stall filings in this isolate; do not mass-spawn against the ledger.
  When the PO reports landed + GREEN gate, your next owner-visible message must be one
  sentence that includes the full landed commit SHA (no empty end_turn).
  Use jevons_* MCP tools only; never read or list ~/.jevons (development daemon).
`
	return config[:i+len(marker)] + notes + "\n"
}
