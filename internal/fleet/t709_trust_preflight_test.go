// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/claudetrust"
)

// 🎯T709: a Claude seat must not be launched into Claude Code's
// workspace-trust modal. ge-po was minted on a freshly-cloned squz
// workdir on 2026-09-20 and vanished, repeatedly: the modal blocked the
// composer, the launch handshake timed out, and the owner saw only
// reaped_held on send. The preflight answers the question before the
// process reads its config.
//
// This exercises the method Launch calls, with the config path and owner
// roots redirected. Residual: that Launch calls it at all is a one-line
// wiring not covered here, because reg.Launch wants a live tmux process.
func withTrustSeams(t *testing.T, configPath string, roots []string) {
	t.Helper()
	oldPath, oldRoots := trustConfigPath, trustOwnerRoots
	trustConfigPath = func() string { return configPath }
	trustOwnerRoots = func() []string { return roots }
	t.Cleanup(func() { trustConfigPath, trustOwnerRoots = oldPath, oldRoots })
}

func newTrustFleet(t *testing.T, def claudia.AgentDef) *Claudia {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	return NewClaudia(reg)
}

func TestT709PreflightTrustsAClaudeSeatsWorkdir(t *testing.T) {
	const (
		name    = "ge-po"
		workdir = "/Users/marcelo/work/github.com/marcelocantos/squz"
	)
	config := filepath.Join(t.TempDir(), ".claude.json")
	withTrustSeams(t, config, []string{"/Users/marcelo/work"})

	f := newTrustFleet(t, claudia.AgentDef{
		Name: name, WorkDir: workdir, SessionID: "s-1",
		Provider: claudia.ProviderClaude, Purpose: claudia.PurposeWork,
	})
	f.preflightTrust(name)

	doc, err := os.ReadFile(config)
	if err != nil {
		t.Fatalf("preflight wrote no config: %v", err)
	}
	if !claudetrust.Accepted(doc, workdir) {
		t.Fatalf("the seat's workdir is still untrusted: %s", doc)
	}
}

// Only Claude asks this question, so only Claude seats get the write —
// a Grok or Codex seat must not have jevons editing ~/.claude.json on
// its behalf. An unknown agent name is a no-op, not a panic.
func TestT709PreflightTouchesNothingForOtherProviders(t *testing.T) {
	const workdir = "/Users/marcelo/work/github.com/marcelocantos/squz"
	config := filepath.Join(t.TempDir(), ".claude.json")
	withTrustSeams(t, config, []string{"/Users/marcelo/work"})

	f := newTrustFleet(t, claudia.AgentDef{
		Name: "jv-grok", WorkDir: workdir, SessionID: "s-2",
		Provider: claudia.ProviderGrok, Purpose: claudia.PurposeWork,
	})
	f.preflightTrust("jv-grok")
	f.preflightTrust("never-registered")

	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatalf("a non-Claude seat must not write the Claude config (err=%v)", err)
	}
}

// Trust is a safety boundary. A workdir outside the owner roots is
// refused: the launch still proceeds, and agenterr.ClassWorkspaceTrust
// is what tells the owner about the modal if it appears.
func TestT709PreflightRefusesWorkdirsOutsideOwnerRoots(t *testing.T) {
	const workdir = "/Users/marcelo/Downloads/someone-elses-repo"
	config := filepath.Join(t.TempDir(), ".claude.json")
	withTrustSeams(t, config, []string{"/Users/marcelo/work"})

	f := newTrustFleet(t, claudia.AgentDef{
		Name: "jv-stranger", WorkDir: workdir, SessionID: "s-3",
		Provider: claudia.ProviderClaude, Purpose: claudia.PurposeWork,
	})
	f.preflightTrust("jv-stranger")

	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatalf("jevons must not say yes for a workdir the owner never pointed it at (err=%v)", err)
	}
}
