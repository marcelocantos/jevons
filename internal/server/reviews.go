// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

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
	ID          string                     `json:"id"`
	URL         string                     `json:"url"`
	Identity    ownerquestionview.Identity `json:"identity"`
	Question    string                     `json:"question"`
	Asker       string                     `json:"asker"`
	AnswerRoute string                     `json:"answer_route"`
	State       ownerquestionview.State    `json:"state"`
	Readiness   string                     `json:"readiness"`
	Resolution  string                     `json:"resolution,omitempty"`
}

func reviewFromQuestion(q ownerquestionview.Question) reviewItem {
	id := reviewID(q.Identity)
	// The typed owner-gate event is produced only after code is landed and a
	// gate was recorded. Other typed owner questions may still depend on a
	// device or another prerequisite; until the producer explicitly proves
	// readiness, never invite the owner to press Approve on those.
	readiness := "prerequisite_blocked"
	if q.State == ownerquestionview.Open && q.Identity.ID == "owner-gate" {
		readiness = "actionable"
	}
	if q.State != ownerquestionview.Open {
		readiness = "closed"
	}
	return reviewItem{ID: id, URL: "/review/" + id, Identity: q.Identity, Question: q.Text, Asker: q.Asker, AnswerRoute: q.AnswerRoute, State: q.State, Readiness: readiness, Resolution: q.Resolution}
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
				_ = json.NewEncoder(w).Encode(reviewFromQuestion(q))
				return
			}
		}
		writeJSONError(w, http.StatusNotFound, "review not found")
		return
	}
	out := make([]reviewItem, 0, len(rows))
	for _, q := range rows {
		if q.State == ownerquestionview.Open || strings.EqualFold(r.URL.Query().Get("include_closed"), "true") {
			out = append(out, reviewFromQuestion(q))
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

// A same-origin POST, a loopback connection, and even a device certificate
// provisioned by /api/provision do NOT establish that the actor is the owner.
// The product has no owner-authenticated HTTP principal yet. Refuse *all*
// answer mutations rather than laundering an unauthenticated verdict into
// owner_gate or a PO thread. The read API remains useful to mobile deep links.
func (s *Server) answerReview(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusForbidden, "owner-authenticated review answers are not configured")
}
