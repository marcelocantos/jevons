// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package writconf_test

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/writconf"
)

func TestClassifyAccess_AllowAndDenyNet(t *testing.T) {
	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{
		NetHosts: []string{"example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev := writconf.ClassifyAccess(m, "w1", "example.com", "", "read"); ev != nil {
		t.Fatalf("allowed host: %+v", ev)
	}
	ev := writconf.ClassifyAccess(m, "w1", "blocked.invalid", "", "read")
	if ev == nil || ev.Kind != writconf.KindDenyNet {
		t.Fatalf("want deny_net, got %+v", ev)
	}
}

func TestClassifyAccess_SecretAndHighRisk(t *testing.T) {
	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{})
	if err != nil {
		t.Fatal(err)
	}
	ev := writconf.ClassifyAccess(m, "w1", "", "/Users/x/.ssh/id_ed25519", "read")
	if ev == nil || ev.Kind != writconf.KindSecretPath {
		t.Fatalf("secret: %+v", ev)
	}
	ev = writconf.ClassifyAccess(m, "w1", "evil.ru", "", "read")
	if ev == nil || ev.Kind != writconf.KindHighRiskEgress {
		t.Fatalf("high risk: %+v", ev)
	}
}

func TestClassifyAccess_DenyFS(t *testing.T) {
	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{
		WorkDir:   "/tmp/workdir-t335",
		IncludeFS: true,
		NetHosts:  []string{"example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := writconf.ClassifyAccess(m, "w1", "", "/etc/passwd", "read")
	if ev == nil || ev.Kind != writconf.KindDenyFS {
		t.Fatalf("want deny_fs, got %+v", ev)
	}
	if !writconf.PathDeclared(m, "/tmp/workdir-t335/a.go", "edit") {
		t.Fatal("workdir should be declared")
	}
}

func TestPureExecutor_AllowAndBlock(t *testing.T) {
	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{
		NetHosts: []string{"example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ex := writconf.PureExecutor{}
	ok, err := ex.Exec(t.Context(), &writconf.ExecArgs{
		Manifest: m,
		Argv:     []string{"curl", "https://example.com/"},
		Agent:    "t",
	})
	if err != nil || ok.Denied {
		t.Fatalf("allow: res=%+v err=%v", ok, err)
	}
	bad, err := ex.Exec(t.Context(), &writconf.ExecArgs{
		Manifest: m,
		Argv:     []string{"curl", "https://blocked.invalid/"},
		Agent:    "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bad.Denied || bad.Event == nil || bad.Event.Kind != writconf.KindDenyNet {
		t.Fatalf("block: %+v", bad)
	}
}

func TestParseDenialOutput(t *testing.T) {
	body := `{"error":"missing_capability","host":"evil.example","manifest_field":"net[].host","message":"host not declared"}`
	ev := writconf.ParseDenialOutput(body)
	if ev == nil || ev.Host != "evil.example" {
		t.Fatalf("got %+v", ev)
	}
}

func TestBypassAndDrift(t *testing.T) {
	b := writconf.ClassifyBypass("w1", "retried curl without writ")
	if b.Kind != writconf.KindBypass || !b.OutsideSandbox {
		t.Fatalf("%+v", b)
	}
	d := writconf.ClassifyDrift("w1", "/secret", "", "undeclared open")
	if d.Kind != writconf.KindDrift {
		t.Fatalf("%+v", d)
	}
}
