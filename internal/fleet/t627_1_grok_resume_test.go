// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

const t6271GrokSID = "01a030e9-existing-grok"

// grokLoadErr is the product wording grok_acp.openSession emits when
// RequireResume load fails. It contains claudia.ErrCursorResumeDenied's
// text, which is how LaunchRecovering used to remint Grok.
func grokLoadErr(sid string) error {
	return fmt.Errorf("acp session/load %s: %w — existing conversation; refusing to mint a replacement session",
		sid, errors.New("Path not found"))
}

func bounceLoadedGrok(t *testing.T, materialized bool) *claudia.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agents.json")
	work := t.TempDir()
	r1, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := r1.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: work, SessionID: t6271GrokSID,
		Provider: claudia.ProviderGrok, Materialized: materialized, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	r2, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	r2.SetDirect(true)
	return r2
}

func TestT627_1GrokLoadFailureDoesNotRemint(t *testing.T) {
	for _, materialized := range []bool{false, true} {
		t.Run(fmt.Sprintf("materialized=%v", materialized), func(t *testing.T) {
			reg := bounceLoadedGrok(t, materialized)
			starts := 0
			reg.SetLaunchers(&claudia.RegistryLaunchers{
				Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
					starts++
					if !cfg.RequireResume {
						reminted := cfg
						reminted.SessionID = t6271GrokSID + "-reminted"
						return claudia.StartStub(context.Background(), reminted, nil)
					}
					return nil, grokLoadErr(cfg.SessionID)
				},
			})
			_, err := LaunchRecovering(reg, "jevons")
			if err == nil {
				t.Fatal("Grok load failure reminted")
			}
			if !strings.Contains(err.Error(), "Path not found") {
				t.Fatalf("err=%v, want the load failure, not a remint", err)
			}
			if got := reg.Def("jevons").SessionID; got != t6271GrokSID {
				t.Fatalf("session drifted to %q", got)
			}
			if starts != 1 {
				t.Fatalf("starts=%d, remint retried Launch", starts)
			}
		})
	}
}

func TestT627_1GrokMissingStorageDoesNotLookLost(t *testing.T) {
	def := &claudia.AgentDef{
		Name: "bounce-aside", WorkDir: t.TempDir(), SessionID: t6271GrokSID,
		Provider: claudia.ProviderGrok, Materialized: true,
	}
	if SessionLost(def) {
		t.Fatal("missing Grok home classified as Claude JSONL loss — would rotate")
	}
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(*def); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := RehydrateLostSessionIn(reg, def.Name); err != nil || ok {
		t.Fatalf("Grok row rotated without a provider load: ok=%v err=%v", ok, err)
	}
	if got := reg.Def(def.Name).SessionID; got != t6271GrokSID {
		t.Fatalf("session drifted to %q", got)
	}
}

func TestT627_1SameProcessGrokMintIsNotResume(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "fresh-grok", WorkDir: t.TempDir(), SessionID: "requested-id",
		Provider: claudia.ProviderGrok,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var requireResume *bool
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			v := cfg.RequireResume
			requireResume = &v
			return claudia.StartStub(context.Background(), cfg, nil)
		},
	})
	if _, err := LaunchRecovering(reg, "fresh-grok"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("fresh-grok") })
	if requireResume == nil || *requireResume {
		t.Fatal("same-process first mint required resume — mint and resume are no longer distinct")
	}
}

func TestT627_1CursorResumeDeniedStillRemints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	r1, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := r1.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: t.TempDir(), SessionID: "cursor-present",
		Provider: claudia.ProviderCursor, Materialized: true,
	}); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			if cfg.RequireResume {
				return nil, fmt.Errorf("acp session/load %s: Invalid params (%w)", cfg.SessionID, claudia.ErrCursorResumeDenied)
			}
			return claudia.StartStub(context.Background(), cfg, nil)
		},
	})
	agent, err := LaunchRecovering(reg, "jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Stop("jevons-po") })
	if agent == nil {
		t.Fatal("cursor remint returned no process")
	}
	if got := reg.Def("jevons-po").SessionID; got == "cursor-present" {
		t.Fatal("cursor resume denial did not rotate — T541.1 stacking path is back")
	}
	if starts != 2 {
		t.Fatalf("starts=%d want fail then remint", starts)
	}
}

func TestRemintAfterResumeErrorGrokStringMatchIsNotCursor(t *testing.T) {
	err := grokLoadErr(t6271GrokSID)
	if !claudia.IsCursorResumeDenied(err) {
		t.Fatal("fixture no longer shares Cursor's refuse-to-mint phrase — the T627.1 trap is gone")
	}
	grok := &claudia.AgentDef{Name: "jevons", Provider: claudia.ProviderGrok, SessionID: t6271GrokSID}
	if remintAfterResumeError(grok, err) {
		t.Fatal("Grok load failure authorized a remint")
	}
	cursor := &claudia.AgentDef{Name: "jevons-po", Provider: claudia.ProviderCursor, SessionID: "sid"}
	if !remintAfterResumeError(cursor, fmt.Errorf("acp session/load sid: Invalid params (%w)", claudia.ErrCursorResumeDenied)) {
		t.Fatal("Cursor resume denial no longer remints")
	}
	if remintAfterResumeError(cursor, errors.New("connection reset")) {
		t.Fatal("generic Cursor error reminted")
	}
	if remintAfterResumeError(nil, err) || remintAfterResumeError(grok, nil) {
		t.Fatal("nil def or nil error reminted")
	}
}
