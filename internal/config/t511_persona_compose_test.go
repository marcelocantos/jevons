// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/roles"
)

func TestT511PersonaComposesOverseerRoleFile(t *testing.T) {
	out, err := Default().Persona()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, roles.DoctrineMarker) {
		t.Fatal("Persona() missing role doctrine marker")
	}
	builtin, err := roles.Builtin(roles.Overseer)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, builtin.Body) {
		t.Fatal("Persona() must append overseer role body verbatim")
	}
	for _, want := range []string{
		"Never parent product workers",
		"🎯T129",
		"🎯T1053",
		"🎯T1140",
		"🎯T31",
		"🎯T427",
		"git merge-base --is-ancestor",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("composed persona missing %q", want)
		}
	}
	shell := out[:strings.Index(out, roles.DoctrineMarker)]
	for _, banned := range []string{
		"### Overseer never parents product workers",
		"## Oracle-first as system property",
	} {
		if strings.Contains(shell, banned) {
			t.Errorf("persona shell still duplicates role section %q", banned)
		}
	}
}
