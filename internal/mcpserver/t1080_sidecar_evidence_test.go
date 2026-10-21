// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/spool"
)

// 🎯T1080 — a sidecar seat's lifecycle oracles decide from evidence the
// sidecar or broker owns (the dated spool), never from a vendor transcript
// since claudia v0.58 a named-plan seat does not write. claudeJSONLSeat
// (empty provider) is the one row shape that still takes the Claude JSONL
// convention; every named plan, in either spelling, is this target's
// subject. t1080Pairs pins both spellings so the oracles cannot drift back
// to reading one vocabulary.
var t1080Pairs = [][2]claudia.Provider{
	{"grok", "xai-oauth"},
}

// plantT1080SpoolLine writes one dated spool line naming seat, the shape
// internal/spool decodes (ts, seat, type). dir is a day file's directory;
// the oracle under test asks spool.Dir(), so HOME/JEVONS_SPOOL_DIR must
// already point here.
func plantT1080SpoolLine(t *testing.T, dir, seat string, ts time.Time, typ string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("events-%s.log", ts.UTC().Format("2006-01-02"))
	line := fmt.Sprintf(`{"ts":%q,"seat":%q,"type":%q,"text":"x"}`+"\n",
		ts.UTC().Format(time.RFC3339Nano), seat, typ)
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

// 🎯T1080 / 🎯T797 twin: a sidecar seat's required-MCP check never scans a
// vendor transcript for either spelling of its provider — observed stays
// false, because the sidecar's own launch report, not this reader, is that
// seat's answer (🎯T934). The Claude JSONL fixture it used to be driven
// through (claudeJSONLSeat) is retired for this oracle: that row shape is
// not a sidecar seat at all, so it proved nothing about sidecar coverage.
func TestT1080SeatMCPNeverScansSidecarSeat(t *testing.T) {
	for _, pair := range t1080Pairs {
		for _, provider := range pair {
			t.Run(string(provider), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				work := t.TempDir()
				const sid = "019f4f4b-945a-7a23-ba4c-51a0c26e1080"
				// Plant a Claude-shaped JSONL at the path a Claude seat
				// would use for this session id. A sidecar seat storing
				// this provider must not read it as its own records.
				path := claudia.SessionJSONLPath(sid, work)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				body := `{"type":"user","timestamp":"2026-10-19T00:00:00Z"}` + "\n" +
					`{"type":"assistant","attachment":{"type":"deferred_tools_delta","addedNames":["mcp__bullseye__list"]}}` + "\n"
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
				s := New(t.TempDir(), nil, nil)
				def := claudia.AgentDef{Name: "jv-t1080-seat", WorkDir: work, SessionID: sid, Provider: provider}
				diag := s.diagnoseSeatMCPOnRequest(def, time.Now())
				if diag.Observed {
					t.Fatalf("provider %q: seat mcp diagnosis observed a vendor JSONL a sidecar seat never writes: %+v", provider, diag)
				}
			})
		}
	}
}

// 🎯T1080 / 🎯T412+T444 twin: ReadSessionEvidence answers unknown for a
// sidecar seat, in either spelling of its provider, whether or not a Claude
// JSONL happens to sit on disk at that session id's path — that file is not
// this seat's conversation (🎯T1079). A false "absent" there is exactly the
// death this target exists to stop; a false "present" is exactly the
// never_briefed miss.
func TestT1080SessionEvidenceNeverReadsVendorJSONLForSidecarSeat(t *testing.T) {
	for _, pair := range t1080Pairs {
		for _, provider := range pair {
			t.Run(string(provider), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				work := t.TempDir()
				const sid = "019f4f4b-945a-7a23-ba4c-51a0c26e1081"
				path := claudia.SessionJSONLPath(sid, work)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"type":"user"}`+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if got := ReadSessionEvidence(provider, sid, work); got != SessionEvidenceUnknown {
					t.Fatalf("provider %q over a vendor JSONL on disk: %s want unknown", provider, got)
				}
			})
		}
	}
}

// 🎯T1080 — the production observation pass (observeSeatTranscriptWith) is
// the one place that upgrades a sidecar seat's evidence from unknown to
// present, and it does so from the daemon's own spool, not a vendor path.
// Before any spool record lands, the seat reads never_briefed (honest: the
// daemon has seen nothing yet); once the sidecar has written a record
// naming the seat, the same pass reads it running. Both spellings of the
// provider take the same answer.
func TestT1080SidecarSeatEvidenceComesFromOwnSpoolRecord(t *testing.T) {
	for _, pair := range t1080Pairs {
		for _, provider := range pair {
			t.Run(string(provider), func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				spoolDir := t.TempDir()
				t.Setenv(spool.DirEnv, spoolDir)
				work := t.TempDir()
				const name = "jv-t1080-sidecar-seat"
				const sid = "019f4f4b-945a-7a23-ba4c-51a0c26e1082"
				reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
				if err != nil {
					t.Fatal(err)
				}
				def := claudia.AgentDef{
					Name: name, WorkDir: work, SessionID: sid, Provider: provider,
					Purpose: claudia.PurposeWork,
				}
				if err := reg.Register(def); err != nil {
					t.Fatal(err)
				}
				s := New(t.TempDir(), nil, nil)
				s.SetRegistry(reg)
				s.SetSeatAliveFn(func(n string) bool { return n == name })
				s.observeRegistryLiveness()

				roots := s.transcriptRoots()
				s.observeSeatTranscriptWith(def, roots)
				before := s.seatState(name).Status
				if before != AgentStatusPhaseUnknown {
					t.Fatalf("provider %q before any spool record: status %q want %q (no evidence yet must not read running, and must not claim briefed)",
						provider, before, AgentStatusPhaseUnknown)
				}

				plantT1080SpoolLine(t, spoolDir, name, time.Now().UTC(), "accepted")
				s.observeSeatTranscriptWith(def, roots)
				after := s.seatState(name).Status
				if after != AgentStatusRunning {
					t.Fatalf("provider %q after its own spool record: status %q want %q",
						provider, after, AgentStatusRunning)
				}
			})
		}
	}
}
