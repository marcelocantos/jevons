package treeguard_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/treeguard"
)

// TestSweepReportsLostLinesOnly is the provider-independent detector half of
// 🎯T391: when a guarded file changes without the guard seeing it and lines
// disappear, Sweep names them. Additions alone must stay quiet.
func TestSweepReportsLostLinesOnly(t *testing.T) {
	repo := t.TempDir()
	storeRoot := t.TempDir()
	hot := filepath.Join(repo, "web", "index.html")
	if err := os.MkdirAll(filepath.Dir(hot), 0o755); err != nil {
		t.Fatal(err)
	}
	base := []byte("<script src=\"fleet_cycle.js\"></script>\n<title>old</title>\n")
	if err := os.WriteFile(hot, base, 0o644); err != nil {
		t.Fatal(err)
	}

	env := &treeguard.Env{
		Store:    &treeguard.Store{Root: storeRoot},
		RepoRoot: repo,
		Guarded:  []string{"web/index.html"},
		Now:      time.Now,
	}
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	// Bootstrap: first sight adopts silently.
	findings, err := env.Sweep(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("bootstrap reported findings: %+v", findings)
	}

	clobber := []byte("<title>sidebar-composer-title</title>\n")
	if err := os.WriteFile(hot, clobber, 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err = env.Sweep(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 loss finding, got %+v", findings)
	}
	if findings[0].RelPath != "web/index.html" {
		t.Errorf("RelPath = %q", findings[0].RelPath)
	}
	joined := strings.Join(findings[0].Lost, "\n")
	if !strings.Contains(joined, "fleet_cycle.js") {
		t.Errorf("lost lines did not name fleet_cycle.js: %v", findings[0].Lost)
	}
	report := treeguard.FormatSweepReport(findings)
	if !strings.Contains(report, "UNGUARDED CHANGE") || !strings.Contains(report, "fleet_cycle.js") {
		t.Errorf("report missing loss markers:\n%s", report)
	}

	// Addition-only must not cry wolf.
	if err := env.Store.Journal(hot, clobber, now, "s", treeguard.ViaTool); err != nil {
		t.Fatal(err)
	}
	added := append(clobber, []byte("<p>new</p>\n")...)
	if err := os.WriteFile(hot, added, 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err = env.Sweep(now.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("addition-only change reported as loss: %+v", findings)
	}
}
