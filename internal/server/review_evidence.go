// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// Review evidence is an index of claims, not an artifact-reading capability.
// No untrusted git diff, stored report body or local file is served as a URL.
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
var reviewShot = regexp.MustCompile(`(?i)artifacts/[a-zA-Z0-9_./-]+\.(?:png|jpe?g)`)
var reviewReport = regexp.MustCompile(`\breport[ -]handle[=: ]+([a-zA-Z0-9_.-]+)/([0-9]{8}T[0-9]{6}Z-[a-f0-9]{8}(?:-[0-9]+)?)\b`)

// Distinct references in a producer's typed evidence must be unambiguous;
// never pair the first arbitrary SHA and gate id from unrelated prose.
func uniqueReference(matches []string) string {
	if len(matches) == 0 {
		return ""
	}
	first := strings.ToLower(matches[0])
	for _, v := range matches[1:] {
		if strings.ToLower(v) != first {
			return ""
		}
	}
	return first
}
func uniqueGate(text string) string {
	matches := reviewGate.FindAllStringSubmatch(text, -1)
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m[1])
	}
	return uniqueReference(ids)
}

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
		row, found, e := computeTargetFromLedger(ledger, q.Identity.Target)
		if e != nil {
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
	// Untyped legacy questions may mention a SHA or path in their prose, but
	// cannot bind any of them to a producer evidence claim.
	if !matchingReview(q) {
		return item
	}
	text := q.Review.Evidence
	sha := uniqueReference(reviewSHA.FindAllString(text, -1))
	id := uniqueGate(text)
	if sha != "" {
		item.Evidence.Commit = reviewArtifact{Status: "reported_only", Reference: sha}
		item.Evidence.Diff.Status = "reported_only"
	}
	if id != "" {
		item.Evidence.Gate = reviewArtifact{Status: "reported_only", Reference: id}
	}
	// The verified claim is the PAIR, not two independent successes. A gate
	// must be green, clean, from the same shared repo, measured at this exact
	// cited SHA, which must be reachable from this repository's HEAD.
	if sha != "" && id != "" {
		if store, e := gate.OpenStore(""); e == nil {
			if rec, ok := store.Lookup(id); ok && rec != nil {
				item.Evidence.Gate.Verdict = string(rec.Verdict)
				measured, repoErr := ownerquestion.SharedRepo(gate.MeasuredRepo(rec))
				if rec.Tree != nil && repoErr == nil && measured == repo && rec.Tree.Commit == sha && rec.Tree.Clean && rec.Verdict == gate.VerdictGreen && rec.StatusKnown && rec.ExitStatus == 0 && gate.ScopeOf(rec, repo).Verdict == gate.ScopeOwn {
					if exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", sha, "HEAD").Run() == nil {
						item.Evidence.Commit.Status = "verified"
						item.Evidence.Gate.Status = "verified"
					}
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
	// A diff's content cannot be certified safe to display from a reference
	// alone. Keep the link absent even for a verified commit.
	if m := reviewReport.FindStringSubmatch(text); len(m) > 2 {
		item.Evidence.Report = reviewArtifact{Status: "reported_only", Reference: m[1] + "/" + m[2]}
		if rec, e := agentreport.Load(s.stateDir, m[1], m[2]); e != nil || rec.ID != m[2] || rec.Agent != m[1] || !strings.Contains(rec.Text, "jevons: target "+q.Identity.Target+"\n") {
			item.Evidence.Report.Status = "inaccessible"
		}
	}
	for _, path := range reviewShot.FindAllString(text, -1) {
		item.Evidence.Screenshots = append(item.Evidence.Screenshots, reviewArtifact{Status: "reported_only", Reference: filepath.Base(path)})
	}
	return item
}

// Human prose may contain filesystem paths or secrets. Redact the *whole*
// token containing an absolute path, including --config=/tmp/x,
// file="/tmp/x", and URL query assignments; never expose raw report/diff.
var reviewLocalPath = regexp.MustCompile(`[^\s\x60,;]*?(?:/[A-Za-z0-9_.-]+){2,}[^\s\x60,;]*|[^\s\x60,;]*[A-Za-z]:\\[^\s\x60,;]+`)

func redactReviewPaths(text, repo string) string {
	text = strings.ReplaceAll(text, repo, "[repository]")
	return reviewLocalPath.ReplaceAllStringFunc(text, func(token string) string {
		// URLs may be safe navigation, but their query can embed private paths.
		// Fail closed for any path-like token; no URL is authored from prose.
		return "[local path]"
	})
}
