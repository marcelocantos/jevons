// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marcelocantos/claudia"
)

// t943Fleet registers and launches one running seat per name, on provider.
func t943Fleet(t *testing.T, seats map[string]claudia.Provider) (*claudia.Registry, *[]string) {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	for name, provider := range seats {
		if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: t.TempDir(), Provider: provider, SessionID: name + "-sid"}); err != nil {
			t.Fatal(err)
		}
	}
	launched := &[]string{}
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		*launched = append(*launched, cfg.Name)
		return claudia.NewStubAgent(nil), nil
	}})
	for name := range seats {
		if _, err := reg.Launch(name); err != nil {
			t.Fatal(err)
		}
	}
	*launched = nil
	return reg, launched
}

// t943Server is a daemon whose plan logins read as plans, whose auth
// failures are failed, and which records every attempt to repair a login.
func t943Server(t *testing.T, reg *claudia.Registry, failed map[string]bool, plans []PlanAuth, statusErr error) (*Server, *[]string, *int) {
	t.Helper()
	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	observeLaunchedStubSeats(s, reg)
	cleared := &[]string{}
	s.SetPlanAuthFailure(
		func(name string) bool { return failed[name] },
		func(names []string) {
			*cleared = append(*cleared, names...)
			for _, n := range names {
				failed[n] = false
			}
		},
	)
	s.authStatus = func(context.Context) ([]PlanAuth, error) { return plans, statusErr }
	repairs := new(int)
	s.authRecover = func(context.Context, claudia.Provider) error {
		*repairs++
		return nil
	}
	return s, cleared, repairs
}

// 🎯T943: a running seat refused on a revoked plan token does not retry its
// own login, so the standing sweep clears it once the broker reports the
// plan healthy again, without a click and without relaunching the seat. On
// 2026-09-30 jevons-po sat "running idle" for twelve minutes after jevons
// and claudia-po completed real turns on the same anthropic plan.
//
// 🎯T971: the sweep never repairs a login itself. Each repair rotated the
// token under every other seat, and on invalid_grant opened an interactive
// sign-in the owner had not asked for.
func TestT943RunningSeatOnAHealthyPlanIsClearedWithoutARefresh(t *testing.T) {
	const refused, healthy, other = "t943-refused", "t943-healthy", "t943-grok"
	reg, launched := t943Fleet(t, map[string]claudia.Provider{refused: "anthropic", healthy: "anthropic", other: "grok"})
	failed := map[string]bool{refused: true}
	s, cleared, repairs := t943Server(t, reg, failed, []PlanAuth{{Provider: "anthropic", State: planAuthOK}}, nil)

	got := s.RecoverRunningPlanAuthFailures(context.Background())
	if *repairs != 0 {
		t.Fatalf("the sweep repaired a login %d times; only the owner's Reauth may", *repairs)
	}
	if len(*launched) != 0 {
		t.Fatalf("a running seat was relaunched: %v", *launched)
	}
	if !slices.Equal(got, []string{refused}) {
		t.Fatalf("recovered = %v, want [%s]", got, refused)
	}
	slices.Sort(*cleared)
	if !slices.Equal(*cleared, []string{healthy, refused}) {
		t.Fatalf("cleared = %v, want both seats on the anthropic plan and not the grok one", *cleared)
	}
	if got := s.RecoverRunningPlanAuthFailures(context.Background()); len(got) != 0 {
		t.Fatalf("second sweep should find nothing once the seat is cleared: %v", got)
	}
}

// 🎯T971: while the plan's login is broken, or its health cannot be read, the
// seat stays marked for the owner and nothing tries to sign in.
func TestT971RunningSweepNeverRepairsABrokenLogin(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plans []PlanAuth
		err   error
	}{
		{"unhealthy", []PlanAuth{{Provider: "anthropic", State: "rejected"}}, nil},
		{"unreadable", nil, errors.New("broker down")},
		{"absent", []PlanAuth{{Provider: "cursor", State: planAuthOK}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const refused = "t971-refused"
			reg, launched := t943Fleet(t, map[string]claudia.Provider{refused: "anthropic"})
			failed := map[string]bool{refused: true}
			s, cleared, repairs := t943Server(t, reg, failed, tc.plans, tc.err)
			for range 3 {
				if got := s.RecoverRunningPlanAuthFailures(context.Background()); len(got) != 0 {
					t.Fatalf("recovered %v on a plan that is not healthy", got)
				}
			}
			if *repairs != 0 || len(*cleared) != 0 || len(*launched) != 0 {
				t.Fatalf("repairs=%d cleared=%v launched=%v, want none", *repairs, *cleared, *launched)
			}
			if !failed[refused] {
				t.Fatal("the seat's refusal was forgotten while its plan is broken")
			}
		})
	}
}

// A stopped seat is RevivePlanAuthWhereHealthy's job, not this sweep's: this
// sweep only ever touches seats it finds still alive.
func TestT943RunningSweepSkipsStoppedSeats(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	if err := reg.Register(claudia.AgentDef{Name: "t943-stopped", WorkDir: t.TempDir(), Provider: "anthropic", SessionID: "sid"}); err != nil {
		t.Fatal(err)
	}
	// Never launched: reg.Get returns nil, so the seat is not alive.
	s, cleared, repairs := t943Server(t, reg, map[string]bool{"t943-stopped": true}, []PlanAuth{{Provider: "anthropic", State: planAuthOK}}, nil)
	if got := s.RecoverRunningPlanAuthFailures(context.Background()); len(got) != 0 || *repairs != 0 || len(*cleared) != 0 {
		t.Fatalf("stopped seat should not be touched: got=%v repairs=%d cleared=%v", got, *repairs, *cleared)
	}
}
