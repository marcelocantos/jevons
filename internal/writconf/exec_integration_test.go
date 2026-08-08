// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package writconf_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/writconf"
)

// TestCLIExecutor_AllowAndDenyNet is the product-path oracle when writ is
// available: real seatbelt + egress proxy deny undeclared hosts.
func TestCLIExecutor_AllowAndDenyNet(t *testing.T) {
	bin := writconf.ResolveWritBin()
	if bin == "" {
		t.Skip("writ binary not found (install or build marcelocantos/writ)")
	}
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not found")
	}

	m, err := writconf.FleetManifest(writconf.FleetManifestArgs{
		NetHosts: []string{"example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ex := &writconf.CLIExecutor{Bin: bin}
	ctx := context.Background()

	// Allowed host: should not produce a missing_capability denial.
	allow, err := ex.Exec(ctx, &writconf.ExecArgs{
		Manifest: m,
		Argv:     []string{curl, "-sS", "-o", os.DevNull, "-w", "%{http_code}", "--max-time", "8", "https://example.com/"},
		WorkDir:  t.TempDir(),
		Agent:    "t335-allow",
		Timeout:  20 * time.Second,
	})
	if err != nil && (allow == nil || !allow.Denied) {
		// Network flakes are skippable; sandbox denials are not.
		if allow != nil && strings.Contains(allow.Stdout+allow.Stderr, "missing_capability") {
			t.Fatalf("allowed host denied: %+v err=%v", allow, err)
		}
		t.Logf("allow path soft-fail (network?): err=%v out=%q errOut=%q", err, trim(allow), trimErr(allow))
	} else if allow != nil && allow.Denied {
		t.Fatalf("example.com must not be denied: %+v", allow.Event)
	}

	// Denied host: writ egress returns 403 / missing_capability.
	outFile := filepath.Join(t.TempDir(), "deny-body.txt")
	deny, err := ex.Exec(ctx, &writconf.ExecArgs{
		Manifest: m,
		Argv: []string{"/bin/sh", "-c",
			curl + " -sS -o " + outFile + " -w '%{http_code}' --max-time 5 http://blocked.invalid./ || true"},
		WorkDir: t.TempDir(),
		Agent:   "t335-deny",
		Timeout: 20 * time.Second,
	})
	body, _ := os.ReadFile(outFile)
	combined := string(body) + "\n"
	if deny != nil {
		combined += deny.Stdout + "\n" + deny.Stderr
	}
	if deny != nil && deny.Denied && deny.Event != nil {
		return // structured deny observed
	}
	if ev := writconf.ParseDenialOutput(combined); ev != nil {
		return
	}
	if strings.Contains(combined, "403") || strings.Contains(combined, "missing_capability") {
		return
	}
	// Seatbelt may drop the connection without JSON body — non-zero exit + no body is OK.
	if deny != nil && deny.ExitCode != 0 && len(body) == 0 {
		t.Logf("deny inferred from non-zero exit without body (seatbelt): exit=%d err=%v", deny.ExitCode, err)
		return
	}
	t.Fatalf("expected undeclared host deny; body=%q deny=%+v err=%v", truncate(combined, 400), deny, err)
}

func trim(r *writconf.ExecResult) string {
	if r == nil {
		return ""
	}
	return truncate(r.Stdout, 120)
}

func trimErr(r *writconf.ExecResult) string {
	if r == nil {
		return ""
	}
	return truncate(r.Stderr, 120)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
