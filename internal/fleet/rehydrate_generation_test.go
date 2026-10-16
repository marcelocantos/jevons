// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// Exercise the actual recovery caller with a loaded materialized generation,
// absent vendor history and refused launch. Neither historical file layout
// nor legacy error prose can authorize discarding canonical sidecar history.
func TestLaunchRecoveringPreservesGenerationOnResumeRefusal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, provider := range []claudia.Provider{
		"", claudia.ProviderClaude, claudia.ProviderCodex, claudia.ProviderGrok,
		claudia.ProviderCursor, "anthropic", "openai-codex", "xai-oauth",
	} {
		for _, refusal := range []string{
			"canonical generation could not be loaded",
			"existing conversation required but JSONL not found at /missing/session.jsonl",
			"exclusive GROK_HOME unavailable for session generation",
			"acp session/load: Invalid params: existing conversation; refusing to mint a replacement session",
			"existing conversation required but no spool records for seat worker",
		} {
			t.Run(string(provider)+"/"+refusal, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "agents.json")
				reg, err := claudia.NewRegistry(path)
				if err != nil {
					t.Fatal(err)
				}
				want := claudia.AgentDef{Name: "worker", WorkDir: t.TempDir(),
					SessionID: "canonical-generation", Materialized: true, Provider: provider,
					Parent: "po", Purpose: claudia.PurposeWork, TargetID: "T1055"}
				if err := reg.Register(want); err != nil {
					t.Fatal(err)
				}
				reg, err = claudia.NewRegistry(path)
				if err != nil {
					t.Fatal(err)
				}
				reg.SetDirect(true)
				starts := 0
				wantErr := errors.New(refusal)
				reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
					starts++
					if cfg.SessionID != want.SessionID || !cfg.RequireResume {
						t.Errorf("launch abandoned saved generation: session=%q required=%v", cfg.SessionID, cfg.RequireResume)
					}
					return nil, wantErr
				}})
				seatstate.RegistryAuthority(reg).ObserveRegistry(reg)
				if _, err := LaunchRecovering(reg, want.Name); !errors.Is(err, wantErr) {
					t.Fatalf("launch did not preserve refusal: %v", err)
				}
				if starts != 1 {
					t.Fatalf("launch attempts=%d, want one without automatic remint", starts)
				}
				if st := seatstate.ReadRegistry(reg, want.Name); st.ResumeLost != seatstate.Unknown {
					t.Fatalf("historical storage inferred canonical loss: %+v", st)
				}
				reopened, err := claudia.NewRegistry(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := reopened.Def(want.Name); got == nil || !reflect.DeepEqual(*got, want) {
					t.Fatalf("refusal changed durable generation: got %+v, want %+v", got, want)
				}
			})
		}
	}
}
