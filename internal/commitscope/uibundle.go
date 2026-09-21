// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"fmt"
	"path"
	"strings"
)

// UIBundleEnv turns the stale-bundle rule off for one command (🎯T812).
// Separate from DisableEnv: opting out of shared-index protection must not
// also wave a stale embed through. The binary records every bypass.
const UIBundleEnv = "JEVONS_UI_BUNDLE"

// BundlePath is the tracked embed input that `make ui-build` regenerates.
const BundlePath = "ui/bundle.zip"

// bundleInputDirs are trees whose files Vite copies or compiles into the
// bundle (ui/index.html, ui/public, ui/src) and files at the ui/ root that
// steer the build. Read from scripts/package-ui and ui/vite.config.ts.
var bundleRootFiles = map[string]bool{
	"ui/index.html":        true,
	"ui/package.json":      true,
	"ui/package-lock.json": true,
	"ui/vite.config.ts":    true,
}

// IsBundleInput reports whether a change to p can alter ui/bundle.zip. Tests,
// the oracle fixtures/methodology, and vitest setup live under ui/src but are
// never imported by the app entry, so they are excluded; counting them would
// refuse every test-only commit. This is a path rule, not a build: a source
// change that happens to be bundle-neutral (a comment) still counts.
func IsBundleInput(p string) bool {
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	switch {
	case bundleRootFiles[p]:
		return true
	case strings.HasPrefix(p, "ui/tsconfig") && strings.HasSuffix(p, ".json"):
		return true
	case strings.HasPrefix(p, "ui/public/"):
		return true
	case !strings.HasPrefix(p, "ui/src/"):
		return false
	}
	base := path.Base(p)
	switch {
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".test.tsx"),
		strings.HasPrefix(base, "test-setup"),
		strings.HasSuffix(base, ".md"),
		strings.HasPrefix(p, "ui/src/oracle/"):
		return false
	}
	return true
}

// bundleVerdict refuses a commit whose index changes bundle inputs without
// changing ui/bundle.zip. It judges only the staged paths, so under
// `git commit --only` another worker's uncommitted ui/src edits are invisible.
// It cannot catch a WRONG bundle, only a missing one; the byte comparison
// stays with `make ui-check-bundle`.
func bundleVerdict(req *Request) (string, bool) {
	var inputs []string
	for _, p := range req.Staged {
		if p == BundlePath {
			return "", false
		}
		if IsBundleInput(p) {
			inputs = append(inputs, p)
		}
	}
	if len(inputs) == 0 {
		return "", false
	}
	if req.BundleDisabled {
		return BypassNotice(inputs), false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "commitscope: refusing — this commit changes %s but not %s (🎯T812).\n\n", plural(len(inputs), "React bundle input"), BundlePath)
	b.WriteString("The daemon embeds the tracked bundle, so the source and its rebuilt bundle\nmust land in the SAME commit. Stale inputs:\n")
	for i, p := range inputs {
		if i == MaxNamed {
			fmt.Fprintf(&b, "  … and %d more\n", len(inputs)-MaxNamed)
			break
		}
		fmt.Fprintf(&b, "  %s\n", p)
	}
	b.WriteString("\nRebuild, then commit source and bundle together:\n")
	b.WriteString("  make ui-build\n")
	b.WriteString("  git commit --only <your ui paths> ui/bundle.zip -m \"…\"\n")
	b.WriteString("  bin/gate -clean -- make ui-check-bundle\n\n")
	b.WriteString("Note: this check sees only that the bundle was touched, not that it is right;\nthe gate above is the byte comparison. A bundle-neutral change to a bundle\ninput (a comment) is refused too; rebuild anyway, or bypass deliberately:\n")
	fmt.Fprintf(&b, "  %s=off git commit …   (the bypass is logged)\n", UIBundleEnv)
	return b.String(), true
}

// BypassNoticePrefix starts the line a bypassed stale-bundle check leaves on
// stderr and in the audit log, so a bypass is visible and greppable.
const BypassNoticePrefix = "commitscope: UI bundle check BYPASSED"

// BypassNotice is the audit line for a commit that changed bundle inputs
// without the bundle while UIBundleEnv=off.
func BypassNotice(inputs []string) string {
	return fmt.Sprintf("%s (%s=off) for %s: %s\n", BypassNoticePrefix, UIBundleEnv, plural(len(inputs), "input"), strings.Join(inputs, " "))
}
