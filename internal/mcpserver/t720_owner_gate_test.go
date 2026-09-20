// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/ownergate"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

const t720Evidence = "landed at abcdef1; GATE id=deadbeef GREEN"

func t720RecordReq(cwd, target string) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd":      cwd,
		"target":   target,
		"op":       "record",
		"question": "Does the landed work look right after a hard reload?",
		"evidence": t720Evidence,
		"by":       "jevons-po",
	}
	return req
}

func t720AnswerReq(cwd, target, verdict string) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd":     cwd,
		"target":  target,
		"op":      "answer",
		"verdict": verdict,
		"by":      "jevons-po",
	}
	return req
}

func TestT720OwnerGateRecordShellsApplyNotCommitOwner(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	var saw []string
	runBullseye = func(args ...string) (string, error) {
		saw = append([]string{}, args...)
		return "ok: true\nchanged: T2\n", nil
	}

	s := New(t.TempDir(), nil, nil)
	res, err := s.handleOwnerGate(context.Background(), t720RecordReq(t.TempDir(), "T2"))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("record: %s", targetFileToolText(res))
	}
	if len(saw) == 0 || saw[0] != "apply" {
		t.Fatalf("want apply, got %v", saw)
	}
	for _, a := range saw {
		name, _, _ := strings.Cut(a, "=")
		if name == "--owner" {
			t.Fatalf("passed --owner, which 0.56.0 rejects: %v", saw)
		}
	}
	joined := strings.Join(saw, "\n")
	if !strings.Contains(joined, "owner="+ownergate.OwnerHandle) {
		t.Fatalf("missing --set owner=: %v", saw)
	}
	if !strings.Contains(joined, ownergate.MarkerAwaiting) {
		t.Fatalf("missing awaiting reason: %v", saw)
	}
}

func TestT720OwnerGateFlagsMatchInstalledCLI(t *testing.T) {
	requireBullseye(t)
	reason, err := ownergate.Record{
		Question: "Does this look right?",
		Evidence: t720Evidence,
	}.Reason()
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]string{
		ownerGateRecordArgs(t.TempDir(), "T2", reason),
		ownerGateAnswerArgs(t.TempDir(), "T2"),
	}
	for _, args := range cases {
		accepted := probeAcceptedFlags(t, args[0])
		for _, name := range flagNames(args) {
			if _, ok := accepted[name]; !ok {
				t.Errorf("tool passes %s to bullseye %s, but that CLI rejects it (🎯T720): accepted %v",
					name, args[0], acceptedKeys(accepted))
			}
		}
	}
}

func TestT720OwnerGateUnknownFlagGoesRed(t *testing.T) {
	requireBullseye(t)
	reason, err := ownergate.Record{
		Question: "Does this look right?",
		Evidence: t720Evidence,
	}.Reason()
	if err != nil {
		t.Fatal(err)
	}
	args := append(ownerGateRecordArgs(t.TempDir(), "T2", reason), "--not-a-real-flag")
	out, err := runBullseye(args...)
	if err == nil {
		t.Fatalf("injected unrecognised flag succeeded:\n%s", out)
	}
	if !strings.Contains(out, "unrecognised flag") {
		t.Fatalf("want unrecognised flag, got err=%v\n%s", err, out)
	}
}

func TestT720OwnerGateRecordRoundTripAndAnswersClear(t *testing.T) {
	requireBullseye(t)
	s := New(t.TempDir(), nil, nil)

	t.Run("accept", func(t *testing.T) {
		t720RoundTrip(t, s, "accept")
	})
	t.Run("reject", func(t *testing.T) {
		t720RoundTrip(t, s, "reject")
	})
}

func t720RoundTrip(t *testing.T, s *Server, verdict string) {
	t.Helper()
	repo, id := t720FixtureTarget(t)

	res, err := s.handleOwnerGate(context.Background(), t720RecordReq(repo, id))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("record: %s", targetFileToolText(res))
	}
	text := targetFileToolText(res)
	if !strings.Contains(text, ownergate.MarkerAwaiting) {
		t.Fatalf("record response drops the claim: %s", text)
	}

	leaves, _, err := targetfile.LoadFrontierLeavesFromCwd(repo)
	if err != nil {
		t.Fatal(err)
	}
	leaf, ok := t720Leaf(leaves, id)
	if !ok {
		t.Fatalf("frontier read dropped 🎯%s after record", id)
	}
	if leaf.OwnedBy != ownergate.OwnerHandle {
		t.Fatalf("owned_by.owner=%q, want %s", leaf.OwnedBy, ownergate.OwnerHandle)
	}
	if !strings.Contains(leaf.OwnedByReason, ownergate.MarkerAwaiting) ||
		!strings.Contains(leaf.OwnedByReason, "abcdef1") {
		t.Fatalf("reason not carried: %q", leaf.OwnedByReason)
	}
	kind := poproactive.ClassifyLeaf(poproactive.LeafObs{
		ID:            leaf.ID,
		Name:          leaf.Name,
		OwnedBy:       leaf.OwnedBy,
		OwnedByReason: leaf.OwnedByReason,
	})
	if kind != poproactive.LeafSkipAwaitingOwnerVerdict {
		t.Fatalf("after record: classified %s, want skip_awaiting_owner_verdict", kind)
	}

	res, err = s.handleOwnerGate(context.Background(), t720AnswerReq(repo, id, verdict))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("answer %s: %s", verdict, targetFileToolText(res))
	}
	if _, assigned := targetfile.LoadTargetOwnerFromCwd(repo, id); assigned {
		t.Fatalf("answer %s left the assignment in place", verdict)
	}
	leaves, _, err = targetfile.LoadFrontierLeavesFromCwd(repo)
	if err != nil {
		t.Fatal(err)
	}
	leaf, ok = t720Leaf(leaves, id)
	if !ok {
		t.Fatalf("frontier dropped 🎯%s after answer", id)
	}
	kind = poproactive.ClassifyLeaf(poproactive.LeafObs{
		ID:            leaf.ID,
		Name:          leaf.Name,
		OwnedBy:       leaf.OwnedBy,
		OwnedByReason: leaf.OwnedByReason,
	})
	if kind != poproactive.LeafReady {
		t.Fatalf("after answer %s: classified %s, want ready", verdict, kind)
	}
}

func TestT720OwnerGateRecordRefusesWithoutEvidence(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	runBullseye = func(args ...string) (string, error) {
		t.Fatalf("bullseye must not run on a refused record: %v", args)
		return "", nil
	}
	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd":      t.TempDir(),
		"target":   "T2",
		"op":       "record",
		"question": "Does this look right?",
		"evidence": "done",
	}
	res, err := s.handleOwnerGate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("want refuse, got %s", targetFileToolText(res))
	}
}

func requireBullseye(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bullseye"); err != nil {
		t.Skip("bullseye not on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func t720FixtureTarget(t *testing.T) (repo, id string) {
	t.Helper()
	repo = t.TempDir()
	gitInit(t, repo)
	open := exec.Command("bullseye", "open", "--cwd", repo, "--location", "in_repo")
	if out, err := open.CombinedOutput(); err != nil {
		t.Fatalf("bullseye open: %v\n%s", err, out)
	}
	track := exec.Command("bullseye", "commit", "--op", "track",
		"--cwd", repo,
		"--name", "T720 hermetic: owner-gate assignment",
		"--acceptance", "op=record writes owned_by and a frontier read parks")
	out, err := track.CombinedOutput()
	if err != nil {
		t.Fatalf("bullseye track: %v\n%s", err, out)
	}
	id = parseBullseyeTrackID(string(out))
	if id == "" {
		t.Fatalf("no id from track:\n%s", out)
	}
	return repo, id
}

func t720Leaf(leaves []targetfile.FrontierLeaf, id string) (targetfile.FrontierLeaf, bool) {
	for _, leaf := range leaves {
		if leaf.ID == id {
			return leaf, true
		}
	}
	return targetfile.FrontierLeaf{}, false
}

func flagNames(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name, _, _ := strings.Cut(a, "=")
		out = append(out, name)
	}
	return out
}

func probeAcceptedFlags(t *testing.T, subcmd string) map[string]struct{} {
	t.Helper()
	cmd := exec.Command("bullseye", subcmd, "--__jevons_flag_probe")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("probe %s: expected unrecognised-flag error, got:\n%s", subcmd, out)
	}
	text := string(out)
	const prefix = "Accepted flags: "
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			line = strings.TrimPrefix(strings.TrimSpace(l), prefix)
			break
		}
	}
	if line == "" {
		t.Fatalf("probe %s: no Accepted flags line:\n%s", subcmd, text)
	}
	set := map[string]struct{}{}
	for _, f := range strings.Split(line, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			set[f] = struct{}{}
		}
	}
	return set
}

func acceptedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}
