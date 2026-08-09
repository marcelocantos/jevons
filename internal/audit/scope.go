// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ScopeKind is one audited surface. A full-scan pass covers all three:
// product code, skills trees, and agent prompts / persona / standing
// briefs (🎯T357 acceptance #1). A pass missing a kind is degraded, and
// says so, rather than quietly auditing code only.
type ScopeKind string

const (
	// ScopeCode is product source under the named code roots.
	ScopeCode ScopeKind = "code"
	// ScopeSkills is the skills trees (~/.claude/skills and project).
	ScopeSkills ScopeKind = "skills"
	// ScopePrompts is agent prompts, persona, and standing briefs.
	ScopePrompts ScopeKind = "prompts"
)

// RequiredScopes is the full-scan coverage contract.
var RequiredScopes = []ScopeKind{ScopeCode, ScopeSkills, ScopePrompts}

// scanPrecedence orders scope assignment when roots overlap. A prompt file
// such as internal/config/persona.md also sits under a code root; the more
// specific surface claims it, so it is audited once, as a prompt, and its
// findings carry one stable scope across passes.
var scanPrecedence = []ScopeKind{ScopePrompts, ScopeSkills, ScopeCode}

// NormalizeScope maps free text to a known scope kind ("" when unknown).
func NormalizeScope(s string) ScopeKind {
	switch ScopeKind(strings.ToLower(strings.TrimSpace(s))) {
	case ScopeCode:
		return ScopeCode
	case ScopeSkills:
		return ScopeSkills
	case ScopePrompts:
		return ScopePrompts
	}
	return ""
}

// SkipReason explains why the classifier excluded a path (empty = keep).
type SkipReason string

const (
	// SkipNone means the file is in scope.
	SkipNone SkipReason = ""
	// SkipHidden is a dotfile or dot-directory.
	SkipHidden SkipReason = "hidden"
	// SkipVendored is third-party or build output.
	SkipVendored SkipReason = "vendored"
	// SkipNotAudited is an extension the auditor does not read.
	SkipNotAudited SkipReason = "not_audited"
	// SkipGenerated is a lockfile or generated artifact.
	SkipGenerated SkipReason = "generated"
	// SkipTooLarge exceeds the per-file byte bound.
	SkipTooLarge SkipReason = "too_large"
	// SkipEmpty is a zero-byte file.
	SkipEmpty SkipReason = "empty"
)

// Bounds are the scan-shaping limits for one pass.
type Bounds struct {
	MaxFilesPerScope int   `json:"max_files_per_scope"`
	MaxBytesPerScope int64 `json:"max_bytes_per_scope"`
	MaxFileBytes     int64 `json:"max_file_bytes"`
}

// auditedExtensions are the file types an audit pass reads. Anything else
// (binaries, images, archives, media) is skipped: the auditor reasons over
// source, config, and prose, not blobs.
var auditedExtensions = map[string]bool{
	".go": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true,
	".tsx": true, ".jsx": true, ".css": true, ".html": true, ".swift": true,
	".sh": true, ".bash": true, ".zsh": true, ".py": true, ".rs": true,
	".sql": true, ".tla": true, ".md": true, ".yaml": true, ".yml": true,
	".json": true, ".toml": true, ".txt": true,
}

// skipDirs are directories never descended into: third-party trees, build
// output, and VCS internals. Auditing them burns budget on code we neither
// own nor change.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".build": true, "DerivedData": true, "__pycache__": true,
	".venv": true, "venv": true, "target": true, "coverage": true,
	"test-results": true, "playwright-report": true, ".next": true,
	".cache": true, "tmp": true,
}

// generatedFiles are checked-in artifacts with no audit value.
var generatedFiles = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "Cargo.lock": true, "Package.resolved": true,
}

// SkipDir reports whether a directory name is never descended into.
func SkipDir(name string) bool {
	if skipDirs[name] {
		return true
	}
	// Hidden directories are skipped, except the .claude tree which holds
	// the skills and prompt surfaces this audit exists to cover.
	return strings.HasPrefix(name, ".") && name != "." && name != ".claude"
}

// ClassifyFile is the pure in-scope decision for one file. It returns
// SkipNone when the file belongs in a manifest.
func ClassifyFile(name string, size int64, maxFileBytes int64) SkipReason {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." {
		return SkipNotAudited
	}
	if generatedFiles[base] {
		return SkipGenerated
	}
	if strings.HasPrefix(base, ".") {
		return SkipHidden
	}
	if strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") {
		return SkipGenerated
	}
	if strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_generated.go") {
		return SkipGenerated
	}
	if !auditedExtensions[strings.ToLower(filepath.Ext(base))] {
		return SkipNotAudited
	}
	if size <= 0 {
		return SkipEmpty
	}
	if maxFileBytes > 0 && size > maxFileBytes {
		return SkipTooLarge
	}
	return SkipNone
}

// FileRef is one in-scope file in a manifest.
type FileRef struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// ScopeManifest is the bounded file list for one scope kind.
type ScopeManifest struct {
	Kind         ScopeKind `json:"kind"`
	Roots        []string  `json:"roots,omitempty"`
	MissingRoots []string  `json:"missing_roots,omitempty"`
	Files        []FileRef `json:"files,omitempty"`
	TotalBytes   int64     `json:"total_bytes"`
	// Skipped counts files the classifier excluded, by reason.
	Skipped map[SkipReason]int `json:"skipped,omitempty"`
	// Truncated is set when a bound clipped the listing: the auditor is
	// told so it never reports "clean" over an unseen remainder.
	Truncated bool `json:"truncated"`
	// Omitted counts in-scope files dropped by the bounds.
	Omitted int `json:"omitted"`
}

// Covered reports whether this scope contributed any files.
func (s ScopeManifest) Covered() bool { return len(s.Files) > 0 }

// Manifest is the bounded full-scan scope for one audit pass.
type Manifest struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Workdir     string          `json:"workdir,omitempty"`
	Bounds      Bounds          `json:"bounds"`
	Scopes      []ScopeManifest `json:"scopes"`
}

// Scope returns the manifest for a kind (zero value when absent).
func (m Manifest) Scope(kind ScopeKind) ScopeManifest {
	for _, s := range m.Scopes {
		if s.Kind == kind {
			return s
		}
	}
	return ScopeManifest{Kind: kind}
}

// CoveredScopes lists the scope kinds that contributed files.
func (m Manifest) CoveredScopes() []ScopeKind {
	var out []ScopeKind
	for _, kind := range RequiredScopes {
		if m.Scope(kind).Covered() {
			out = append(out, kind)
		}
	}
	return out
}

// MissingScopes lists required scope kinds with no files this pass. A
// non-empty result means the pass is a partial scan: findings still count,
// but residue in the missing scopes must not be resolved (residue.Merge
// only resolves within covered scopes).
func (m Manifest) MissingScopes() []ScopeKind {
	var out []ScopeKind
	for _, kind := range RequiredScopes {
		if !m.Scope(kind).Covered() {
			out = append(out, kind)
		}
	}
	return out
}

// FullScan reports whether every required scope contributed files.
func (m Manifest) FullScan() bool { return len(m.MissingScopes()) == 0 }

// TotalFiles counts files across all scopes.
func (m Manifest) TotalFiles() int {
	n := 0
	for _, s := range m.Scopes {
		n += len(s.Files)
	}
	return n
}

// TotalBytes sums manifest bytes across all scopes.
func (m Manifest) TotalBytes() int64 {
	var n int64
	for _, s := range m.Scopes {
		n += s.TotalBytes
	}
	return n
}

// Truncated reports whether any scope was clipped by a bound.
func (m Manifest) Truncated() bool {
	for _, s := range m.Scopes {
		if s.Truncated {
			return true
		}
	}
	return false
}

// BuildManifest walks the configured roots and produces the bounded scan
// scope. Roots may be directories or single files. Unreadable or missing
// roots are recorded, not fatal: a machine without ~/.claude/skills still
// gets a code+prompts audit, and the missing scope is visible.
func BuildManifest(cfg Config, now time.Time) (Manifest, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	bounds := cfg.Bounds()
	m := Manifest{
		GeneratedAt: now,
		Workdir:     strings.TrimSpace(cfg.Workdir),
		Bounds:      bounds,
	}
	// Claimed is shared across scopes so overlapping roots never list the
	// same file twice; precedence order decides which surface claims it.
	claimed := map[string]bool{}
	byKind := map[ScopeKind]ScopeManifest{}
	for _, kind := range scanPrecedence {
		sm, err := scanScope(kind, cfg.RootsFor(kind), bounds, claimed)
		if err != nil {
			return m, err
		}
		byKind[kind] = sm
	}
	// Emit in the stable reporting order, not the scan order.
	for _, kind := range RequiredScopes {
		m.Scopes = append(m.Scopes, byKind[kind])
	}
	return m, nil
}

// scanScope collects the bounded file list for one scope kind. Files are
// gathered first and sorted by path so the manifest is deterministic, then
// the bounds clip a stable prefix — the same tree always yields the same
// manifest, which is what makes finding fingerprints comparable across
// passes.
func scanScope(kind ScopeKind, roots []string, bounds Bounds, seen map[string]bool) (ScopeManifest, error) {
	sm := ScopeManifest{Kind: kind, Skipped: map[SkipReason]int{}}
	if seen == nil {
		seen = map[string]bool{}
	}
	var candidates []FileRef

	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs := root
		if !filepath.IsAbs(abs) {
			if p, err := filepath.Abs(abs); err == nil {
				abs = p
			}
		}
		info, err := os.Stat(abs)
		if err != nil {
			sm.MissingRoots = append(sm.MissingRoots, root)
			continue
		}
		sm.Roots = append(sm.Roots, root)

		if !info.IsDir() {
			if reason := ClassifyFile(abs, info.Size(), bounds.MaxFileBytes); reason != SkipNone {
				sm.Skipped[reason]++
				continue
			}
			if !seen[abs] {
				seen[abs] = true
				candidates = append(candidates, FileRef{Path: abs, Bytes: info.Size()})
			}
			continue
		}

		walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// An unreadable subtree is skipped, not fatal: a permission
				// hole in one directory must not void the whole audit.
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if path != abs && SkipDir(d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if reason := ClassifyFile(path, info.Size(), bounds.MaxFileBytes); reason != SkipNone {
				sm.Skipped[reason]++
				return nil
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			candidates = append(candidates, FileRef{Path: path, Bytes: info.Size()})
			return nil
		})
		if walkErr != nil {
			return sm, fmt.Errorf("audit scan %s root %s: %w", kind, root, walkErr)
		}
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })

	for _, f := range candidates {
		if len(sm.Files) >= bounds.MaxFilesPerScope {
			sm.Truncated = true
			sm.Omitted++
			continue
		}
		if bounds.MaxBytesPerScope > 0 && sm.TotalBytes+f.Bytes > bounds.MaxBytesPerScope && len(sm.Files) > 0 {
			sm.Truncated = true
			sm.Omitted++
			continue
		}
		sm.Files = append(sm.Files, f)
		sm.TotalBytes += f.Bytes
	}
	if len(sm.Skipped) == 0 {
		sm.Skipped = nil
	}
	return sm, nil
}
