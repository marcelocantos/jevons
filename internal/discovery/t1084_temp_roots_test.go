// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// globExclusiveGrokSessionRoots is the pre-🎯T1084 answer: list the temp dir
// on every call. The cached reading is held to it.
func globExclusiveGrokSessionRoots(tmp string) []string {
	matches, err := filepath.Glob(filepath.Join(tmp, exclusiveGrokHomePrefix+"*"))
	if err != nil {
		return nil
	}
	var roots []string
	for _, home := range matches {
		sess := filepath.Join(home, "sessions")
		if fi, err := os.Stat(sess); err == nil && fi.IsDir() {
			roots = append(roots, sess)
		}
	}
	sort.Strings(roots)
	return roots
}

// 🎯T1084: the seat-observation tick asked for the exclusive Grok homes
// several times per seat, and each ask listed $TMPDIR — 35,100 entries,
// ~0.36 s of system CPU apiece on the development host. A temp dir whose
// entries have not changed is not listed again; the answer is the same.
func TestT1084ExclusiveGrokRootsListTempDirOnlyWhenItChanges(t *testing.T) {
	tmp := t.TempDir()
	for i := 0; i < 50; i++ {
		if err := os.WriteFile(filepath.Join(tmp, "go-build-junk-"+string(rune('a'+i%26))+string(rune('a'+i/26))), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mkdir := func(p string) {
		t.Helper()
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	home1 := filepath.Join(tmp, exclusiveGrokHomePrefix+"111")
	home2 := filepath.Join(tmp, exclusiveGrokHomePrefix+"222")
	mkdir(filepath.Join(home1, "sessions"))
	mkdir(home2) // no sessions dir yet

	check := func(label string, wantGlobs int64, calls int) {
		t.Helper()
		before := tempGlobs.Load()
		for i := 0; i < calls; i++ {
			got, want := exclusiveGrokSessionRoots(tmp), globExclusiveGrokSessionRoots(tmp)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: roots %v, a fresh listing says %v", label, got, want)
			}
		}
		if n := tempGlobs.Load() - before; n != wantGlobs {
			t.Fatalf("%s: %d calls listed the temp dir %d times, want %d", label, calls, n, wantGlobs)
		}
	}
	check("first read", 1, 1)
	check("quiet temp dir", 0, 20)

	// A home gaining its sessions dir does not touch the temp dir itself;
	// the answer still sees it, without a listing.
	mkdir(filepath.Join(home2, "sessions"))
	check("sessions dir appears inside a known home", 0, 3)

	// A new home, and a removed one, change the temp dir: one listing each.
	mkdir(filepath.Join(tmp, exclusiveGrokHomePrefix+"333", "sessions"))
	check("new home", 1, 3)
	if err := os.RemoveAll(home1); err != nil {
		t.Fatal(err)
	}
	check("removed home", 1, 3)
	if err := os.WriteFile(filepath.Join(tmp, "unrelated"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	check("unrelated churn relists once", 1, 3)
}
