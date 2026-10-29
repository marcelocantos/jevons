// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// t947RootCause20260930 is acceptance criterion 1 for the 2026-09-30 09:23:31
// invalid_grant. claudia.log before T167 did not record which PID rotated the
// token, so the historical rotating process cannot be named from logs alone.
// Contemporaneous evidence: the development broker restarted at 09:23:28 and
// resumed seats cl-t73-frame-cap and cl-t130-grok-final; both failed refresh at
// 09:23:31 — consistent with two broker seat refresh paths racing on one
// single-use refresh token. Not the bun sidecar (no refresh token since T166),
// not an isolated broker (T940 was live). Forward attribution: claudia
// `omp plan login renewed` (T167) and jevons `plan auth: login failure recorded`.
const t947RootCause20260930 = "2026-09-30 09:23:31: unattributed in historical claudia.log; consumer class development-broker seat refresh race on broker resume (cl-t73-frame-cap + cl-t130-grok-final); not sidecar/isolated-broker"

// TestT947PlanRefreshOracleMap ratchets the cross-repo oracle map for 🎯T947:
// jevons consumers refuse refresh; jevonsd logs plan-auth loss; pinned claudia
// attributes broker renewals and bars non-broker rotation paths.
func TestT947PlanRefreshOracleMap(t *testing.T) {
	if t947RootCause20260930 == "" {
		t.Fatal("root-cause statement must stay pinned for the 09-23 loss")
	}

	fleet := readRepo(t, "internal/fleet/provider_caps.go")
	for _, needle := range []string{
		"logPlanAuthLoss",
		"plan auth: login failure recorded for seat",
		"🎯T947",
	} {
		if !strings.Contains(fleet, needle) {
			t.Fatalf("internal/fleet/provider_caps.go missing 🎯T947 seam %q", needle)
		}
	}
	if !strings.Contains(readRepo(t, "internal/fleet/plan_auth_loss_log_test.go"), "TestPlanAuthLossIsLoggedWithPidAndSeat") {
		t.Fatal("internal/fleet/plan_auth_loss_log_test.go must cover plan-auth loss logging (T973 / T947)")
	}

	broker := readRepo(t, "scripts/journey-suite/isolated_broker.go")
	for _, needle := range []string{
		"CLAUDIA_OMP_NO_REFRESH=1",
		"🎯T940",
	} {
		if !strings.Contains(broker, needle) {
			t.Fatalf("scripts/journey-suite/isolated_broker.go missing %q", needle)
		}
	}
	if !strings.Contains(readRepo(t, "scripts/journey-suite/bounce.go"), "CLAUDIA_OMP_NO_REFRESH=1") {
		t.Fatal("journey isolate daemon must force CLAUDIA_OMP_NO_REFRESH (T940/T947)")
	}
	if !strings.Contains(readRepo(t, "scripts/journey-suite/t940_no_refresh_test.go"), "TestT940IsolateDaemonEnvNeverRefreshes") {
		t.Fatal("scripts/journey-suite/t940_no_refresh_test.go must refuse isolate-daemon refresh")
	}

	claudiaDir := claudiaModuleDir(t)
	auth := readFile(t, filepath.Join(claudiaDir, "omp", "auth.go"))
	for _, needle := range []string{
		`NoNetworkEnv = "CLAUDIA_OMP_NO_REFRESH"`,
		`omp plan login renewed`,
		"Caller",
	} {
		if !strings.Contains(auth, needle) {
			t.Fatalf("pinned claudia omp/auth.go missing %q", needle)
		}
	}
	for _, rel := range []string{
		"omp/t167_attribution_test.go",
		"omp/t940_no_refresh_test.go",
	} {
		if _, err := os.Stat(filepath.Join(claudiaDir, rel)); err != nil {
			t.Fatalf("pinned claudia %s: %v", rel, err)
		}
	}
	if !strings.Contains(readFile(t, filepath.Join(claudiaDir, "omp/t167_attribution_test.go")), "TestT167EveryRenewalNamesItsCaller") {
		t.Fatal("pinned claudia must ship T167 renewal attribution tests")
	}
	if !strings.Contains(readFile(t, filepath.Join(claudiaDir, "daemon/clauses_test.go")), "legacy CLI seat transport removed") {
		t.Fatal("pinned claudia must refuse legacy CLI seats that could hold their own grant")
	}
}

func claudiaModuleDir(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/marcelocantos/claudia")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list claudia: %v", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("empty claudia module dir")
	}
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
