package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T1152: frontierConsumeSweep reads and parses the ledger once, no matter
// how many target rows the owner-blocker scan walks.
func TestT1152FrontierSweepReadsLedgerOnce(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("schema_version: \"1.0\"\ntargets:\n")
	const n = 40
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  T%d:\n    name: leaf %d\n    status: identified\n", 1000+i, i)
	}
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "po1",
		Purpose: claudia.PurposeWork, Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	targetfile.ResetLedgerIOStats()
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetOwnerQuestionsDir(t.TempDir())

	_ = s.frontierConsumeSweep(FrontierConsumeLoopArgs{
		Server:            s,
		Workdir:           dir,
		ParentPO:          "jevons-po",
		MaxSpawnsPerCycle: 0,
		Spawn: func(leaf targetfile.FrontierLeaf, workerName, parent string) error {
			return nil
		},
	}, nil)

	if reads := targetfile.LedgerReadOps(); reads != 1 {
		t.Fatalf("ledger reads = %d; want 1 for %d leaves", reads, n)
	}
	if parses := targetfile.LedgerParseOps(); parses != 1 {
		t.Fatalf("ledger parses = %d; want 1 for %d leaves", parses, n)
	}
}
