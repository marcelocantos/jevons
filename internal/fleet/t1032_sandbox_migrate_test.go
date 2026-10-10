// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// Exercise the production wrapper and Claudia's persisted-destination retry,
// with provider capability enforcement and no provider credentials/processes.
func TestT1032StoppedSandboxMigration(t *testing.T) {
	f, reg := migrateStoppedFixture(t, "codex-source")
	src := *reg.Def("jevons-po")
	src.Provider = claudia.ProviderCodex
	src.SandboxMode = "workspace-write"
	src.SandboxWritableRoots = []string{t.TempDir()}
	src.SandboxNetworkAccess = true
	src.SandboxGitWrite = true
	if err := reg.Register(src); err != nil {
		t.Fatal(err)
	}
	summaries, refusals, launches := 0, 0, 0
	var destSession string
	reg.SetMigrationSummarizer(func(context.Context, claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
		summaries++
		return claudia.MigrationTransferResult{Brief: "continue violet work"}, nil
	})
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Adopt: func(claudia.Config) (*claudia.Agent, error) { return nil, claudia.ErrNoSessionWindow },
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			if cfg.AdoptOnly {
				return nil, claudia.ErrNoSessionWindow
			}
			launches++
			if destSession == "" {
				destSession = cfg.SessionID
			} else if cfg.SessionID != destSession {
				t.Fatal("retry rotated destination")
			}
			if cfg.SandboxMode != "" || len(cfg.SandboxWritableRoots) > 0 || cfg.SandboxNetworkAccess || cfg.SandboxGitWrite {
				if err := claudia.CheckCapability(cfg.Provider, claudia.CapabilitySandboxPolicy); err != nil {
					refusals++
					return nil, err
				}
			}
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	f.SetRetainedHistory(func(string) (string, error) { return "user: continue violet work", nil })
	pending, err := f.PrepareMigration(src.Name, claudia.ProviderClaude, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop(src.Name) })
	if summaries != 1 || refusals != 1 || launches != 2 {
		t.Fatalf("summaries=%d refusals=%d launches=%d", summaries, refusals, launches)
	}
	dest := reg.Def(src.Name)
	if len(StaleProviderCaps(*dest)) != 0 || dest.SandboxGitWrite || dest.MigrationSeed != "" || dest.MigrationPendingStart {
		t.Fatalf("stale or pending destination: %+v", dest)
	}
	if !pending.Delivered || pending.NewSessionID != destSession || pending.OldSessionID != src.SessionID || !strings.Contains(pending.Brief, "violet") {
		t.Fatalf("handover lost: %+v", pending)
	}
	if !strings.Contains(CapDrop(src.Name), "sandbox_policy") {
		t.Fatal("reset not recorded")
	}
}

func TestT1032SandboxRestrictionRefusedBeforeTransfer(t *testing.T) {
	for _, mode := range []string{"read-only", "unknown-policy"} {
		t.Run(mode, func(t *testing.T) {
			f, reg := migrateStoppedFixture(t, "restricted-source")
			src := *reg.Def("jevons-po")
			src.Provider, src.SandboxMode = claudia.ProviderCodex, mode
			if err := reg.Register(src); err != nil {
				t.Fatal(err)
			}
			reg.SetMigrationSummarizer(func(context.Context, claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
				t.Fatal("restriction crossed transfer boundary")
				return claudia.MigrationTransferResult{}, nil
			})
			_, err := f.PrepareMigration(src.Name, claudia.ProviderClaude, true)
			if !errors.Is(err, ErrProviderSwitchWouldWeaken) {
				t.Fatalf("error=%v", err)
			}
			if !reflect.DeepEqual(*reg.Def(src.Name), src) {
				t.Fatal("refusal changed source")
			}
		})
	}
}

func TestT1032FailedTransferKeepsSourceSandbox(t *testing.T) {
	f, reg := migrateStoppedFixture(t, "intact-source")
	src := *reg.Def("jevons-po")
	src.Provider, src.SandboxMode = claudia.ProviderCodex, "workspace-write"
	src.SandboxGitWrite = true
	if err := reg.Register(src); err != nil {
		t.Fatal(err)
	}
	reg.SetMigrationSummarizer(func(context.Context, claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
		return claudia.MigrationTransferResult{}, errors.New("transfer unavailable")
	})
	f.SetRetainedHistory(func(string) (string, error) { return "user: keep working", nil })
	if _, err := f.PrepareMigration(src.Name, claudia.ProviderClaude, false); err == nil {
		t.Fatal("failed transfer accepted")
	}
	if !reflect.DeepEqual(*reg.Def(src.Name), src) {
		t.Fatal("failed transfer weakened source")
	}
}
