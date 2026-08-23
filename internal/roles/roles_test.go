// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package roles_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/roles"
)

func TestT5362BuiltinAuditorResolves(t *testing.T) {
	r, err := (roles.Catalog{}).Resolve("auditor")
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != roles.SourceBuiltin || r.Name != "auditor" {
		t.Fatalf("got %+v", r)
	}
	for _, want := range []string{
		"cannot write or patch product code",
		"silent-decision ledger",
		"do not file bullseye",
		"🎯T536.2",
		"ReadSilentLedger",
	} {
		if !strings.Contains(r.Body, want) {
			t.Errorf("auditor body missing %q", want)
		}
	}
}

func TestT5362OwnerOverrideWins(t *testing.T) {
	dir := t.TempDir()
	body := "---\nrole: auditor\npurpose: work\nsummary: override\n---\n\n# Override auditor\n"
	if err := os.WriteFile(filepath.Join(dir, "auditor.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := (roles.Catalog{OwnerDir: dir}).Resolve("auditor")
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != roles.SourceOwner || !strings.Contains(r.Body, "Override auditor") {
		t.Fatalf("override lost: %+v", r)
	}
}

func TestT5362UnknownRoleFailsLoud(t *testing.T) {
	_, err := (roles.Catalog{}).Resolve("not-a-role")
	if err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Fatalf("want unknown role error, got %v", err)
	}
}

func TestT5362MalformedFrontmatterHardError(t *testing.T) {
	dir := t.TempDir()
	bad := "---\nrole: [oops\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "custom.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (roles.Catalog{OwnerDir: dir}).Resolve("custom")
	if err == nil {
		t.Fatal("expected hard error on malformed frontmatter")
	}
	if !strings.Contains(err.Error(), "frontmatter") && !strings.Contains(err.Error(), "yaml") {
		t.Fatalf("error should name frontmatter/yaml, got %v", err)
	}
}

func TestT5362BuiltinCannotBeDeleted(t *testing.T) {
	err := (roles.Catalog{OwnerDir: t.TempDir()}).Delete("auditor", 0, false)
	if err == nil || !strings.Contains(err.Error(), "cannot be deleted") {
		t.Fatalf("want builtin delete refusal, got %v", err)
	}
}

func TestT5362InUseDeleteRefusedWithoutForce(t *testing.T) {
	dir := t.TempDir()
	body := "---\nrole: scout\npurpose: work\n---\n\n# Scout\n"
	if err := os.WriteFile(filepath.Join(dir, "scout.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := roles.Catalog{OwnerDir: dir}
	if err := c.Delete("scout", 2, false); err == nil || !strings.Contains(err.Error(), "live instance") {
		t.Fatalf("want in-use refusal, got %v", err)
	}
	if err := c.Delete("scout", 2, true); err != nil {
		t.Fatal(err)
	}
}

func TestT5362AssembleOrder(t *testing.T) {
	out := roles.Assemble("UNIVERSAL", "ROLEBODY", "MISSION")
	if !strings.Contains(out, "UNIVERSAL") || !strings.Contains(out, "ROLEBODY") || !strings.Contains(out, "MISSION") {
		t.Fatalf("missing parts: %s", out)
	}
	ui := strings.Index(out, "UNIVERSAL")
	ri := strings.Index(out, "ROLEBODY")
	mi := strings.Index(out, "MISSION")
	if !(ui < ri && ri < mi) {
		t.Fatalf("want universal < role < mission, got %d %d %d", ui, ri, mi)
	}
	if !strings.Contains(out, "[Jevons role doctrine]") {
		t.Fatal("role section marker missing")
	}
}

func TestT5362DefaultForPurpose(t *testing.T) {
	if got := roles.DefaultForPurpose("work", "jv-t1-x"); got != "worker" {
		t.Fatalf("work default=%q", got)
	}
	if got := roles.DefaultForPurpose("aside", "chat"); got != "aside" {
		t.Fatalf("aside=%q", got)
	}
	if got := roles.DefaultForPurpose("work", "jevons-po"); got != "product-owner" {
		t.Fatalf("po=%q", got)
	}
	if got := roles.DefaultForPurpose("", "x"); got != "worker" {
		t.Fatalf("empty purpose=%q", got)
	}
}

func TestT5362ListIncludesAuditorBuiltin(t *testing.T) {
	list, err := (roles.Catalog{}).List()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range list {
		if r.Name == "auditor" && r.Source == roles.SourceBuiltin {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("auditor builtin missing from list: %+v", list)
	}
}
