// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package writconf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/writconf"
)

// Each mutation must refuse load and must never launch even a permissive CLI.
func TestT862_17MalformedPolicyRefusesLoadAndExecution(t *testing.T) {
	policies := []string{
		`{"schema_version":"1","fss":{"read":["/tmp/**"]}}`,
		`{"schema_version":"1","fs":{"reed":["/tmp/**"]}}`,
		`{"schema_version":"1","fs":{"edit":[{"glob":"/tmp/**","max_file":0}]}}`,
		`{"schema_version":"1","fs":{"edit":[""]}}`,
		`{"schema_version":"1","fs":{"edit":[null]}}`,
		`{"schema_version":"1","fs":{"edit":["/tmp/["]}}`,
		`{"schema_version":"1","fs":{"edit":[{"glob":"/tmp/**","max_files":-1}]}}`,
		`{"schema_version":"1","net":[{"hsot":"example.com"}]}`,
		`{"schema_version":"1"} {"fs":{}}`,
	}
	for _, policy := range policies {
		t.Run(policy, func(t *testing.T) {
			if m, err := writconf.Parse([]byte(policy)); err == nil || m != nil {
				t.Fatalf("malformed policy loaded: manifest=%+v error=%v", m, err)
			}
			dir := t.TempDir()
			marker := filepath.Join(dir, "executed")
			bin := filepath.Join(dir, "writ")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "policy.json")
			if err := os.WriteFile(path, []byte(policy), 0600); err != nil {
				t.Fatal(err)
			}
			res, err := (&writconf.CLIExecutor{Bin: bin}).Exec(t.Context(), &writconf.ExecArgs{ManifestPath: path, Argv: []string{"true"}})
			if err == nil || res != nil {
				t.Fatalf("malformed policy executed: result=%+v error=%v", res, err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("CLI ran: %v", err)
			}
		})
	}
}

func TestT862_17ValidPolicyStillLoads(t *testing.T) {
	for _, policy := range []string{
		`{"schema_version":"1","net":[{"host":"example.com"}]}`,
		`{"schema_version":"1","fs":{"read":["/tmp/**"],"edit":[{"glob":"/tmp/*","max_files":0}]}}`,
	} {
		if _, err := writconf.Parse([]byte(policy)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestT862_17CLIUsesValidatedSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	policy := `{"schema_version":"1","fs":{"read":["/tmp/**"]}}`
	if err := os.WriteFile(path, []byte(policy), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "writ")
	// A policy edited after validation must not change what the CLI consumes.
	script := "#!/bin/sh\nprintf '{}' > '" + path + "'\ncat \"$3\"\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	res, err := (&writconf.CLIExecutor{Bin: bin}).Exec(t.Context(), &writconf.ExecArgs{ManifestPath: path, Argv: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := writconf.Parse([]byte(res.Stdout))
	if err != nil || m.FS == nil || len(m.FS.Read) != 1 {
		t.Fatalf("snapshot lost: %+v %v", m, err)
	}
	if res.ManifestPath == path {
		t.Fatal("CLI consumed mutable source policy")
	}
}
