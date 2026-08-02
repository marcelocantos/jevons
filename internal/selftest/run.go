// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package selftest

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RunRequest is the kick payload for self_test.run / POST /api/self_test/run.
type RunRequest struct {
	Pack string `json:"pack"` // pack id, or "all"
	Site Site   `json:"site"` // live | drill | ci
}

// RunResult is one pack report, or a multi-pack envelope.
type RunResult struct {
	// Single is set when one pack was requested.
	Single *Report `json:"report,omitempty"`
	// Reports is set when pack=all (or multiple).
	Reports []Report `json:"reports,omitempty"`
}

// Run executes pack(s) for site. Pack "" or "all" runs every pack allowed
// on that site. Grade is always from measurements.
func Run(ctx context.Context, env *Env, req RunRequest) (*RunResult, error) {
	if env == nil {
		env = &Env{}
	}
	site := req.Site
	if site == "" {
		site = SiteCI
	}
	if !ValidSite(site) {
		return nil, fmt.Errorf("invalid site %q (want live|drill|ci)", site)
	}
	env.Site = site

	packID := strings.TrimSpace(req.Pack)
	if packID == "" || strings.EqualFold(packID, "all") {
		var reports []Report
		for _, p := range defaultRegistry {
			if !siteAllowed(p, site) {
				r := newReport(p.ID(), site)
				reports = append(reports, finishSkip(r, fmt.Sprintf("pack not allowed on site %s", site)))
				continue
			}
			reports = append(reports, runOne(ctx, p, env))
		}
		return &RunResult{Reports: reports}, nil
	}

	p, ok := LookupPack(packID)
	if !ok {
		return nil, fmt.Errorf("unknown pack %q", packID)
	}
	if !siteAllowed(p, site) {
		r := newReport(p.ID(), site)
		return &RunResult{Single: ptr(finishSkip(r, fmt.Sprintf("pack not allowed on site %s", site)))}, nil
	}
	rep := runOne(ctx, p, env)
	return &RunResult{Single: &rep}, nil
}

func runOne(ctx context.Context, p Pack, env *Env) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	// Bound each pack; self-tests must not hang the daemon.
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return p.Run(cctx, env)
}

func ptr(r Report) *Report { return &r }

// Summarize grades a multi-report run for logging.
func Summarize(reports []Report) (pass, fail, skip, errn int) {
	for _, r := range reports {
		switch r.Grade {
		case GradePass:
			pass++
		case GradeFail:
			fail++
		case GradeSkip:
			skip++
		default:
			errn++
		}
	}
	return
}
