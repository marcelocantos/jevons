package planstep

import "testing"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestSetPlanAndClaimInOrder(t *testing.T) {
	s := newTestStore(t)
	_, err := s.SetPlan("T900", []Step{
		{Name: "one", Acceptance: "a1"},
		{Name: "two", Acceptance: "a2"},
		{Name: "three", Acceptance: "a3"},
	}, false)
	if err != nil {
		t.Fatalf("SetPlan: %v", err)
	}

	step, err := s.ClaimNext("T900", "worker-a")
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if step == nil || step.Name != "one" {
		t.Fatalf("expected step 'one' first, got %+v", step)
	}
	if step.Status != StepInProgress {
		t.Fatalf("expected in_progress, got %s", step.Status)
	}

	// Claiming again before completion must NOT skip ahead to step two.
	again, err := s.ClaimNext("T900", "worker-a")
	if err != nil {
		t.Fatalf("ClaimNext (repeat): %v", err)
	}
	if again == nil || again.Name != "one" {
		t.Fatalf("expected still 'one' while in progress, got %+v", again)
	}
}

func TestClaimResumeAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	// Process A: define the plan and claim the first step.
	storeA, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore A: %v", err)
	}
	if _, err := storeA.SetPlan("T254.3", []Step{
		{ID: "s1", Name: "design step graph", Acceptance: "schema exists"},
		{ID: "s2", Name: "claim/resume", Acceptance: "hermetic passes"},
	}, false); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	claimed, err := storeA.ClaimNext("T254.3", "jv-worker-1")
	if err != nil {
		t.Fatalf("ClaimNext A: %v", err)
	}
	if claimed == nil || claimed.ID != "s1" {
		t.Fatalf("expected s1 claimed, got %+v", claimed)
	}

	// "Restart": a brand new Store instance, same directory, simulating a
	// fresh process after a daemon/agent restart. No new SetPlan call —
	// this must resume, not re-brief.
	storeB, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore B: %v", err)
	}
	resumed, err := storeB.ClaimNext("T254.3", "jv-worker-1-resumed")
	if err != nil {
		t.Fatalf("ClaimNext B: %v", err)
	}
	if resumed == nil || resumed.ID != "s1" {
		t.Fatalf("expected resume onto s1 (still in_progress), got %+v", resumed)
	}
	if resumed.Status != StepInProgress {
		t.Fatalf("expected s1 still in_progress after restart, got %s", resumed.Status)
	}
	// The original claimant is preserved — resuming does not silently
	// reassign the step to a different claimant.
	if resumed.ClaimedBy != "jv-worker-1" {
		t.Fatalf("expected original claimant preserved, got %q", resumed.ClaimedBy)
	}

	// Complete s1 via the post-restart store; only then does s2 unblock.
	if _, err := storeB.CompleteStep("T254.3", "s1"); err != nil {
		t.Fatalf("CompleteStep: %v", err)
	}
	next, err := storeB.ClaimNext("T254.3", "jv-worker-1-resumed")
	if err != nil {
		t.Fatalf("ClaimNext after complete: %v", err)
	}
	if next == nil || next.ID != "s2" {
		t.Fatalf("expected s2 to unblock after s1 done, got %+v", next)
	}

	plan, err := storeB.Load("T254.3")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	done, total, complete := plan.Progress()
	if done != 1 || total != 2 || complete {
		t.Fatalf("expected progress 1/2 incomplete, got done=%d total=%d complete=%v", done, total, complete)
	}
}

func TestCompleteStepRefusesOutOfOrder(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SetPlan("T901", []Step{
		{ID: "a", Name: "a"},
		{ID: "b", Name: "b"},
	}, false); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	// b is still pending (not in_progress) — completing it must be refused.
	if _, err := s.CompleteStep("T901", "b"); err == nil {
		t.Fatalf("expected error completing pending step out of order")
	}
	if _, err := s.CompleteStep("T901", "does-not-exist"); !errIsUnknownStep(err) {
		t.Fatalf("expected ErrUnknownStep, got %v", err)
	}
}

func errIsUnknownStep(err error) bool {
	return err == ErrUnknownStep
}

func TestSetPlanRefusesOverwriteByDefault(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SetPlan("T902", []Step{{Name: "one"}}, false); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if _, err := s.SetPlan("T902", []Step{{Name: "one-again"}}, false); err != ErrPlanExists {
		t.Fatalf("expected ErrPlanExists, got %v", err)
	}
	// overwrite=true is allowed (explicit re-plan).
	if _, err := s.SetPlan("T902", []Step{{Name: "replaced"}}, true); err != nil {
		t.Fatalf("SetPlan overwrite: %v", err)
	}
}

func TestClaimNextNoPlan(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.ClaimNext("T-missing", "w"); err != ErrNoPlan {
		t.Fatalf("expected ErrNoPlan, got %v", err)
	}
}

func TestClaimNextAllDone(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SetPlan("T903", []Step{{ID: "only", Name: "only"}}, false); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if _, err := s.ClaimNext("T903", "w"); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if _, err := s.CompleteStep("T903", "only"); err != nil {
		t.Fatalf("CompleteStep: %v", err)
	}
	step, err := s.ClaimNext("T903", "w")
	if err != nil {
		t.Fatalf("ClaimNext after all done: %v", err)
	}
	if step != nil {
		t.Fatalf("expected nil step when plan complete, got %+v", step)
	}
}
