// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// A status describes what the server actually inspected, not what prose claimed.
// A URL exists only when its target is served by this daemon.
type reviewArtifact struct {
	Status    string `json:"status"` // missing, reported_only, inaccessible, verified
	Reference string `json:"reference,omitempty"`
	URL       string `json:"url,omitempty"`
	Verdict   string `json:"verdict,omitempty"`
}
type reviewEvidence struct {
	Commit      reviewArtifact   `json:"commit"`
	Gate        reviewArtifact   `json:"gate"`
	Diff        reviewArtifact   `json:"diff"`
	Report      reviewArtifact   `json:"report"`
	Screenshots []reviewArtifact `json:"screenshots"`
}

func emptyReviewEvidence() reviewEvidence {
	return reviewEvidence{Commit: reviewArtifact{Status: "missing"}, Gate: reviewArtifact{Status: "missing"}, Diff: reviewArtifact{Status: "missing"}, Report: reviewArtifact{Status: "missing"}, Screenshots: []reviewArtifact{}}
}

var reviewSHA = regexp.MustCompile(`\b[0-9a-fA-F]{40}\b`)
var reviewGate = regexp.MustCompile(`\b(?:gate-id[=: ]+|GATE\s+[^\n]*?\bid=)([0-9a-f]{8})\b`)
var reviewReport = regexp.MustCompile(`\breport[ -]handle[=: ]+([a-zA-Z0-9_.-]+)/([0-9]{8}T[0-9]{6}Z-[a-f0-9]{8}(?:-[0-9]+)?)\b`)
var reviewShot = regexp.MustCompile(`(?i)artifacts/[a-zA-Z0-9_./-]+\.(?:png|jpe?g)`)

// reviewDetail does not use the caller's cwd or an ambiguous repository label.
// The question's canonical repo is the only authority for target and git reads.
func (s *Server) reviewDetail(q ownerquestionview.Question) reviewItem {
	item := reviewFromQuestion(q)
	repo, err := ownerquestion.SharedRepo(q.Identity.Repo)
	if err != nil || repo != q.Identity.Repo {
		item.TargetLookup = "inaccessible"
		return item
	}
	ledger, absent, err := cachedLedgerPath(repo)
	if err != nil {
		item.TargetLookup = "inaccessible"
	} else if !absent {
		row, found, readErr := computeTargetFromLedger(ledger, q.Identity.Target)
		if readErr != nil {
			item.TargetLookup = "inaccessible"
		} else if found {
			item.TargetLookup = "verified"
			item.TargetTitle = row.Name
			item.TargetStatus = row.Status
		}
	}
	if q.Review != nil && !matchingReview(q) {
		return item
	}
	// Producer evidence is reported, never inferred from an unrelated target's
	// attestation. A legacy question may contain a citation, but is not proof.
	text := q.Text
	if matchingReview(q) {
		text = q.Review.Evidence + "\n" + q.Text
	}
	if sha := reviewSHA.FindString(text); sha != "" {
		sha = strings.ToLower(sha)
		item.Evidence.Commit = reviewArtifact{Status: "reported_only", Reference: sha}
		cmd := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", sha, "HEAD")
		if err := cmd.Run(); err == nil {
			item.Evidence.Commit.Status = "verified"
			item.Evidence.Diff = reviewArtifact{Status: "verified", URL: item.URL + "/diff"}
		} else {
			item.Evidence.Commit.Status = "inaccessible"
			item.Evidence.Diff.Status = "inaccessible"
		}
	}
	if m := reviewGate.FindStringSubmatch(text); len(m) > 1 {
		id := m[1]
		item.Evidence.Gate = reviewArtifact{Status: "reported_only", Reference: id}
		if store, err := gate.OpenStore(""); err == nil {
			if rec, ok := store.Lookup(id); ok && rec != nil {
				// A gate is scoped by BOTH the measured repo and commit reachability.
				// Never treat a store-global ID from another checkout as this one's.
				measured, e := ownerquestion.SharedRepo(gate.MeasuredRepo(rec))
				if e == nil && measured == repo && gate.ScopeOf(rec, repo).Verdict == gate.ScopeOwn {
					item.Evidence.Gate.Status = "verified"
					item.Evidence.Gate.URL = item.URL + "/gate"
					item.Evidence.Gate.Verdict = string(rec.Verdict)
				} else {
					item.Evidence.Gate.Status = "inaccessible"
				}
			} else {
				item.Evidence.Gate.Status = "inaccessible"
			}
		} else {
			item.Evidence.Gate.Status = "inaccessible"
		}
	}
	if m := reviewReport.FindStringSubmatch(text); len(m) > 2 {
		item.Evidence.Report = reviewArtifact{Status: "reported_only", Reference: m[1] + "/" + m[2]}
		if rec, err := agentreport.Load(s.stateDir, m[1], m[2]); err == nil && rec.ID == m[2] && rec.Agent == m[1] && strings.Contains(rec.Text, "jevons: target "+q.Identity.Target) {
			item.Evidence.Report.Status = "verified"
			item.Evidence.Report.URL = item.URL + "/report"
		} else {
			item.Evidence.Report.Status = "inaccessible"
		}
	}
	// A reported local file path is NEVER a URL. The basename is enough to
	// explain what is missing without disclosing the producer's filesystem.
	for _, path := range reviewShot.FindAllString(text, -1) {
		item.Evidence.Screenshots = append(item.Evidence.Screenshots, reviewArtifact{Status: "reported_only", Reference: filepath.Base(path)})
	}
	return item
}

// The diff endpoint resolves the exact version before reading git. No client
// supplied path or SHA reaches git; cap output to prevent unbounded responses.
func (s *Server) reviewDiff(w http.ResponseWriter, r *http.Request) {
	rows, err := ownerquestionview.New(s.stateDir).List(false)
	if err != nil {
		http.Error(w, "review index unavailable", 500)
		return
	}
	for _, q := range rows {
		if reviewID(q.Identity) != r.PathValue("id") {
			continue
		}
		item := s.reviewDetail(q)
		if item.Evidence.Diff.Status != "verified" {
			http.NotFound(w, r)
			return
		}
		sha := item.Evidence.Commit.Reference
		cmd := exec.Command("git", "-C", q.Identity.Repo, "show", "--format=", "--no-ext-diff", sha, "--")
		out, e := cmd.Output()
		if e != nil || len(out) > 1024*1024 {
			http.Error(w, "diff unavailable", http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprint(w, redactReviewPaths(string(out), q.Identity.Repo))
		return
	}
	http.NotFound(w, r)
}

// A report handle is only served when the exact review version explicitly
// cites it and the stored report itself names that target.
func (s *Server) reviewReport(w http.ResponseWriter, r *http.Request) {
	rows, err := ownerquestionview.New(s.stateDir).List(false)
	if err != nil {
		http.Error(w, "review index unavailable", 500)
		return
	}
	for _, q := range rows {
		if reviewID(q.Identity) != r.PathValue("id") {
			continue
		}
		item := s.reviewDetail(q)
		if item.Evidence.Report.Status != "verified" {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(item.Evidence.Report.Reference, "/")
		rec, e := agentreport.Load(s.stateDir, parts[0], parts[1])
		if e != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = fmt.Fprint(w, redactReviewPaths(rec.Text, q.Identity.Repo))
		return
	}
	http.NotFound(w, r)
}

// Review text can quote arbitrary local file paths. Strip them before the
// browser sees them; links remain server-generated and opaque.
var reviewLocalPath = regexp.MustCompile(`(?:^|[\s(\x60])(?:/[^\s)\x60,;]+|[A-Za-z]:\\[^\s)\x60,;]+)`)

func redactReviewPaths(text, repo string) string {
	text = strings.ReplaceAll(text, repo, "[repository]")
	return reviewLocalPath.ReplaceAllStringFunc(text, func(match string) string {
		if len(match) > 0 && (match[0] == ' ' || match[0] == '\n' || match[0] == '\t' || match[0] == '(' || match[0] == '`') {
			return match[:1] + "[local path]"
		}
		return "[local path]"
	})
}

// The global gate API includes a measured filesystem directory. Review links
// expose a scoped, deliberately small record projection instead.
func (s *Server) reviewGateDetail(w http.ResponseWriter, r *http.Request) {
	rows, err := ownerquestionview.New(s.stateDir).List(false)
	if err != nil {
		http.Error(w, "review index unavailable", 500)
		return
	}
	for _, q := range rows {
		if reviewID(q.Identity) != r.PathValue("id") {
			continue
		}
		item := s.reviewDetail(q)
		if item.Evidence.Gate.Status != "verified" {
			http.NotFound(w, r)
			return
		}
		store, e := gate.OpenStore("")
		if e != nil {
			http.NotFound(w, r)
			return
		}
		rec, ok := store.Lookup(item.Evidence.Gate.Reference)
		if !ok || rec == nil || rec.Tree == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			ID      string       `json:"id"`
			Verdict gate.Verdict `json:"verdict"`
			Status  string       `json:"exit_status"`
			Command []string     `json:"command"`
			Commit  string       `json:"measured_commit"`
		}{rec.ID, rec.Verdict, rec.Status(), reviewSafeArgs(rec.Command, q.Identity.Repo), rec.Tree.Commit})
		return
	}
	http.NotFound(w, r)
}

func reviewSafeArgs(args []string, repo string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, redactReviewPaths(arg, repo))
	}
	return out
}
