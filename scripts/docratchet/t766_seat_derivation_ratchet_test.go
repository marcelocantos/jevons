// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 🎯T766.2 clause 3: every control reads the seat-state authority, and the
// derivations the census catalogued are removed from production code rather
// than merely bypassed.
//
// A boolean "no control re-derives" test cannot be written today, because
// several controls still do. What can be written — and is the only thing
// 🎯T766 is measured on — is that the number can only fall. The parent's
// acceptance says so in as many words: an implementation that adds a
// mechanism and leaves the old ones in place has failed the target however
// well it works.
//
// So this pins the population. Add a new direct derivation and the test
// fails; convert one and the test tells you to lower the pin. The pins move
// in one direction only, by a deliberate edit, which is what makes them a
// ratchet rather than a threshold (AGENTS.md, Verification honesty).

// seatInFlightPin is the number of production sites that ask a claudia
// process handle whether a turn is in flight, instead of asking the
// authority. Census derivation 5 — the signal that authorises a SIGKILL of
// a process group.
//
// The one permitted site is the funnel itself, Server.seatInFlight in
// internal/mcpserver/mcpserver.go, which is excluded below: it asks the
// party that knows, records the answer, and is what every other site is
// being converted to call.
const seatInFlightPin = 6

// Liveness — census derivation 4 — is deliberately NOT ratcheted here.
// `.Alive()` is spelled the same by types that have nothing to do with a
// seat (internal/upgrade/handles.go among them), so the needle counts 56
// sites of which an unknown number are not controls at all. A ratchet that
// fires on unrelated code gets switched off, and a switched-off ratchet
// measures nothing. It becomes ratchetable when liveness reaches controls
// through the authority and the direct call has a distinguishable spelling.

func TestT766SeatStateDerivationsOnlyFall(t *testing.T) {
	for _, tc := range []struct {
		what   string
		needle string
		pin    int
	}{
		{"in-flight", ".PromptInFlight()", seatInFlightPin},
	} {
		t.Run(tc.what, func(t *testing.T) {
			sites := productionSites(t, tc.needle)
			if len(sites) > tc.pin {
				t.Fatalf("%d production sites derive %s directly, pin is %d — a new one was added.\n"+
					"Ask the seat-state authority (internal/seatstate) instead; see docs/fleet-census.md.\nsites:\n%s",
					len(sites), tc.what, tc.pin, strings.Join(sites, "\n"))
			}
			if len(sites) < tc.pin {
				t.Fatalf("%d production sites derive %s directly but the pin is still %d.\n"+
					"Lower it to %d in this file: a ratchet that is not tightened stops measuring.\nsites:\n%s",
					len(sites), tc.what, tc.pin, len(sites), strings.Join(sites, "\n"))
			}
		})
	}
}

// productionSites lists "path:line" for every occurrence of needle in
// tracked, non-test Go under internal/ and cmd/, excluding the authority
// package itself and the one funnel that is allowed to ask.
func productionSites(t *testing.T, needle string) []string {
	t.Helper()
	root := repoRoot(t)
	var out []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			// The authority and its feeds are where these facts are
			// supposed to be read; counting them would ratchet against
			// the fix.
			if strings.HasPrefix(rel, filepath.Join("internal", "seatstate")) {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			inFunnel := false
			for i, line := range strings.Split(string(body), "\n") {
				switch {
				case strings.HasPrefix(line, "func (s *Server) seatInFlight("):
					inFunnel = true
				case inFunnel && line == "}":
					inFunnel = false
				}
				if inFunnel || !strings.Contains(line, needle) {
					continue
				}
				if idx := strings.Index(line, "//"); idx >= 0 && idx < strings.Index(line, needle) {
					continue // a comment about the call, not the call
				}
				out = append(out, rel+":"+itoa(i+1))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
