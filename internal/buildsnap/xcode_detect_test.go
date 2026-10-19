// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

// TestIsXcodeLicenseFailure is the pure classifier oracle for 🎯T655.
// The public-package integration tests drive the fallback path; this
// pins the decision that routes a build into that path.
func TestIsXcodeLicenseFailure(t *testing.T) {
	exit69 := exec.Command("sh", "-c", "exit 69").Run()
	var ee *exec.ExitError
	if !errors.As(exit69, &ee) || ee.ExitCode() != 69 {
		t.Fatalf("fixture exit 69 did not produce ExitError: %v", exit69)
	}

	cases := []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{"nil", "", nil, false},
		{"prose", "You have not agreed to the Xcode license agreements.", fmt.Errorf("exit status 1"), true},
		{"sdk prose", "agree to the Xcode and Apple SDKs license", fmt.Errorf("boom"), true},
		{"exit69 err", "", exit69, true},
		{"exit69 wrapped", "", fmt.Errorf("build: %w", exit69), true},
		{"exit69 string", "", fmt.Errorf("exit status 69"), true},
		{"real compile", "undefined: FooBar", fmt.Errorf("exit status 1"), false},
		{"exit1 only", "", fmt.Errorf("exit status 1"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isXcodeLicenseFailure(tc.out, tc.err)
			if got != tc.want {
				t.Fatalf("isXcodeLicenseFailure(%q, %v) = %v, want %v", tc.out, tc.err, got, tc.want)
			}
		})
	}
}

func TestMergeEnvReplacesKey(t *testing.T) {
	got := mergeEnv([]string{"PATH=/bin", "DEVELOPER_DIR=/Xcode", "HOME=/tmp"}, []string{"DEVELOPER_DIR=/CLT"})
	want := map[string]string{"PATH": "/bin", "DEVELOPER_DIR": "/CLT", "HOME": "/tmp"}
	seen := map[string]string{}
	for _, kv := range got {
		k, v, ok := splitKV(kv)
		if !ok {
			t.Fatalf("bad entry %q", kv)
		}
		seen[k] = v
	}
	for k, v := range want {
		if seen[k] != v {
			t.Errorf("%s = %q, want %q", k, seen[k], v)
		}
	}
}

func splitKV(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}
