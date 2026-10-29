// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// reviewID binds a deep link to the entire durable question identity. It is
// opaque to the client, but not an authorization token. A revised question
// gets a different URL and the old URL remains inspectable as superseded.
func reviewID(id ownerquestionview.Identity) string {
	b, _ := json.Marshal(id)
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type reviewItem struct {
	SchemaVersion int            `json:"schema_version"`
	TargetTitle   string         `json:"target_title,omitempty"`
	TargetStatus  string         `json:"target_status,omitempty"`
	TargetLookup  string         `json:"target_lookup"`
	Evidence      reviewEvidence `json:"evidence"`
	ID            string         `json:"id"`
	URL           string         `json:"url"`
	Repository    string         `json:"repository"`
	Target        string         `json:"target"`
	AskID         string         `json:"ask_id"`
	Version       string         `json:"version"`
	Question      string         `json:"question"`
	Asker         string         `json:"asker"`
	AnswerRoute   string         `json:"answer_route"`
	// DefaultOnSilence and Options are the 🎯T404 question contract: what the
	// asker does if never answered, and the closed-form choices if any.
	DefaultOnSilence string                  `json:"default_on_silence"`
	Options          []string                `json:"options,omitempty"`
	State            ownerquestionview.State `json:"state"`
	Readiness        string                  `json:"readiness"`
	Prerequisite     string                  `json:"prerequisite,omitempty"`
	Action           string                  `json:"action,omitempty"`
	EvidenceStatus   string                  `json:"evidence_status,omitempty"`
	Resolution       string                  `json:"resolution,omitempty"`
}

// A review is trustworthy only when its embedded identity names the exact
// durable question. A mismatched or legacy record must not solicit a verdict.
func matchingReview(q ownerquestionview.Question) bool {
	if q.Review == nil {
		return false
	}
	return q.Review.Identity == (ownerquestion.Identity{Repo: q.Identity.Repo, Target: q.Identity.Target, ID: q.Identity.ID, Version: q.Identity.Version})
}

func reviewFromQuestion(q ownerquestionview.Question) reviewItem {
	// Owner-question prose is not a trusted URL or path field. In particular,
	// keep the canonical repository root internal even if it was quoted in a
	// question or resolution. The repository display label is separate.
	redact := func(text string) string { return redactReviewPaths(text, q.Identity.Repo) }
	trustedReview := matchingReview(q)
	id := reviewID(q.Identity)
	// Readiness is a producer-owned typed fact, never a classifier over the
	// question ID or prose. Older records without a Review stay non-actionable.
	readiness := "unspecified"
	if trustedReview {
		readiness = string(q.Review.Readiness)
	}
	if trustedReview && q.Review.Readiness == ownerquestion.Actionable {
		readiness = "actionable"
	}
	if q.State != ownerquestionview.Open {
		readiness = "closed"
	}
	item := reviewItem{SchemaVersion: 2, TargetLookup: "missing", Evidence: emptyReviewEvidence(), ID: id, URL: "/api/reviews/" + id, Repository: filepath.Base(q.Identity.Repo), Target: q.Identity.Target, AskID: q.Identity.ID, Version: q.Identity.Version, Question: redact(q.Text), Asker: redact(q.Asker), AnswerRoute: redact(q.AnswerRoute), DefaultOnSilence: redact(q.SilenceOutcome()), State: q.State, Readiness: readiness, Resolution: redact(q.Resolution)}
	for _, o := range q.Options {
		item.Options = append(item.Options, redact(o))
	}
	if q.Review != nil && !trustedReview {
		// The prose may contain reported artifact paths. A corrupt nested
		// review is not safe to render as an ordinary owner question.
		item.Question = "Review record unavailable"
		item.AnswerRoute = ""
	}
	if trustedReview {
		// Producer Question() embeds reported artifact paths. Present only the
		// typed action; the artifact has no verified download URL yet.
		item.Question = redact(q.Review.Action)
		item.Prerequisite = redact(q.Review.Prerequisite)
		item.Action = redact(q.Review.Action)
		// T1042 records reported local artifact references, not verified
		// downloadable URLs. Never present them as inspected evidence.
		item.EvidenceStatus = "reported-unverified"
	}
	return item
}

func (s *Server) reviews(w http.ResponseWriter, r *http.Request) {
	rows, err := ownerquestionview.New(s.stateDir).List(false)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "review index unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if id := r.PathValue("id"); id != "" {
		for _, q := range rows {
			if reviewID(q.Identity) == id {
				_ = json.NewEncoder(w).Encode(s.reviewDetail(q))
				return
			}
		}
		writeJSONError(w, http.StatusNotFound, "review not found")
		return
	}
	includeClosed := strings.EqualFold(r.URL.Query().Get("include_closed"), "true")
	out := make([]reviewItem, 0, len(rows))
	for _, q := range rows {
		if q.State != ownerquestionview.Open && !includeClosed {
			continue
		}
		item := s.reviewDetail(q)
		// A question about a target the ledger already closed is no longer a
		// decision the owner must make (🎯T1122). It stays readable by id and
		// under include_closed.
		if !includeClosed && item.TargetLookup == "verified" && isDoneStatus(item.TargetStatus) {
			continue
		}
		out = append(out, item)
	}
	_ = json.NewEncoder(w).Encode(out)
}

// A same-origin POST, a loopback connection, and even a device certificate
// provisioned by /api/provision do NOT establish that the actor is the owner.
// The product has no owner-authenticated HTTP principal yet. Refuse *all*
// answer mutations rather than laundering an unauthenticated verdict into
// owner_gate or a PO thread. The read API remains useful to mobile deep links.
func (s *Server) answerReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.authorizeOwnerSurfaceHTTP(r, "owner", ""); err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}
	s.mu.RLock()
	hook := s.ownerQuestionAnswerHook
	s.mu.RUnlock()
	if hook == nil {
		writeJSONError(w, http.StatusForbidden, "owner-authenticated review answers are not configured")
		return
	}
	reviewIDParam := strings.TrimSpace(r.PathValue("id"))
	if reviewIDParam == "" {
		writeJSONError(w, http.StatusBadRequest, "review id required")
		return
	}
	var body struct {
		Answer string `json:"answer"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	answer := strings.TrimSpace(body.Answer)
	if answer == "" {
		answer = strings.TrimSpace(body.Action)
	}
	if answer == "" {
		writeJSONError(w, http.StatusBadRequest, "answer is required")
		return
	}
	rows, err := ownerquestionview.New(s.stateDir).List(false)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "review index unavailable")
		return
	}
	var identity ownerquestionview.Identity
	for _, q := range rows {
		if reviewID(q.Identity) == reviewIDParam {
			identity = q.Identity
			break
		}
	}
	if identity.Repo == "" {
		writeJSONError(w, http.StatusNotFound, "review not found")
		return
	}
	routed, err := hook(identity, answer)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "answered", "routed": routed})
}

// SetOwnerQuestionAnswerHook wires cockpit POST /api/reviews/{id}/answer to the
// daemon's durable answer + route-back path (🎯T404.1). Nil keeps fail-closed.
func (s *Server) SetOwnerQuestionAnswerHook(fn func(id ownerquestionview.Identity, answer string) (routed string, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ownerQuestionAnswerHook = fn
}
