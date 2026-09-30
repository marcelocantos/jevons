// Package planstep implements 🎯T254.3: targets can carry ordered plan
// steps that agents walk and resume across restarts.
//
// A Plan is an ordered list of Steps belonging to a bullseye target. Steps
// are worked strictly in order: at most one step is ever "in_progress" at a
// time, and ClaimNext only ever hands out the first pending step once every
// earlier step is done. If a step is already in_progress when ClaimNext is
// called — including after a process restart, since the Store persists to
// disk — ClaimNext returns that SAME step again rather than minting a novel
// claim, so a resumed implementer picks up exactly where it left off
// without a fresh owner brief.
package planstep

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StepStatus is the lifecycle state of a single plan step.
type StepStatus string

const (
	StepPending    StepStatus = "pending"
	StepInProgress StepStatus = "in_progress"
	StepDone       StepStatus = "done"
)

// Step is one ordered unit of work within a target's plan.
type Step struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Acceptance string     `json:"acceptance"`
	Status     StepStatus `json:"status"`
	ClaimedBy  string     `json:"claimed_by,omitempty"`
	ClaimedAt  time.Time  `json:"claimed_at,omitempty"`
	DoneAt     time.Time  `json:"done_at,omitempty"`
}

// Plan is the ordered step list for one target.
type Plan struct {
	TargetID string `json:"target_id"`
	Steps    []Step `json:"steps"`
}

// ErrNoPlan is returned when a target has no defined plan.
var ErrNoPlan = errors.New("planstep: no plan for target")

// ErrUnknownStep is returned when a step id does not exist in the plan.
var ErrUnknownStep = errors.New("planstep: unknown step id")

// ErrPlanExists is returned by SetPlan when a plan already exists for the
// target and overwrite was not requested — plans are defined once, not
// silently replaced out from under an in-progress implementer.
var ErrPlanExists = errors.New("planstep: plan already exists for target")

// Store persists plans durably under a directory, one JSON file per
// target, so state survives process restarts (🎯T254.3).
type Store struct {
	dir string
	mu  sync.Mutex
}

// NewStore returns a Store rooted at dir, creating dir if needed.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("planstep: mkdir %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(targetID string) string {
	return filepath.Join(s.dir, targetID+".json")
}

func (s *Store) load(targetID string) (*Plan, error) {
	b, err := os.ReadFile(s.path(targetID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoPlan
		}
		return nil, err
	}
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) save(p *Plan) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(p.TargetID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(p.TargetID))
}

// SetPlan defines the ordered steps for a target. It refuses to overwrite
// an existing plan unless overwrite is true, so a re-brief cannot silently
// discard progress on steps already claimed or done.
func (s *Store) SetPlan(targetID string, steps []Step, overwrite bool) (*Plan, error) {
	if targetID == "" {
		return nil, errors.New("planstep: empty target id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if !overwrite {
		if _, err := s.load(targetID); err == nil {
			return nil, ErrPlanExists
		} else if !errors.Is(err, ErrNoPlan) {
			return nil, err
		}
	}

	normalized := make([]Step, len(steps))
	for i, st := range steps {
		st.Status = StepPending
		st.ClaimedBy = ""
		st.ClaimedAt = time.Time{}
		st.DoneAt = time.Time{}
		if st.ID == "" {
			st.ID = fmt.Sprintf("%s.step%d", targetID, i+1)
		}
		normalized[i] = st
	}
	p := &Plan{TargetID: targetID, Steps: normalized}
	if err := s.save(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Load returns the current plan for a target, reading it fresh from disk —
// this is the "restart" read path: a new Store pointed at the same dir
// sees exactly the state the previous process left behind.
func (s *Store) Load(targetID string) (*Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(targetID)
}

// ClaimNext returns the step an implementer should work on right now.
//
// If a step is already in_progress, that SAME step is returned again
// (idempotent resume — 🎯T254.3's restart requirement) regardless of
// claimant, rather than minting a new claim; the caller can tell it is a
// resume because the returned step's ClaimedBy may differ from claimant.
// Otherwise the first pending step (in order) is claimed and returned.
// Returns (nil, nil) when every step is done.
func (s *Store) ClaimNext(targetID, claimant string) (*Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.load(targetID)
	if err != nil {
		return nil, err
	}

	for i := range p.Steps {
		switch p.Steps[i].Status {
		case StepInProgress:
			step := p.Steps[i]
			return &step, nil
		case StepPending:
			p.Steps[i].Status = StepInProgress
			p.Steps[i].ClaimedBy = claimant
			p.Steps[i].ClaimedAt = time.Now().UTC()
			if err := s.save(p); err != nil {
				return nil, err
			}
			step := p.Steps[i]
			return &step, nil
		case StepDone:
			continue
		}
	}
	// Every step done, or plan empty: nothing left to claim.
	return nil, nil
}

// CompleteStep marks a step done and persists, unblocking the next pending
// step for a future ClaimNext call. Refuses to complete a step that is not
// currently in_progress (guards against completing out of order or twice).
func (s *Store) CompleteStep(targetID, stepID string) (*Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.load(targetID)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range p.Steps {
		if p.Steps[i].ID != stepID {
			continue
		}
		found = true
		if p.Steps[i].Status != StepInProgress {
			return nil, fmt.Errorf("planstep: step %s is %s, not in_progress", stepID, p.Steps[i].Status)
		}
		p.Steps[i].Status = StepDone
		p.Steps[i].DoneAt = time.Now().UTC()
	}
	if !found {
		return nil, ErrUnknownStep
	}
	if err := s.save(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Progress reports counts of done/total steps and whether the plan is
// fully walked.
func (p *Plan) Progress() (done, total int, complete bool) {
	total = len(p.Steps)
	for _, st := range p.Steps {
		if st.Status == StepDone {
			done++
		}
	}
	return done, total, total > 0 && done == total
}
