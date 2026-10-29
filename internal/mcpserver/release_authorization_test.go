package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestReleaseAuthorizationExactActionAndRevocationAcrossRestart(t *testing.T) {
	state := t.TempDir()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	call := func(s *Server, args map[string]any) map[string]any {
		t.Helper()
		r, e := s.handleReleaseAuthorization(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
		if e != nil || r.IsError {
			t.Fatalf("%v: %v %+v", args, e, r)
		}
		out := map[string]any{}
		if json.Unmarshal([]byte(ideaToolText(r)), &out) != nil {
			t.Fatalf("not JSON: %s", ideaToolText(r))
		}
		return out
	}
	s := New(repo, nil, nil)
	s.SetOwnerQuestionsDir(state)
	reg, err := claudia.NewRegistry(filepath.Join(state, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jevons", Role: "overseer", Purpose: claudia.PurposeOverseer, SessionID: "session-overseer", WorkDir: repo}); err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	for _, action := range []string{"minor-release", "patch-release", "major-release"} {
		if call(s, map[string]any{"repo": repo, "action": action})["authorized"] != false {
			t.Fatal("default allowed", action)
		}
	}
	grant, e := s.handleReleaseAuthorization(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"op": "grant", "repo": repo, "action": "minor-release", "actor": "owner", "evidence": "owner statement 2026-10-28"},
		Meta:      &mcp.Meta{AdditionalFields: map[string]any{"claudecode/sessionId": "session-overseer"}},
	}})
	if e != nil || grant.IsError {
		t.Fatalf("grant: %v %+v", e, grant)
	}
	cold := New(repo, nil, nil)
	cold.SetOwnerQuestionsDir(state)
	if call(cold, map[string]any{"repo": repo, "action": "minor-release"})["authorized"] != true {
		t.Fatal("grant absent after restart")
	}
	for _, action := range []string{"patch-release", "major-release"} {
		if call(cold, map[string]any{"repo": repo, "action": action})["authorized"] != false {
			t.Fatal("wrong action allowed", action)
		}
	}
	other := t.TempDir()
	os.Mkdir(filepath.Join(other, ".git"), 0700)
	if call(cold, map[string]any{"repo": other, "action": "minor-release"})["authorized"] != false {
		t.Fatal("another repo allowed")
	}
	revoke, e := cold.handleReleaseAuthorization(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"op": "revoke", "repo": repo, "action": "minor-release", "actor": "owner", "evidence": "owner revoked"}}})
	if e != nil || revoke.IsError {
		t.Fatalf("revoke: %v %+v", e, revoke)
	}
	if call(s, map[string]any{"repo": repo, "action": "minor-release"})["authorized"] != false {
		t.Fatal("revocation not visible")
	}
}

func TestReleaseAuthorizationCannotSelfGrantOrWiden(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0700)
	s := New(repo, nil, nil)
	s.SetOwnerQuestionsDir(t.TempDir())
	for _, args := range []map[string]any{
		{"op": "grant", "repo": repo, "action": "minor-release", "actor": "worker", "evidence": "I want to"},
		{"op": "grant", "repo": repo, "action": "minor-release", "actor": "owner"},
		{"op": "grant", "repo": repo, "action": "major-release", "actor": "owner", "evidence": "x"},
		{"op": "grant", "repo": "relative", "action": "minor-release", "actor": "owner", "evidence": "x"},
	} {
		r, e := s.handleReleaseAuthorization(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
		if e != nil || !r.IsError {
			t.Fatalf("unexpected grant %v: %v %+v", args, e, r)
		}
	}
}

// The actor parameter is a claim, not a credential. The production handler
// sees the seat through its MCP origin, including on seat-pair replay.
func TestReleaseAuthorizationWorkerPendingUntilOwnerAnswer(t *testing.T) {
	state, repo := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	s := New(repo, nil, nil)
	s.SetOwnerQuestionsDir(state)
	reg, err := claudia.NewRegistry(filepath.Join(state, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jv-t1270-worker", Role: "worker", SessionID: "session-worker", WorkDir: repo}); err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	call := func(ctx context.Context, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, e := s.handleReleaseAuthorization(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
		if e != nil || r.IsError {
			t.Fatalf("call %v: %v %+v", args, e, r)
		}
		return r
	}
	args := map[string]any{"op": "grant", "repo": repo, "action": "minor-release", "actor": "owner", "evidence": "purported owner's words"}
	call(withMCPOrigin(context.Background(), mcpOrigin{Seat: "jv-t1270-worker"}), args)
	check := func() map[string]any {
		t.Helper()
		r := call(context.Background(), map[string]any{"repo": repo, "action": "minor-release"})
		out := map[string]any{}
		if err := json.Unmarshal([]byte(ideaToolText(r)), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := check(); got["authorized"] != false || got["record"].(map[string]any)["recorded_by"] != "jv-t1270-worker" {
		t.Fatalf("worker grant not pending: %v", got)
	}
	questions, err := ownerquestionview.New(state).List(true)
	if err != nil || len(questions) != 1 {
		t.Fatalf("owner question: %v %v", questions, err)
	}
	// A worker cannot rubber-stamp its own question by claiming actor=owner.
	denied, err := s.handleOwnerQuestions(withMCPOrigin(context.Background(), mcpOrigin{Seat: "jv-t1270-worker"}), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"op": "answer", "repo": repo, "target": questions[0].Identity.Target, "id": questions[0].Identity.ID, "version": questions[0].Identity.Version, "answer": "confirm", "actor": "owner"}}})
	if err != nil || !denied.IsError || check()["authorized"] != false {
		t.Fatalf("worker self-confirmed: %v %+v", err, denied)
	}
	// The owner-question answer route is the production transition, not a
	// direct test mutation of the release authorization file.
	if _, err := s.AnswerOwnerQuestionIdentity(questions[0].Identity, "confirm", "owner"); err != nil {
		t.Fatal(err)
	}
	if got := check(); got["authorized"] != true {
		t.Fatalf("confirmed grant denied: %v", got)
	}
}

func TestReleaseAuthorizationLegacyGrantRemainsValid(t *testing.T) {
	state, repo := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0700)
	s := New(repo, nil, nil)
	s.SetOwnerQuestionsDir(state)
	shared, err := ownerquestion.SharedRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(shared))
	dir := filepath.Join(state, "release-authorizations")
	os.MkdirAll(dir, 0700)
	old := map[string]releaseGrant{"minor-release": {Repo: shared, Action: "minor-release", By: "owner", Evidence: "owner endorsed 2026-10-28"}}
	b, _ := json.Marshal(old)
	os.WriteFile(filepath.Join(dir, hex.EncodeToString(digest[:])+".json"), b, 0600)
	r, e := s.handleReleaseAuthorization(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"repo": repo, "action": "minor-release"}}})
	if e != nil || r.IsError {
		t.Fatalf("%v %+v", e, r)
	}
	out := map[string]any{}
	json.Unmarshal([]byte(ideaToolText(r)), &out)
	if out["authorized"] != true {
		t.Fatalf("legacy grant denied: %v", out)
	}
}

func TestReleaseAuthorizationUnidentifiedCallerPending(t *testing.T) {
	state, repo := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	s := New(repo, nil, nil)
	s.SetOwnerQuestionsDir(state)
	reg, err := claudia.NewRegistry(filepath.Join(state, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jevons", Role: "overseer", Purpose: claudia.PurposeOverseer, SessionID: "session-overseer", WorkDir: repo}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jv-t1270-worker", Role: "worker", SessionID: "session-worker", WorkDir: repo}); err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	// No X-Jevons-Agent seat, no matching session id: not an owner channel.
	grant, e := s.handleReleaseAuthorization(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"op": "grant", "repo": repo, "action": "minor-release", "actor": "owner", "evidence": "purported owner's words"}}})
	if e != nil || grant.IsError {
		t.Fatalf("grant: %v %+v", e, grant)
	}
	r, e := s.handleReleaseAuthorization(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"repo": repo, "action": "minor-release"}}})
	if e != nil || r.IsError {
		t.Fatalf("check: %v %+v", e, r)
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(ideaToolText(r)), &out); err != nil {
		t.Fatal(err)
	}
	rec, _ := out["record"].(map[string]any)
	if out["authorized"] != false {
		t.Fatalf("unidentified grant authorized: %v", out)
	}
	if rec == nil || rec["pending"] != true {
		t.Fatalf("unidentified grant not pending: %v", out)
	}
	if rec["recorded_by"] == "owner-session" || rec["recorded_by"] == "" {
		t.Fatalf("unidentified caller fell through to owner-session: %v", rec)
	}
}
