// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestIsolatedBrokerCommandNestsSocketSeedAndOverride(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	cmd := isolatedBrokerCommand(nil, "claudia", root, state, filepath.Join(root, "server.ts"))
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, usageOverrideFlag) {
		t.Fatalf("broker args lack T234 override: %v", cmd.Args)
	}
	if got := isolatedBrokerSocketPath(root); !strings.HasPrefix(got, isolatedBrokerStateDir(root)+string(os.PathSeparator)) {
		t.Fatalf("broker socket %q is outside private state %q", got, isolatedBrokerStateDir(root))
	}
	if got := isolatedBrokerMCPSeed(root); !strings.HasPrefix(got, isolatedBrokerStateDir(root)+string(os.PathSeparator)) {
		t.Fatalf("mcp-seed %q is outside private state", got)
	}
	if findEnv(cmd.Env, "CLAUDIA_OMP_SOCKET") != filepath.Join(isolatedBrokerStateDir(root), "omp.sock") {
		t.Fatalf("omp socket=%q, want sibling of nested broker socket", findEnv(cmd.Env, "CLAUDIA_OMP_SOCKET"))
	}
}

func TestIsolatedBrokerEnvStripsSupervisorAndConsumer(t *testing.T) {
	env := isolatedBrokerEnv([]string{
		"CLAUDIA_CONSUMER_STATE_DIR=/tmp/escape",
		"SUPERVISOR_ENABLED=1",
		"SUPERVISOR_SERVER_URL=http://supervisor",
		"SUPERVISOR_PROCESS_NAME=jevonsd",
		"HOME=/Users/x",
	}, "/tmp/a/state/broker.sock", "/tmp/a/state/omp.sock", "/tmp/a/server.ts",
		"/tmp/a", "/tmp/a/spool")
	for _, name := range []string{"CLAUDIA_CONSUMER_STATE_DIR", "SUPERVISOR_ENABLED", "SUPERVISOR_SERVER_URL", "SUPERVISOR_PROCESS_NAME"} {
		if got := findEnv(env, name); got != "" {
			t.Fatalf("%s=%q, want stripped", name, got)
		}
	}
	if findEnv(env, "HOME") != "/Users/x" {
		t.Fatal("unrelated env dropped")
	}
}

func TestPrepareIsolatedBrokerStateIsPrivate(t *testing.T) {
	root := t.TempDir()
	if err := prepareIsolatedBrokerState(root); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{isolatedBrokerStateDir(root), isolatedBrokerMCPSeed(root)} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode %o, want 0700", dir, info.Mode().Perm())
		}
	}
}

func TestDestEmptyOverrideReadingsCoverEveryPlan(t *testing.T) {
	got := destEmptyOverrideReadings()
	if len(got) != 4 {
		t.Fatalf("dest-empty readings=%d, want 4 so dest is empty rather than unread", len(got))
	}
	by := map[string]float64{}
	for _, u := range got {
		if len(u.Windows) == 0 || u.Windows[0].RemainingPercent == nil {
			t.Fatalf("missing weekly remaining: %+v", u)
		}
		by[string(u.Provider)] = *u.Windows[0].RemainingPercent
	}
	if by["grok"] != 30 || by["claude"] != 0 || by["codex"] != 0 || by["cursor"] != 0 {
		t.Fatalf("dest-empty remaining %+v", by)
	}
}

func TestHotSwitchOverrideReadingsKeepDestHealthy(t *testing.T) {
	got := hotSwitchOverrideReadings("codex")
	by := map[string]float64{}
	for _, u := range got {
		by[string(u.Provider)] = *u.Windows[0].RemainingPercent
	}
	if by["claude"] != 0 || by["codex"] != defaultPlanRemaining || by["grok"] != 0 || by["cursor"] != 0 {
		t.Fatalf("hot-switch remaining %+v", by)
	}
}

func TestJourneyNeedsBrokerCoversPlanAndLaunchIds(t *testing.T) {
	need := []claudia.Provider{
		"codex", "openai-codex", "claude", "anthropic", "grok", "xai-oauth", "cursor",
	}
	for _, p := range need {
		if !journeyNeedsBroker(p) {
			t.Errorf("journeyNeedsBroker(%q)=false; sidecar isolate needs a broker so OMP can read the Keychain", p)
		}
	}
	if journeyNeedsBroker("bedrock") {
		t.Error("bedrock is not a sidecar and must not start an isolated broker")
	}
}

func TestIsolatedBrokerRootHasNoSymlinkAncestors(t *testing.T) {
	root, err := newIsolatedBrokerRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := prepareIsolatedBrokerState(root); err != nil {
		t.Fatal(err)
	}
	for p := isolatedBrokerStateDir(root); ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("symlink ancestor %s would be refused by Claudia T234 usage override", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
}

func TestIsolatedBrokerControlSocketFitsUnixPath(t *testing.T) {
	root, err := newIsolatedBrokerRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := prepareIsolatedBrokerState(root); err != nil {
		t.Fatal(err)
	}
	control := isolatedBrokerSocketPath(root) + ".control"
	if len(control) >= darwinUnixPathMax {
		t.Fatalf("control socket %q is %d bytes, Darwin max %d", control, len(control), darwinUnixPathMax)
	}
	if symlinkInAncestry(isolatedBrokerStateDir(root)) {
		t.Fatalf("state dir %s has a symlink ancestor; Claudia T234 would refuse it", isolatedBrokerStateDir(root))
	}
}
