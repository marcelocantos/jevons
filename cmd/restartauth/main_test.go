package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestT1062DirectShellRestartRequiresScopedHostPermit(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	root := t.TempDir()
	state := t.TempDir()
	for _, d := range []string{"scripts", "bin"} {
		if e := os.MkdirAll(filepath.Join(root, d), 0755); e != nil {
			t.Fatal(e)
		}
	}
	b, e := os.ReadFile(filepath.Join(source, "scripts/restart-jevonsd.sh"))
	if e != nil {
		t.Fatal(e)
	}
	script := filepath.Join(root, "scripts/restart-jevonsd.sh")
	if e = os.WriteFile(script, b, 0755); e != nil {
		t.Fatal(e)
	}
	helper := filepath.Join(root, "scripts/restart-auth.sh")
	build := exec.Command("go", "build", "-o", helper, "./cmd/restartauth")
	build.Dir = source
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build checker: %v %s", e, out)
	}
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", root}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %v %s", args, e, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", "scripts")
	git("commit", "-qm", "fixture")
	head := git("rev-parse", "HEAD")
	registry := []map[string]string{{"name": "jevons-po", "role": "product-owner"}, {"name": "jv-t1049-auto", "role": "worker", "target_id": "T1049"}, {"name": "jv-t1056-auto", "role": "worker", "target_id": "T1056"}}
	raw, _ := json.Marshal(registry)
	if e = os.WriteFile(filepath.Join(state, "agents.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	pid := filepath.Join(root, "pid")
	hash := filepath.Join(root, "build-hash")
	os.WriteFile(pid, []byte("4242"), 0600)
	os.WriteFile(hash, []byte("untouched"), 0600)
	runShell := func(seat, id string) (string, error) {
		t.Helper()
		os.WriteFile(filepath.Join(root, "restart.log"), nil, 0600)
		c := exec.Command("/bin/bash", "-c", `nohup /bin/bash "$1" >>"$2" 2>&1 & wait $!`, "fixture", script, filepath.Join(root, "restart.log"))
		c.Env = append(os.Environ(), "HOME="+root, "JEVONS_STATE_DIR="+state, "JEVONS_AGENT="+seat, "JEVONS_RESTART_AUTH_ID="+id)
		out, e := c.CombinedOutput()
		log, _ := os.ReadFile(filepath.Join(root, "restart.log"))
		return string(out) + string(log), e
	}
	// Repeat the direct Bash variant as well as the historical nohup background variant.
	direct := exec.Command("/bin/bash", script)
	direct.Env = append(os.Environ(), "HOME="+root, "JEVONS_STATE_DIR="+state, "JEVONS_AGENT=jv-t1049-auto")
	directOut, directErr := direct.CombinedOutput()
	if directErr == nil || !strings.Contains(string(directOut), "requires fresh scoped host permit") {
		t.Fatalf("direct Bash unexpectedly allowed: %v %s", directErr, directOut)
	}
	for _, seat := range []string{"jv-t1049-auto", "jv-t1056-auto"} {
		out, e := runShell(seat, "")
		if e == nil || !strings.Contains(out, "requires fresh scoped host permit") {
			t.Fatalf("%s unexpectedly allowed: %v %s", seat, e, out)
		}
	}
	for _, pair := range []struct{ path, want string }{{pid, "4242"}, {hash, "untouched"}} {
		b, _ := os.ReadFile(pair.path)
		if string(b) != pair.want {
			t.Fatalf("%s changed: %s", pair.path, b)
		}
	}
	t.Setenv("JEVONS_STATE_DIR", state)
	t.Setenv("JEVONS_AGENT", "jevons-po")
	// Permit issuance is host-registry checked and bound to seat, target, action,
	// HEAD and ancestry, with a ten-minute expiry. Pause marker prevents actual
	// restart after the accepted check; no real daemon or fixture daemon touched.
	if e = os.MkdirAll(filepath.Join(root, ".local/share/sen"), 0755); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(root, "pause")
	os.WriteFile(marker, []byte("fixture stop before build"), 0600)
	grant := exec.Command(helper, "grant", root, "jv-t1049-auto", "T1049", head, head)
	granted, e := grant.CombinedOutput()
	if e != nil {
		t.Fatalf("grant: %v %s", e, granted)
	}
	id := strings.TrimSpace(string(granted))
	t.Setenv("JEVONS_RESTART_PAUSE_MARKER", marker)
	out, e := runShell("jv-t1049-auto", id)
	if e == nil || !strings.Contains(out, "accepted permit=") || !strings.Contains(out, "fixture stop before build") {
		t.Fatalf("accepted audit / pause missing: %v %s", e, out)
	}
	audit, _ := os.ReadFile(filepath.Join(state, "restart-auth/audit.jsonl"))
	if !strings.Contains(string(audit), `"event":"grant"`) || !strings.Contains(string(audit), `"event":"accepted"`) {
		t.Fatalf("missing auditable grant and acceptance: %s", audit)
	}
	out, e = runShell("jv-t1056-auto", id)
	if e == nil || !strings.Contains(out, "scope mismatch") {
		t.Fatalf("cross-seat permit allowed: %v %s", e, out)
	}
}
