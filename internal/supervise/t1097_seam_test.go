// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// 🎯T1097: the seam's own default refuses in a test binary instead of
// execing launchctl. A binary that forgot to install a fake fails loudly,
// naming the import, rather than reaching the owner's launchd on the run
// before the ratchet notices.
func TestT1097DefaultSeamRefusesInATestBinary(t *testing.T) {
	for _, args := range [][]string{
		{"print", supervise.AgentDomain() + "/" + supervise.AgentLabel},
		{"bootout", supervise.AgentDomain() + "/" + supervise.AgentLabel},
		{"kickstart", "-k", supervise.AgentDomain() + "/" + supervise.DaemonLabel},
	} {
		out, err := supervise.DefaultLaunchctl(args...)
		if err == nil {
			t.Fatalf("default seam ran launchctl %v in a test binary: %s", args, out)
		}
		if !strings.Contains(err.Error(), supervise.LaunchdFakePackage) {
			t.Errorf("refusal %q does not name the fix %q", err, supervise.LaunchdFakePackage)
		}
	}
}

// Launchctl is the exported door to the seam, and ReplaceLaunchctl swaps
// what answers it.
func TestT1097LaunchctlGoesThroughTheSeam(t *testing.T) {
	var got []string
	prev := supervise.ReplaceLaunchctl(func(args ...string) ([]byte, error) {
		got = append(got, strings.Join(args, " "))
		return []byte("ok"), nil
	})
	t.Cleanup(func() { supervise.ReplaceLaunchctl(prev) })
	if out, err := supervise.Launchctl("print", "gui/1/x"); err != nil || string(out) != "ok" {
		t.Fatalf("Launchctl = %q, %v", out, err)
	}
	if len(got) != 1 || got[0] != "print gui/1/x" {
		t.Fatalf("seam saw %q, want one print", got)
	}
}
