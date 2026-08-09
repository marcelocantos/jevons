// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"strings"
	"testing"
)

// TestClassifyNamesTheIndexGitIsCommitting pins the mapping the whole guard
// rests on. The values are not invented: they are what git 2.55 actually put
// in GIT_INDEX_FILE for a pre-commit hook under each commit form.
func TestClassifyNamesTheIndexGitIsCommitting(t *testing.T) {
	for _, c := range []struct {
		name      string
		indexFile string
		want      IndexKind
		sweeps    bool
	}{
		{"bare commit leaves it unset", "", SharedIndex, true},
		{"bare commit, relative as git passes it", ".git/index", SharedIndex, true},
		{"bare commit, absolute", "/repo/.git/index", SharedIndex, true},
		{"commit -a commits through the shared lock", "/repo/.git/index.lock", SharedLock, true},
		{"commit -i commits through the shared lock", ".git/index.lock", SharedLock, true},
		{"commit --only builds a temporary index", "/repo/.git/next-index-56626.lock", ScopedIndex, false},
		{"commit --only, unlocked spelling", "/repo/.git/next-index-4321", ScopedIndex, false},
		{"worker-owned index", "/repo/.git/index-jv-t377", PrivateIndex, false},
		{"worker-owned index outside the git dir", "/tmp/w/idx", PrivateIndex, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.indexFile)
			if got != c.want {
				t.Errorf("Classify(%q) = %v, want %v", c.indexFile, got, c.want)
			}
			if got.Sweeps() != c.sweeps {
				t.Errorf("Classify(%q).Sweeps() = %v, want %v", c.indexFile, got.Sweeps(), c.sweeps)
			}
		})
	}
}

// TestDecideRefusesOnlyWhatCanMisattribute states the rule in both
// directions. Getting either direction wrong is a real failure: allowing a
// shared-index commit is the 29e69e8 bug, and refusing a scoped one makes the
// guard something workers route around.
func TestDecideRefusesOnlyWhatCanMisattribute(t *testing.T) {
	staged := []string{"web/scripts/fleet_cycle.js", "web/index.html"}
	for _, c := range []struct {
		name    string
		req     Request
		refused bool
	}{
		{"bare commit with foreign hunks staged", Request{IndexFile: "", Staged: staged}, true},
		{"commit -a", Request{IndexFile: ".git/index.lock", Staged: staged}, true},
		{"commit --only", Request{IndexFile: "/r/.git/next-index-9.lock", Staged: staged}, false},
		{"private index", Request{IndexFile: "/r/.git/index-w", Staged: staged}, false},
		{"nothing staged cannot misattribute", Request{IndexFile: "", Staged: nil}, false},
		{"explicitly disabled", Request{IndexFile: "", Staged: staged, Disabled: true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := Decide(&c.req)
			if v.Refused != c.refused {
				t.Fatalf("Decide(%+v).Refused = %v, want %v", c.req, v.Refused, c.refused)
			}
			if !c.refused && v.Message != "" {
				t.Errorf("allowed commit still produced a message: %q", v.Message)
			}
		})
	}
}

// TestRefusalNamesWhatWouldHaveBeenCommitted. A refusal that says only "no"
// leaves the worker guessing which of several concurrent workers is in the
// way, and the recovery command is the whole point of stopping them.
func TestRefusalNamesWhatWouldHaveBeenCommitted(t *testing.T) {
	v := Decide(&Request{Staged: []string{"web/scripts/fleet_cycle.js", "internal/server/chat_wire.go"}})
	if !v.Refused {
		t.Fatal("bare commit of staged paths was allowed")
	}
	for _, want := range []string{
		"web/scripts/fleet_cycle.js",
		"internal/server/chat_wire.go",
		"git commit --only",
		"git show --stat HEAD",
		DisableEnv,
	} {
		if !strings.Contains(v.Message, want) {
			t.Errorf("refusal message omits %q:\n%s", want, v.Message)
		}
	}
}

// TestRefusalCapsTheListing keeps a refusal a diagnostic rather than a diff.
func TestRefusalCapsTheListing(t *testing.T) {
	staged := make([]string, MaxNamed+5)
	for i := range staged {
		staged[i] = "path/f" + string(rune('a'+i%26))
	}
	msg := Decide(&Request{Staged: staged}).Message
	// Count only the listing block; the recovery commands are indented the
	// same way and are not paths.
	if listed := strings.Count(msg, "\n  path/"); listed > MaxNamed {
		t.Errorf("listed %d paths, want at most %d", listed, MaxNamed)
	}
	if !strings.Contains(msg, "and 5 more") {
		t.Errorf("truncated listing does not say how many were elided:\n%s", msg)
	}
}

// TestGuardCannotBeDisabledByAccident. An empty or unrecognised value must
// leave the guard on; a guard that a stray export silently removes is not a
// guard.
func TestGuardCannotBeDisabledByAccident(t *testing.T) {
	for _, v := range []string{"", " ", "1", "on", "true", "yes", "please"} {
		if OffValue(v) {
			t.Errorf("OffValue(%q) = true, want false", v)
		}
	}
	for _, v := range []string{"off", "OFF", " off ", "0", "false", "no", "disabled"} {
		if !OffValue(v) {
			t.Errorf("OffValue(%q) = false, want true", v)
		}
	}
}
