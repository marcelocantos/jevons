// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import "regexp"

// ErrNotFound is the distinguishable token a supervisor gets when an
// attestation cites an id with no record behind it (🎯T697). It is a word
// rather than a generic 404 body so a fabricated or mistyped id cannot be
// mistaken for an unreadably-shaped pass.
const ErrNotFound = "not-found"

// recordIDRe is the id alphabet an attestation may cite. Anything outside it
// is not a handle into the store — including path fragments that would walk
// out of ~/.jevons/gates.
var recordIDRe = regexp.MustCompile(`^[0-9a-zA-Z]+$`)

// ValidRecordID reports whether id is a legal gate-record handle.
func ValidRecordID(id string) bool {
	return id != "" && recordIDRe.MatchString(id)
}

// View is the supervisor-facing projection of a Record (🎯T697): the
// command that ran, the status it actually exited with, the verdict a
// worker may cite, and the tree it measured. It is what /api/gates and
// jevons_gate_show return, so a session without filesystem access to the
// store or permission to run bin/gate can still check an attestation.
type View struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Command     []string        `json:"command"`
	Dir         string          `json:"dir,omitempty"`
	ExitStatus  int             `json:"exit_status"`
	StatusKnown bool            `json:"status_known"`
	Status      string          `json:"status"`
	Verdict     Verdict         `json:"verdict"`
	Tree        *TreeProvenance `json:"tree,omitempty"`
	Attestation string          `json:"attestation"`
	VoidReason  string          `json:"void_reason,omitempty"`
	Found       bool            `json:"found"`
}

// ViewOf projects rec for a supervisor. Nil rec is not a view — callers
// that missed the store use NotFoundView instead of this.
func ViewOf(rec *Record) View {
	if rec == nil {
		return View{}
	}
	return View{
		ID:          rec.ID,
		Name:        rec.Name,
		Command:     rec.Command,
		Dir:         rec.Dir,
		ExitStatus:  rec.ExitStatus,
		StatusKnown: rec.StatusKnown,
		Status:      rec.Status(),
		Verdict:     rec.Verdict,
		Tree:        rec.Tree,
		Attestation: rec.Attestation(),
		VoidReason:  rec.VoidReason,
		Found:       true,
	}
}

// NotFoundView is the body for an id with no record. Found is false so a
// reader that ignores HTTP status still cannot treat it as a pass.
type NotFoundView struct {
	Error string `json:"error"`
	ID    string `json:"id"`
	Found bool   `json:"found"`
}

// NewNotFoundView names the missing id.
func NewNotFoundView(id string) NotFoundView {
	return NotFoundView{Error: ErrNotFound, ID: id, Found: false}
}
