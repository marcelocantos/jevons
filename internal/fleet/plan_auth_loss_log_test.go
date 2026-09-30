// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
)

// 🎯T947: a plan login loss (invalid_grant and friends) must be
// attributable from jevonsd.log alone — the last recorded failure names
// when it happened, this process's pid, and the seat whose rehydrate
// surfaced it. This is jevons's half of "the logs record the last refresh
// that succeeded / the loss that followed" (claudia's broker owns the
// other half, the actual refresh attempt).
func TestPlanAuthLossIsLoggedWithPidAndSeat(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	t.Cleanup(func() { rehydrateFailures.Delete("po-lost") })
	RecordRehydrateFailure("po-lost", errors.New("omp: anthropic refresh failed: invalid_grant"))

	out := buf.String()
	if !strings.Contains(out, "plan auth: login failure recorded for seat") {
		t.Fatalf("expected a plan-auth-loss log line, got: %s", out)
	}
	if !strings.Contains(out, "seat=po-lost") {
		t.Fatalf("expected the log line to name the seat, got: %s", out)
	}
	wantPID := "pid=" + strconv.Itoa(os.Getpid())
	if !strings.Contains(out, wantPID) {
		t.Fatalf("expected the log line to carry %s, got: %s", wantPID, out)
	}
	if !strings.Contains(out, "time=") {
		t.Fatalf("expected the log line to carry a time, got: %s", out)
	}
}

// A failure that is not a plan login failure (a plain crash, say) is
// recorded on the seat for RehydrateHealth, but does not pollute the log
// with a plan-auth-loss line: this line means one thing only.
func TestPlanAuthLossLogSkipsNonPlanFailures(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	t.Cleanup(func() { rehydrateFailures.Delete("po-crashed") })
	RecordRehydrateFailure("po-crashed", fmt.Errorf("process exited: signal: killed"))

	if strings.Contains(buf.String(), "plan auth: login failure recorded for seat") {
		t.Fatalf("did not expect a plan-auth-loss line for a non-plan failure, got: %s", buf.String())
	}
}

// A recovery (err == nil) clears the failure and logs nothing new.
func TestPlanAuthLossLogSkipsRecovery(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	rehydrateFailures.Store("po-recovered", "omp: anthropic refresh failed: invalid_grant")
	t.Cleanup(func() { rehydrateFailures.Delete("po-recovered") })

	RecordRehydrateFailure("po-recovered", nil)

	if strings.Contains(buf.String(), "plan auth: login failure recorded for seat") {
		t.Fatalf("did not expect a plan-auth-loss line on recovery, got: %s", buf.String())
	}
}
