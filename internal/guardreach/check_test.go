// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package guardreach

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCatalogHasPinningInstanceAndLivingSuccessor(t *testing.T) {
	var retired, living bool
	for _, e := range Catalog() {
		if e.ID == "handover-mark-delivered" && e.Status == StatusRetired && e.Guard == "MarkDelivered" {
			retired = true
		}
		if e.ID == "delivery-record-on-begun" && e.Status == StatusEnforced &&
			e.Guard == "RecordPayload" && e.Intermediate == "recordDeliveryEvidence" &&
			strings.Contains(e.Predicate, "OutcomeBegun") {
			living = true
		}
	}
	if !retired {
		t.Fatal("catalogue must retire MarkDelivered (🎯T419 pinning instance)")
	}
	if !living {
		t.Fatal("catalogue must enforce delivery-record-on-begun under OutcomeBegun")
	}
	if len(AuditOnly()) == 0 {
		t.Fatal("clause 3 requires at least one audit-only residual report entry")
	}
	if len(Enforced()) < 2 {
		t.Fatalf("expected several enforced guards, got %d", len(Enforced()))
	}
}

func TestCheckRepoPassesOnThisTree(t *testing.T) {
	root := repoRoot(t)
	findings := CheckRepo(root)
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// TestDirectGuardCallIsNotEnough is the pure distinction 🎯T419 acceptance 1
// asks for: a green unit test of the guard alone does not satisfy an
// enforced entry — the CallSiteTest marker must name a production-path test.
func TestDirectGuardCallIsNotEnough(t *testing.T) {
	dir := t.TempDir()
	// Minimal fake tree: production calls Intermediate+Guard, but the only
	// test invokes Guard directly and does not name CallSiteTest.
	mustWrite(t, filepath.Join(dir, "internal", "pkg", "prod.go"), `
package pkg
func Guard() {}
func Intermediate() { Guard() }
`)
	mustWrite(t, filepath.Join(dir, "internal", "pkg", "guard_test.go"), `
package pkg
import "testing"
func TestGuardWorksWhenCalled(t *testing.T) { Guard() }
`)
	// Inject a one-entry catalogue check by exercising callSiteTestPresent.
	if callSiteTestPresent(dir, "TestProductionPathUnderPredicate") {
		t.Fatal("direct unit test must not count as the call-site marker")
	}
	mustWrite(t, filepath.Join(dir, "internal", "pkg", "callsite_test.go"), `
package pkg
import "testing"
func TestProductionPathUnderPredicate(t *testing.T) { Intermediate() }
`)
	if !callSiteTestPresent(dir, "TestProductionPathUnderPredicate") {
		t.Fatal("call-site test marker must be found once present")
	}
}

func TestRetiredResurrectionIsFlagged(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "internal", "fleet", "migrate.go"), `
package fleet
func seed() { MarkDelivered("x") }
`)
	hits := retiredHits(dir, "MarkDelivered")
	if len(hits) == 0 {
		t.Fatal("MarkDelivered in production must be reported as resurrection")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/guardreach/check_test.go → repo root
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
