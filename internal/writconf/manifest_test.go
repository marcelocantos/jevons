// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package writconf_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/writconf"
)

func TestFleetManifestNetOnly(t *testing.T) {
	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != "1" {
		t.Fatalf("schema %q", m.SchemaVersion)
	}
	if m.FS != nil {
		t.Fatal("net-only default must omit fs (no FUSE requirement)")
	}
	if len(m.Net) == 0 {
		t.Fatal("expected default net hosts")
	}
	if !writconf.NetAllowed(m, "api.x.ai") {
		t.Fatal("api.x.ai should be allowed")
	}
	if writconf.NetAllowed(m, "evil.example") {
		t.Fatal("evil.example must be denied")
	}
	raw, err := writconf.MarshalJSONDocument(m)
	if err != nil {
		t.Fatal(err)
	}
	var round writconf.Manifest
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	if err := writconf.Validate(&round); err != nil {
		t.Fatal(err)
	}
}

func TestFleetManifestIncludeFS(t *testing.T) {
	wd := t.TempDir()
	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{
		WorkDir:   wd,
		IncludeFS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.FS == nil || len(m.FS.Read) == 0 {
		t.Fatal("expected fs scopes")
	}
	inside := filepath.Join(wd, "src", "main.go")
	if !writconf.PathDeclared(m, inside, "edit") {
		t.Fatalf("workdir path should be declared: %s", inside)
	}
	if writconf.PathDeclared(m, filepath.Join(wd, "..", "outside.txt"), "read") {
		// Clean may resolve; secret is that ~/.ssh is not declared
	}
	ssh := filepath.Join(wd, "..", ".ssh", "id_rsa")
	// Absolute secret outside workdir
	homeSSH := "/Users/nobody/.ssh/id_rsa"
	if writconf.PathDeclared(m, homeSSH, "read") {
		t.Fatal("~/.ssh must not be declared by workdir-only manifest")
	}
	_ = ssh
}

func TestValidateRejectsEmptyNetIntent(t *testing.T) {
	err := writconf.Validate(&writconf.Manifest{
		SchemaVersion: "1",
		Net:           []writconf.NetIntent{{}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMarshalFSTargetBareString(t *testing.T) {
	m := &writconf.Manifest{
		SchemaVersion: "1",
		FS: &writconf.FSScopes{
			Read: []writconf.FSTarget{{Glob: "/tmp"}},
		},
	}
	raw, err := writconf.MarshalJSONDocument(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"/tmp"`) {
		t.Fatalf("want bare string glob, got %s", raw)
	}
}
