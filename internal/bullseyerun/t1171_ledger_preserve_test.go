// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package bullseyerun_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/bullseyerun"
)

func fileModeTrack(t *testing.T, clone, name, acceptance string) (id string, out string) {
	t.Helper()
	cmd := exec.Command(bullseyerun.Command(), "commit", "--op", "track", "--cwd", clone,
		"--name", name, "--acceptance", acceptance)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("file-mode track: %v\n%s", err, buf.String())
	}
	out = buf.String()
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ids:") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "ids:"))
			break
		}
	}
	if id == "" {
		t.Fatalf("file-mode track missing ids:\n%s", out)
	}
	return id, out
}

func TestT1171FileModeRowSurvivesWriterMutation(t *testing.T) {
	requireCommitModeBullseye(t)
	clone := t.TempDir()
	initGitRepo(t, clone)

	_, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", clone,
		"--name", "seed ledger", "--acceptance", "baseline on ref",
	)
	if err != nil {
		t.Fatal(err)
	}
	ledgerClean(t, clone)

	fileModeID, _ := fileModeTrack(t, clone, "file-mode survivor T1171", "hermetic fixture row")
	if err := exec.Command("git", "-C", clone, "diff", "--quiet", "HEAD", "--", "bullseye.yaml").Run(); err == nil {
		t.Fatal("file-mode row should be uncommitted vs HEAD")
	}

	trackOut, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", clone,
		"--name", "writer mutation after file-mode row", "--acceptance", "both rows on ref",
	)
	if err != nil {
		t.Fatalf("RunMutation: %v\n%s", err, trackOut)
	}
	ledgerClean(t, clone)

	blob := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	if !strings.Contains(blob, "file-mode survivor T1171") {
		t.Fatalf("HEAD missing file-mode row:\n%s", blob)
	}
	if !strings.Contains(blob, "writer mutation after file-mode row") {
		t.Fatalf("HEAD missing writer row:\n%s", blob)
	}

	writerID := ""
	for _, line := range strings.Split(trackOut, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ids:") {
			writerID = strings.TrimSpace(strings.TrimPrefix(line, "ids:"))
			break
		}
	}
	if writerID == fileModeID {
		t.Fatalf("writer reused file-mode id %s", fileModeID)
	}

	secondOut, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", clone,
		"--name", "second allocation check", "--acceptance", "no id reuse",
	)
	if err != nil {
		t.Fatalf("second track: %v\n%s", err, secondOut)
	}
	ledgerClean(t, clone)
	thirdID := ""
	for _, line := range strings.Split(secondOut, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ids:") {
			thirdID = strings.TrimSpace(strings.TrimPrefix(line, "ids:"))
			break
		}
	}
	if thirdID == fileModeID || thirdID == writerID {
		t.Fatalf("id reuse: file=%s writer=%s third=%s", fileModeID, writerID, thirdID)
	}

	wt, err := os.ReadFile(filepath.Join(clone, "bullseye.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	head := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	if string(wt) != head {
		t.Fatalf("working tree != HEAD after mutation")
	}
}

func TestT1171WorkingTreeMatchesRefAfterCommitModeWrite(t *testing.T) {
	requireCommitModeBullseye(t)
	clone := t.TempDir()
	initGitRepo(t, clone)

	_, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", clone,
		"--name", "stale mirror probe", "--acceptance", "wt equals ref",
	)
	if err != nil {
		t.Fatal(err)
	}
	ledgerClean(t, clone)

	stale := gitOut(t, clone, "show", "HEAD~1:bullseye.yaml")
	writeFile(t, filepath.Join(clone, "bullseye.yaml"), stale)

	_, err = bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", clone,
		"--name", "after stale working tree", "--acceptance", "refreshed",
	)
	if err != nil {
		t.Fatal(err)
	}
	ledgerClean(t, clone)

	wt := gitOut(t, clone, "show", ":bullseye.yaml")
	head := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	if wt != head {
		t.Fatalf("index/working tree still stale after writer mutation")
	}
}

func TestT1171PreserveCommitsUncommittedRow(t *testing.T) {
	requireCommitModeBullseye(t)
	clone := t.TempDir()
	initGitRepo(t, clone)
	_, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", clone,
		"--name", "seed", "--acceptance", "ok",
	)
	if err != nil {
		t.Fatal(err)
	}
	fileModeID, _ := fileModeTrack(t, clone, "must survive", "ok")

	out, err := bullseyerun.RunMutation(
		"commit", "--op", "track", "--cwd", clone,
		"--name", "after preserve", "--acceptance", "ok",
	)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	blob := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	if !strings.Contains(blob, "must survive") {
		t.Fatalf("uncommitted row lost:\n%s", blob)
	}
	if !strings.Contains(blob, fileModeID+":") {
		t.Fatalf("id %s missing from HEAD:\n%s", fileModeID, blob)
	}
}
