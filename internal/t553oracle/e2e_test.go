// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package t553oracle

// 🎯T553.1 end-to-end oracle. Gated (JEVONS_T553_ORACLE=1, run by
// `make test-t553-oracle`) because it does two real npm ci + vite + go builds.
// Everything happens in a scratch clone under the OS temp dir; the shared
// clone and the development daemon on :13705 are never touched.

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/buildsnap"
)

func run(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, tail(out))
	}
	return strings.TrimSpace(string(out))
}

func tail(b []byte) string {
	if len(b) > 3000 {
		b = b[len(b)-3000:]
	}
	return string(b)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	p := l.Addr().(*net.TCPAddr).Port
	if p == 13705 {
		t.Fatal("refusing :13705 (development daemon)")
	}
	return p
}

// servedScripts boots bin on a throwaway port and returns the concatenated
// JS that GET / references, as the browser would execute it.
func servedScripts(t *testing.T, bin, scratch, name string) string {
	t.Helper()
	port := freePort(t)
	state := filepath.Join(scratch, "state-"+name)
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(state, "config.yaml")
	body := fmt.Sprintf("owner_name: T553Oracle\nbind_addr: 127.0.0.1\nport: %d\nstate_dir: %q\nworkdir: %q\n", port, state, state)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "-config", cfg, "-port", fmt.Sprint(port), "-bind", "127.0.0.1", "-workdir", state)
	cmd.Dir = state
	logf, _ := os.Create(filepath.Join(state, "jevonsd.log"))
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Env = append(os.Environ(), "CLAUDIA_NO_BROKER=1", "XDG_STATE_HOME="+state)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	get := func(p string) (string, error) {
		resp, err := http.Get(base + p)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("GET %s: %s", p, resp.Status)
		}
		return string(b), nil
	}
	var index string
	var err error
	for deadline := time.Now().Add(3 * time.Minute); time.Now().Before(deadline); time.Sleep(time.Second) {
		if index, err = get("/"); err == nil {
			break
		}
	}
	if err != nil {
		lb, _ := os.ReadFile(filepath.Join(state, "jevonsd.log"))
		t.Fatalf("%s daemon never served GET /: %v\n%s", name, err, tail(lb))
	}
	paths := ServedScriptPaths(index)
	if len(paths) == 0 {
		t.Fatalf("%s: GET / references no script: %.300s", name, index)
	}
	var js strings.Builder
	for _, p := range paths {
		s, err := get(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		js.WriteString(s)
	}
	if js.Len() < 10_000 {
		t.Fatalf("%s: served JS only %d bytes; not the React app", name, js.Len())
	}
	t.Logf("%s served %d script(s), %d bytes from :%d", name, len(paths), js.Len(), port)
	return js.String()
}

func TestT553SeededThrowNotExecutedByDaily(t *testing.T) {
	if os.Getenv("JEVONS_T553_ORACLE") != "1" {
		t.Skip("end-to-end oracle: run `make test-t553-oracle`")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	head := run(t, root, nil, "git", "rev-parse", "HEAD")

	scratch, err := os.MkdirTemp("", "t553-oracle-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(scratch)
		if _, err := os.Stat(scratch); err == nil {
			t.Errorf("scratch %s still exists", scratch)
		}
	})
	clone := filepath.Join(scratch, "jevons")
	run(t, scratch, nil, "git", "clone", "--quiet", "--no-hardlinks", "--no-checkout", root, clone)
	run(t, clone, nil, "git", "checkout", "--quiet", "--detach", head)
	// Mirror the org layout so buildsnap's sibling go.work inject resolves
	// exactly as it does for the real clone.
	if sib := filepath.Join(filepath.Dir(root), "claudia"); dirExists(sib) {
		if err := os.Symlink(sib, filepath.Join(scratch, "claudia")); err != nil {
			t.Fatal(err)
		}
	}

	marker := NewMarker()
	if err := SeedThrow(clone, marker); err != nil {
		t.Fatal(err)
	}
	st := run(t, clone, nil, "git", "status", "--porcelain")
	if !strings.Contains(st, UIThrowPath) || !strings.Contains(st, GoThrowPath) {
		t.Fatalf("seeded throw is not an uncommitted change:\n%s", st)
	}

	// CONTROL: builds that read the working tree pick the throw up.
	ctlBin := filepath.Join(scratch, "ctl-jevonsd")
	run(t, clone, nil, "make", "ui-build")
	zipBytes, err := os.ReadFile(filepath.Join(clone, "ui", "bundle.zip"))
	if err != nil {
		t.Fatal(err)
	}
	uiCtl, err := ZipContains(zipBytes, marker)
	if err != nil {
		t.Fatal(err)
	}
	run(t, clone, nil, "go", "build", "-o", ctlBin, "./cmd/jevonsd")
	binBytes, _ := os.ReadFile(ctlBin)
	goCtl := BytesContain(binBytes, marker)
	ctlJS := servedScripts(t, ctlBin, scratch, "control")
	servedCtl := strings.Contains(ctlJS, marker)

	// SNAPSHOT: the development surface (buildsnap of committed HEAD), over the same
	// clone whose working tree — including its rebuilt bundle.zip — is dirty.
	snapBin := filepath.Join(scratch, "snap-jevonsd")
	res, err := buildsnap.Run(buildsnap.Config{
		RepoRoot: clone, SnapDir: filepath.Join(scratch, "snap"),
		Target: "bin/jevonsd", Artifact: "bin/jevonsd", Dest: snapBin,
		Log: func(s string) { t.Log("buildsnap: " + s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.HEAD != head {
		t.Fatalf("snapshot built %s, want %s", res.HEAD, head)
	}
	snapBytes, _ := os.ReadFile(snapBin)
	goSnap := BytesContain(snapBytes, marker)
	snapZip, err := os.ReadFile(filepath.Join(scratch, "snap", "ui", "bundle.zip"))
	if err != nil {
		t.Fatal(err)
	}
	uiSnap, err := ZipContains(snapZip, marker)
	if err != nil {
		t.Fatal(err)
	}
	snapJS := servedScripts(t, snapBin, scratch, "snapshot")
	servedSnap := strings.Contains(snapJS, marker)

	controls := []Verdict{{"working-tree ui bundle (make ui-build)", uiCtl}, {"working-tree go binary", goCtl}, {"working-tree binary, served JS", servedCtl}}
	snaps := []Verdict{{"HEAD-snapshot ui bundle", uiSnap}, {"HEAD-snapshot go binary", goSnap}, {"HEAD-snapshot binary, served JS", servedSnap}}
	for _, v := range append(controls, snaps...) {
		t.Logf("%-40s throw present=%v", v.Surface, v.Found)
	}
	if err := Judge(controls, snaps); err != nil {
		t.Fatal(err)
	}
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
