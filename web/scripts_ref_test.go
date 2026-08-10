// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package web

import (
	"reflect"
	"testing"
)

// IndexScriptRefs feeds the 🎯T374 serve-time guard, which turns any ref it
// cannot find on disk into an owner-visible "cockpit assets incomplete"
// banner. Both directions of that scan therefore matter to the owner: a ref
// it misses is a silent 404 cascade, and a ref it invents is a false alarm on
// a healthy cockpit — and a banner that cries wolf is how the owner learns to
// ignore the real one.
func TestIndexScriptRefsFindsLocalModulesInOrder(t *testing.T) {
	got := IndexScriptRefs([]byte(
		`<script src="https://cdn.example/marked.js"></script>` +
			`<script src="scripts/boot_sentinel.js"></script>` +
			`<script src='scripts/jlog.js'></script>` +
			`<script src="scripts/../../etc/passwd"></script>` +
			`<script>inline()</script>`))
	want := []string{"scripts/boot_sentinel.js", "scripts/jlog.js"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IndexScriptRefs = %q, want %q", got, want)
	}
}

// TestIndexScriptRefsIgnoresCommentedTags pins the false-alarm direction.
// index.html's own wiring comments quote the tag shape they are explaining,
// and a module is sometimes disabled by commenting its tag out; neither is a
// load, and reporting either as a missing asset would banner a healthy tree.
func TestIndexScriptRefsIgnoresCommentedTags(t *testing.T) {
	got := IndexScriptRefs([]byte(
		`<!-- the gate sees <script src="scripts/never_existed.js"> fail to load -->` +
			`<script src="scripts/real.js"></script>` +
			`<!--<script src="scripts/disabled.js"></script>-->`))
	want := []string{"scripts/real.js"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IndexScriptRefs = %q, want %q (commented tags are not loads)", got, want)
	}
}

// An unterminated comment swallows the rest of the document in a browser too,
// so refs after it are genuinely not loaded and must not be reported.
func TestIndexScriptRefsUnterminatedCommentSwallowsRest(t *testing.T) {
	got := IndexScriptRefs([]byte(
		`<script src="scripts/before.js"></script>` +
			`<!-- oops <script src="scripts/after.js"></script>`))
	want := []string{"scripts/before.js"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IndexScriptRefs = %q, want %q", got, want)
	}
}

// The real document is the case that broke: 🎯T374's wiring comment quotes a
// script tag to explain what the gate watches for. A guard that scanned
// comments would name a nonexistent module on the live cockpit.
func TestIndexScriptRefsOnRealIndexAreAllRealFiles(t *testing.T) {
	raw, err := FS.ReadFile("index.html")
	if err != nil {
		t.Fatalf("embedded index.html missing: %v", err)
	}
	refs := IndexScriptRefs(raw)
	if len(refs) == 0 {
		t.Fatal("no local script refs parsed from index.html — scan broke")
	}
	for _, ref := range refs {
		if _, err := FS.Open(ref); err != nil {
			t.Errorf("IndexScriptRefs reported %q, which is not a real module: %v", ref, err)
		}
	}
}
