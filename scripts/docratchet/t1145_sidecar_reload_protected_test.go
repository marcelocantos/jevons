package docratchet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claudiaSourceDir prefers the sibling checkout (go.work / local dev); docratchet
// otherwise uses the go.mod-pinned module tree (GOWORK=off).
func claudiaSourceDir(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	sibling := filepath.Join(filepath.Dir(root), "claudia")
	if _, err := os.Stat(filepath.Join(sibling, "sidecar", "server.ts")); err == nil {
		return sibling
	}
	return claudiaModuleDir(t)
}

// 🎯T1145: OMP token reload must not clear sidecar ledger protected_paths, and
// broker reload messages must carry them (claudia 🎯T209).
func TestT1145SidecarReloadPreservesProtectedPaths(t *testing.T) {
	dir := claudiaSourceDir(t)
	server := readClaudiaFile(t, dir, "sidecar/server.ts")
	if strings.Contains(server, `msg.op === "load" || msg.protected_paths !== undefined`) {
		t.Fatal("sidecar/server.ts must not clear protected_paths on load when the field is omitted (🎯T1145)")
	}
	if !strings.Contains(server, "msg.protected_paths !== undefined") {
		t.Fatal("sidecar/server.ts must still apply protected_paths when the host sends them")
	}
	omp := readClaudiaFile(t, dir, "omp_agent.go")
	if !strings.Contains(omp, "ProtectedPaths: slices.Clone(c.protectedPaths)") {
		t.Fatal("omp_agent.go loadMessage must re-send protected_paths on seat reload (🎯T1145)")
	}
}
