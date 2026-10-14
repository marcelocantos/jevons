// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpup

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

const (
	staleNestedBullseye = "http://127.0.0.1:52322/upstream/bullseye"
	directBullseye      = "http://127.0.0.1:18743/mcp"
	remoteAtlassian     = "https://mcp.atlassian.com/v1/mcp/authv2"
	dailyPrefix         = "http://127.0.0.1:13705/upstream"
)

// 🎯T1039: a leftover nested proxy on another isolate's port is not a
// real upstream. The owner's direct map wins; the registry is not
// poisoned with the leftover.
func TestT1039ResolveRestoresDirectFromStaleNested(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_upstreams.json")
	if err := os.WriteFile(path, []byte(`{"bullseye":"http://127.0.0.1:18743/mcp"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := OpenUpstreamRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reg.Resolve([]claudia.MCPServer{
		{Name: "bullseye", URL: staleNestedBullseye},
		{Name: "atlassian", URL: remoteAtlassian},
	}, dailyPrefix)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, s := range got {
		byName[s.Name] = s.URL
	}
	if byName["bullseye"] != directBullseye {
		t.Fatalf("resolved bullseye = %q; want owner direct %q", byName["bullseye"], directBullseye)
	}
	if byName["atlassian"] != remoteAtlassian {
		t.Fatalf("resolved atlassian = %q", byName["atlassian"])
	}
	var onDisk map[string]string
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["bullseye"] != directBullseye {
		t.Fatalf("registry poisoned with nested leftover: %v", onDisk)
	}
}

func TestT1039ResolveDoesNotRememberNestedLeftover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_upstreams.json")
	reg, err := OpenUpstreamRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reg.Resolve([]claudia.MCPServer{
		{Name: "bullseye", URL: staleNestedBullseye},
	}, dailyPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("resolved = %+v; unknown nested leftover must be dropped", got)
	}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		t.Fatalf("registry recorded a nested leftover: %s", raw)
	}
}

func TestT1039ResolveHealsPoisonedNestedRegistryFromDirectInventory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_upstreams.json")
	if err := os.WriteFile(path, []byte(`{"bullseye":"http://127.0.0.1:52322/upstream/bullseye"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := OpenUpstreamRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reg.Resolve([]claudia.MCPServer{
		{Name: "bullseye", URL: directBullseye},
	}, dailyPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != directBullseye {
		t.Fatalf("resolved = %+v; want direct grant", got)
	}
	var onDisk map[string]string
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["bullseye"] != directBullseye {
		t.Fatalf("registry not healed: %v", onDisk)
	}
}

// Fixture: stale nested proxy in the owner inventory + current direct
// map in mcp_upstreams.json. Fresh mint Advertised grants the direct
// URL, not :52322/upstream/bullseye and not this daemon's /upstream/.
func TestT1039AdvertisedGrantsDirectDespiteStaleNested(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_upstreams.json")
	if err := os.WriteFile(path, []byte(`{
  "bullseye": "http://127.0.0.1:18743/mcp",
  "atlassian": "https://mcp.atlassian.com/v1/mcp/authv2"
}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := OpenUpstreamRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h, err := Mount(mux, &MountArgs{
		PublicBase: "http://127.0.0.1:13705",
		Servers: []claudia.MCPServer{
			{Name: "bullseye", Type: "http", URL: staleNestedBullseye},
			{Name: "atlassian", Type: "http", URL: remoteAtlassian},
		},
		Upstreams: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]claudia.MCPServer{}
	for _, s := range h.Advertised() {
		byName[s.Name] = s
	}
	if byName["bullseye"].URL != directBullseye {
		t.Fatalf("Advertised bullseye = %q; want direct grant %q (🎯T1039)", byName["bullseye"].URL, directBullseye)
	}
	if byName["atlassian"].URL != "http://127.0.0.1:13705/upstream/atlassian" {
		t.Fatalf("Advertised atlassian = %q; remote OAuth still takes the T520 proxy", byName["atlassian"].URL)
	}
	var onDisk map[string]string
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["bullseye"] != directBullseye {
		t.Fatalf("registry poisoned: %v", onDisk)
	}
}

func TestT1039GrantDirectClassifier(t *testing.T) {
	if !GrantDirect(directBullseye) {
		t.Fatal("direct loopback /mcp must be granted")
	}
	if GrantDirect(staleNestedBullseye) {
		t.Fatal("nested /upstream/ leftover must not be granted")
	}
	if GrantDirect("http://127.0.0.1:13705/upstream/bullseye") {
		t.Fatal("this daemon's proxy URL must not be granted as direct")
	}
	if GrantDirect(remoteAtlassian) {
		t.Fatal("remote OAuth must stay on the T520 proxy")
	}
	if !IsNestedProxyURL(staleNestedBullseye) {
		t.Fatal("52322/upstream/bullseye is a nested leftover")
	}
	if IsNestedProxyURL(directBullseye) {
		t.Fatal("18743/mcp is not nested")
	}
}
