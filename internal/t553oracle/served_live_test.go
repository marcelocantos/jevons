// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package t553oracle

// 🎯T505 live probe, gated (JEVONS_T505_LIVE=1, `make test-t505-live`)
// because it reads the development daemon rather than a throwaway one. It is
// read-only: GET / and the assets that page references, nothing else. It
// closes the T553.1 residue "the oracle never inspects :13705 itself".

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestT505DevelopmentServesCommittedBundle(t *testing.T) {
	if os.Getenv("JEVONS_T505_LIVE") != "1" {
		t.Skip("live probe of the development daemon: run `make test-t505-live`")
	}
	base := os.Getenv("JEVONS_T505_URL")
	if base == "" {
		base = "http://127.0.0.1:13705"
	}
	servedFile := os.Getenv("JEVONS_RESTART_SERVED")
	if servedFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		servedFile = filepath.Join(home, ".jevons", "restart-jevonsd.served")
	}
	raw, err := os.ReadFile(servedFile)
	if err != nil {
		t.Fatalf("read %s: %v", servedFile, err)
	}
	sha, err := ParseServedSHA(string(raw))
	if err != nil {
		t.Fatalf("%s: %v", servedFile, err)
	}
	show := exec.Command("git", "show", sha+":ui/bundle.zip")
	show.Dir = "../.."
	bundle, err := show.Output()
	if err != nil {
		t.Fatalf("git show %s:ui/bundle.zip: %v", sha, err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	get := func(p string) ([]byte, error) {
		req, err := http.NewRequest(http.MethodGet, base+p, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "text/html,*/*")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s", resp.Status)
		}
		return b, nil
	}
	checked, err := ServedMatchesBundle(bundle, get)
	if err != nil {
		t.Fatalf("%s does not serve ui/bundle.zip@%.12s: %v (compared so far: %v)", base, sha, err, checked)
	}
	t.Logf("%s serves ui/bundle.zip@%.12s byte-for-byte: %v", base, sha, checked)
}
