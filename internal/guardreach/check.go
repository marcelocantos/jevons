// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package guardreach

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Finding is one catalogue check failure.
type Finding struct {
	EntryID string
	Kind    string // missing_production_call | missing_call_site_test | retired_resurrected | empty_catalog_field
	Detail  string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s [%s]: %s", f.EntryID, f.Kind, f.Detail)
}

// CheckRepo walks the catalogue against a repository root.
//
// For StatusEnforced:
//   - every ProductionFiles path exists and names Intermediate
//   - at least one *_test.go under the repo names CallSiteTest
//   - Intermediate's defining production file (when present) names Guard, or
//     a production file that names Intermediate also names Guard
//
// For StatusRetired:
//   - Guard must not appear as a production (non-test) identifier in
//     internal/handover (gone) or as a live MarkDelivered call site. We only
//     pin the historical symbol absence in non-test Go under internal/ and
//     cmd/ outside docs and the catalogue itself.
func CheckRepo(root string) []Finding {
	var findings []Finding
	for _, e := range Catalog() {
		findings = append(findings, checkEntry(root, e)...)
	}
	return findings
}

func checkEntry(root string, e Entry) []Finding {
	var out []Finding
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.Guard) == "" || strings.TrimSpace(e.Status) == "" {
		out = append(out, Finding{EntryID: e.ID, Kind: "empty_catalog_field", Detail: "id/guard/status required"})
		return out
	}
	switch e.Status {
	case StatusEnforced:
		if e.Intermediate == "" || e.CallSiteTest == "" || len(e.ProductionFiles) == 0 {
			out = append(out, Finding{
				EntryID: e.ID,
				Kind:    "empty_catalog_field",
				Detail:  "enforced entries need Intermediate, CallSiteTest, ProductionFiles",
			})
			return out
		}
		called := false
		for _, rel := range e.ProductionFiles {
			body, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				out = append(out, Finding{
					EntryID: e.ID,
					Kind:    "missing_production_call",
					Detail:  fmt.Sprintf("cannot read %s: %v", rel, err),
				})
				continue
			}
			src := string(body)
			if strings.Contains(src, e.Intermediate) {
				called = true
			}
		}
		if !called {
			out = append(out, Finding{
				EntryID: e.ID,
				Kind:    "missing_production_call",
				Detail:  fmt.Sprintf("Intermediate %q not found in ProductionFiles", e.Intermediate),
			})
		}
		// Guard must appear somewhere on the production path (Intermediate
		// file or a file that also names Intermediate).
		if !guardOnProductionPath(root, e) {
			out = append(out, Finding{
				EntryID: e.ID,
				Kind:    "missing_production_call",
				Detail:  fmt.Sprintf("Guard %q not reached alongside Intermediate %q in production files", e.Guard, e.Intermediate),
			})
		}
		if !callSiteTestPresent(root, e.CallSiteTest) {
			out = append(out, Finding{
				EntryID: e.ID,
				Kind:    "missing_call_site_test",
				Detail:  fmt.Sprintf("no *_test.go names CallSiteTest %q", e.CallSiteTest),
			})
		}
	case StatusRetired:
		if hits := retiredHits(root, e.Guard); len(hits) > 0 {
			out = append(out, Finding{
				EntryID: e.ID,
				Kind:    "retired_resurrected",
				Detail:  fmt.Sprintf("retired guard %q appears in production: %s", e.Guard, strings.Join(hits, ", ")),
			})
		}
	case StatusAuditOnly:
		// No hard check — the catalogue presence IS the report.
	default:
		out = append(out, Finding{
			EntryID: e.ID,
			Kind:    "empty_catalog_field",
			Detail:  fmt.Sprintf("unknown status %q", e.Status),
		})
	}
	return out
}

func guardOnProductionPath(root string, e Entry) bool {
	for _, rel := range e.ProductionFiles {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		src := string(body)
		if strings.Contains(src, e.Intermediate) && strings.Contains(src, e.Guard) {
			return true
		}
	}
	// Intermediate may wrap Guard in another file (e.g. delivery_evidence.go
	// calls delivery.RecordPayload). Scan all listed production files for Guard
	// alone once Intermediate was confirmed present elsewhere.
	for _, rel := range e.ProductionFiles {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		if strings.Contains(string(body), e.Guard) {
			return true
		}
	}
	return false
}

func callSiteTestPresent(root, marker string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "ui" ||
				name == "jevons-worktree" || name == "vendor" || name == "_scratchpad" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(body), marker) {
			found = true
		}
		return nil
	})
	return found
}

// retiredHits lists non-test .go files under internal/ and cmd/ that still
// name the retired guard as a live call-ish identifier. Docs, comments in
// guardreach, and bullseye prose are excluded by scope.
func retiredHits(root, guard string) []string {
	var hits []string
	for _, sub := range []string{"internal", "cmd"} {
		base := filepath.Join(root, sub)
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "guardreach" {
					// Catalogue and its tests name the retired symbol on purpose.
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if !strings.Contains(string(body), guard) {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, rel)
			return nil
		})
	}
	return hits
}
