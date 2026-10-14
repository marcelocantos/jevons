package mcpserver

import (
	"context"
	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
	"github.com/mark3labs/mcp-go/mcp"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOwnerQuestionsMCPRestart(t *testing.T) {
	state := t.TempDir()
	repo := t.TempDir()
	if err := os.Mkdir(repo+"/.git", 0700); err != nil {
		t.Fatal(err)
	}
	s := New(repo, nil, nil)
	s.SetOwnerQuestionsDir(state)
	call := func(s *Server, args map[string]any) string {
		t.Helper()
		r, e := s.handleOwnerQuestions(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
		if e != nil || r.IsError {
			t.Fatalf("call %v: %v %+v", args, e, r)
		}
		return ideaToolText(r)
	}
	id := map[string]any{"repo": repo, "target": "T44.3", "id": "clause-5", "version": "v1"}
	rec := map[string]any{}
	for k, v := range id {
		rec[k] = v
	}
	rec["op"] = "record"
	rec["text"] = "A or B?"
	rec["asker"] = "po"
	rec["answer_route"] = "reply to po"
	call(s, rec)
	cold := New(repo, nil, nil)
	cold.SetOwnerQuestionsDir(state)
	if got := call(cold, map[string]any{}); !strings.Contains(got, "T44.3") || !strings.Contains(got, "open") {
		t.Fatal(got)
	}
	done := map[string]any{}
	for k, v := range id {
		done[k] = v
	}
	done["op"] = "resolve"
	done["state"] = "moot"
	done["note"] = "scope retired"
	call(cold, done)
	if got := call(s, map[string]any{}); strings.Contains(got, "T44.3") {
		t.Fatalf("resolved row stale-open: %s", got)
	}
	if got := call(s, map[string]any{"op": "all"}); !strings.Contains(got, "T44.3") || !strings.Contains(got, "moot") {
		t.Fatal(got)
	}
}

func TestOwnerQuestionViewShowsSpoolNotDeliveryAndResolution(t *testing.T) {
	state := t.TempDir()
	repo := t.TempDir()
	s := New(repo, nil, nil)
	s.SetOwnerQuestionsDir(state)
	q := ownerquestion.Question{Identity: ownerquestion.Identity{Repo: repo, Target: "T45", ID: "effect-lattice", Version: "v1"}, Text: "Option A?", Asker: "arrai-po", AnswerRoute: "reply to arrai-po", State: ownerquestion.Open}
	if err := s.recordOwnerQuestion(q); err != nil {
		t.Fatal(err)
	}
	out := ownerquestions.New(state)
	out.Send = func(_, _, _, _ string) (string, error) { return "spool/ack", nil }
	if _, err := out.Observe(ownerquestions.Question{Key: repo + "#T45#effect-lattice", Text: q.Text}, time.Now()); err != nil {
		t.Fatal(err)
	}
	r, err := s.handleOwnerQuestions(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{}}})
	if err != nil || r.IsError {
		t.Fatalf("list %v %+v", err, r)
	}
	got := ideaToolText(r)
	if !strings.Contains(got, `"notification": "spooled"`) || !strings.Contains(got, `"delivered": false`) || !strings.Contains(got, `"spool_path": "spool/ack"`) {
		t.Fatalf("wrong transport semantics: %s", got)
	}
	if err := out.Resolve(repo + "#T45#effect-lattice"); err != nil {
		t.Fatal(err)
	}
	if err := ownerquestionview.New(state).Resolve(ownerquestionview.Identity{Repo: repo, Target: "T45", ID: "effect-lattice", Version: "v1"}, ownerquestionview.Answered, "owner chose conservative A"); err != nil {
		t.Fatal(err)
	}
	if err := s.recordOwnerQuestion(q); err != nil {
		t.Fatal(err)
	}
	open, err := ownerquestionview.New(state).List(true)
	if err != nil || len(open) != 0 {
		t.Fatalf("replayed report reopened answer: %+v %v", open, err)
	}
}
