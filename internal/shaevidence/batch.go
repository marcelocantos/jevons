// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package shaevidence

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// CheckMany classifies every SHA in one (or a few) git round-trips: a single
// rev-list of HEAD builds the ancestor set, then cat-file --batch-check
// resolves each citation to a full commit OID (or missing). Used by the
// standing ledger audit so hundreds of citations do not each spawn git.
func CheckMany(dir, headRef string, shas []string) map[string]Reachability {
	out := make(map[string]Reachability, len(shas))
	if len(shas) == 0 {
		return out
	}
	if strings.TrimSpace(headRef) == "" {
		headRef = "HEAD"
	}
	ancestors, err := revListSet(dir, headRef)
	if err != nil {
		// Degrade to per-SHA checks rather than claiming everything missing.
		check := CheckInRepo(dir, headRef)
		for _, sha := range shas {
			out[sha] = check(sha)
		}
		return out
	}
	resolved, err := batchCommitOIDs(dir, shas)
	if err != nil {
		check := CheckInRepo(dir, headRef)
		for _, sha := range shas {
			out[sha] = check(sha)
		}
		return out
	}
	for _, sha := range shas {
		full, ok := resolved[sha]
		if !ok || full == "" {
			out[sha] = Missing
			continue
		}
		if ancestors[full] {
			out[sha] = Ancestor
		} else {
			out[sha] = Rewritten
		}
	}
	return out
}

// ScanLedger is the standing bullseye.yaml walk (🎯T427 acceptance 5): extract
// evidence SHAs from attestation/Achieved prose and report non-ancestors.
func ScanLedger(dir, yamlText string) []Finding {
	cites := ExtractEvidenceSHAs(AttestationBlob(yamlText))
	if len(cites) == 0 {
		return nil
	}
	shas := make([]string, len(cites))
	for i, c := range cites {
		shas[i] = c.SHA
	}
	kinds := CheckMany(dir, "HEAD", shas)
	var out []Finding
	for _, c := range cites {
		k := kinds[c.SHA]
		if k == Ancestor {
			continue
		}
		out = append(out, Finding{
			SHA:    c.SHA,
			Kind:   k,
			Line:   c.Line,
			Source: classifySource(c.Line),
		})
	}
	return out
}

func revListSet(dir, headRef string) (map[string]bool, error) {
	cmd := exec.Command("git", "-C", dir, "rev-list", headRef)
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			set[line] = true
		}
	}
	return set, nil
}

// batchCommitOIDs resolves each SHA to its full commit OID. Missing or
// non-commit objects are omitted from the result map.
func batchCommitOIDs(dir string, shas []string) (map[string]string, error) {
	var stdin bytes.Buffer
	for _, sha := range shas {
		fmt.Fprintf(&stdin, "%s^{commit}\n", sha)
	}
	cmd := exec.Command("git", "-C", dir, "cat-file", "--batch-check")
	cmd.Stdin = &stdin
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(shas))
	sc := bufio.NewScanner(bytes.NewReader(raw))
	i := 0
	for sc.Scan() {
		if i >= len(shas) {
			break
		}
		sha := shas[i]
		i++
		fields := strings.Fields(sc.Text())
		// "OID commit SIZE" on hit; "SHA missing" / "SHA^{commit} missing" on miss.
		if len(fields) >= 2 && fields[1] == "commit" {
			out[sha] = fields[0]
		}
	}
	return out, sc.Err()
}
