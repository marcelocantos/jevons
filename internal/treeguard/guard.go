// Package treeguard makes concurrent fleet writes to shared hot files
// detectable instead of silent (🎯T376).
//
// Several fleet workers run in one clone at once. When two of them touch
// web/index.html (~10.5k lines, the hottest shared file in the repo), the
// second worker's full-file write is derived from a Read that predates the
// first worker's edit, so it silently reverts work that was already correct.
// This happened three times while landing 🎯T370 and cost roughly half that
// mission's wall-clock in re-applying edits.
//
// The mechanism here is optimistic concurrency control (compare-and-swap) at
// the write boundary: every Read/Write/Edit of a guarded path records the
// whole-file hash the session observed, and a write is refused when the hash
// on disk no longer matches that observation. Refusal names the specific
// lines that would have been lost, so the failure is loud rather than silent.
//
// Everything in this file is pure: no filesystem, no clock, no environment.
// The store (store.go) and the hook entry points (hook.go) supply the inputs.
package treeguard

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// MaxAtRisk caps how many about-to-be-lost lines a refusal message names.
// A refusal is a diagnostic, not a diff; the first few lines are enough to
// identify whose edit is in the way.
const MaxAtRisk = 12

// DefaultGuardedPaths are the shared files that concurrent workers collide on.
// Patterns are matched with path.Match against the repo-relative slash path,
// so "web/*.html" covers the cockpit but not web/scripts/*.js (those are
// per-slice files that rarely have two authors in the same window).
//
// Override with JEVONS_TREEGUARD_PATHS (comma-separated) — see hook.go.
var DefaultGuardedPaths = []string{
	"web/index.html",
	"web/*.html",
	"web/embed.go",
	"Makefile",
	"AGENTS.md",
	"CLAUDE.md",
	"agents-guide.md",
	"internal/config/persona.md",
	"bullseye.yaml",
	".gitignore",
	".claude/settings.json",
}

// MutatingTools are the tool names whose calls replace file content and can
// therefore drop another worker's lines. Read is guarded too, but only as an
// observation source (see ObservingTools).
var MutatingTools = []string{"Write", "Edit", "MultiEdit", "NotebookEdit", "StrReplace"}

// ToolBash is the tool whose payload carries a shell command rather than a
// file path. It is not in MutatingTools because whether it mutates anything is
// a property of the command, not of the tool: command.go answers that, and
// DecideArgs.Form carries the answer here (🎯T391).
const ToolBash = "Bash"

// ObservingTools establish or refresh a session's base for a path. Read is the
// normal entry point; the mutating tools are included because a successful
// write leaves the session holding the content it just wrote.
var ObservingTools = append([]string{"Read"}, MutatingTools...)

// Verdict is the guard's answer for one attempted write.
type Verdict int

const (
	// Allow lets the tool call proceed.
	Allow Verdict = iota
	// Deny refuses the tool call. The caller reports Message and exits 2.
	Deny
)

// Observation is what a session last saw on disk for one path.
type Observation struct {
	Path string    `json:"path"`
	Hash string    `json:"hash"`
	At   time.Time `json:"at"`
}

// Decision is the result of Decide.
type Decision struct {
	Verdict Verdict
	// Reason is a stable machine slug, useful in tests and logs.
	Reason string
	// Message is the agent-facing explanation, non-empty on Deny.
	Message string
	// AtRisk names lines present on disk that the proposed write would drop.
	AtRisk []string
}

// DecideArgs are the inputs to Decide. All content is whole-file.
type DecideArgs struct {
	// Tool is the tool name from the hook payload.
	Tool string
	// RelPath is the repo-relative, slash-separated path being written.
	RelPath string
	// Guarded are the glob patterns to match RelPath against. Nil means
	// DefaultGuardedPaths.
	Guarded []string
	// OnDisk is the file's current content, nil when the file does not exist.
	OnDisk []byte
	// Observed is what this session last saw, nil when it never looked.
	Observed *Observation
	// ObservedContent is the content behind Observed, used to name the lines
	// another worker added since. Nil is tolerated (the refusal just cannot
	// name lines).
	ObservedContent []byte
	// Proposed is the full content the tool would write. Write supplies it;
	// Edit and MultiEdit do not, because their result depends on the file
	// they are applied to.
	Proposed []byte
	// Form names the shell construct that makes a Bash call a write (🎯T391),
	// e.g. FormInPlace. Empty for the tool path, where Tool alone decides.
	// A non-empty Form is what makes ToolBash mutating.
	Form string
}

// what names the write for a worker reading a refusal: "Write", or the shell
// construct behind a Bash call. Quoting the whole command back would bury the
// one part the worker has to change.
func (a *DecideArgs) what() string {
	if a.Form == "" {
		return a.Tool
	}
	return a.Tool + " (" + a.Form + ")"
}

// Decide is the whole policy. It is deliberately one readable function: the
// order of the checks *is* the policy, and splitting it into predicates would
// hide that.
func Decide(args *DecideArgs) Decision {
	// A recognized write form (🎯T391) is what makes a Bash call mutating; for
	// every other tool the name alone decides.
	if args.Form == "" && !slices.Contains(MutatingTools, args.Tool) {
		return Decision{Verdict: Allow, Reason: "tool-not-mutating"}
	}
	// Ledger refuse is independent of the T376 guarded set: a
	// JEVONS_TREEGUARD_PATHS override must not re-open bullseye.yaml.
	if IsLedgerPath(args.RelPath) {
		return Decision{
			Verdict: Deny,
			Reason:  "ledger-tool-only",
			Message: "refusing " + args.what() + " to " + args.RelPath +
				" — agents must not edit bullseye.yaml. File, status, achieve," +
				" and query go through jevons_target_file or bullseye MCP/CLI" +
				" (`bullseye commit` / `bullseye query`). 🎯T546",
		}
	}
	guarded := args.Guarded
	if guarded == nil {
		guarded = DefaultGuardedPaths
	}
	if !IsGuarded(args.RelPath, guarded) {
		return Decision{Verdict: Allow, Reason: "path-not-guarded"}
	}
	if args.OnDisk == nil {
		// Creating a file cannot clobber content that is not there.
		return Decision{Verdict: Allow, Reason: "new-file"}
	}

	diskHash := HashBytes(args.OnDisk)
	if args.Observed == nil {
		return Decision{
			Verdict: Deny,
			Reason:  "never-observed",
			Message: "treeguard: refusing " + args.what() + " to " + args.RelPath +
				" — this session has never read the file, so the write cannot" +
				" preserve whatever another fleet worker put there.\n" +
				"Read " + args.RelPath + " first, then write.",
		}
	}
	if args.Observed.Hash == diskHash {
		return Decision{Verdict: Allow, Reason: "fresh"}
	}

	atRisk := DetectLostAdditions(args.ObservedContent, args.OnDisk, args.Proposed)
	if len(atRisk) == 0 {
		// The file moved under us, but nothing the other worker added would
		// be dropped: either they only removed lines, or the proposed content
		// already carries their additions.
		return Decision{Verdict: Allow, Reason: "stale-base-no-loss"}
	}
	return Decision{
		Verdict: Deny,
		Reason:  "stale-base",
		AtRisk:  atRisk,
		Message: "treeguard: refusing " + args.what() + " to " + args.RelPath +
			" — another fleet worker changed it since this session read it" +
			" (observed " + shortHash(args.Observed.Hash) + ", on disk " +
			shortHash(diskHash) + "), and this write would drop their lines:\n" +
			"  " + strings.Join(atRisk, "\n  ") + "\n" +
			"Re-read " + args.RelPath + " and re-apply your change on top of" +
			" the current content (" + args.cite() + ").",
	}
}

// cite names the target a refusal comes from: the tool boundary is 🎯T376's,
// the shell boundary 🎯T391's. Both, for a worker chasing the rule.
func (a *DecideArgs) cite() string {
	if a.Form == "" {
		return "🎯T376"
	}
	return "🎯T376 / 🎯T391"
}

// DetectLostAdditions returns lines that exist on disk now, did not exist when
// the session observed the file, and are absent from the proposed content —
// i.e. exactly the other worker's additions that this write would revert.
//
// Blank lines are skipped and comparison is on trimmed text, so reindentation
// does not read as a loss. Duplicated boilerplate (a bare "}") cannot be
// attributed to anyone, so presence rather than count is the test.
//
// Proposed may be nil (Edit/MultiEdit): then every unobserved disk line counts
// as at risk, because the guard cannot know what the edit will produce.
func DetectLostAdditions(observed, disk, proposed []byte) []string {
	base := lineSet(observed)
	var keep map[string]bool
	if proposed != nil {
		keep = lineSet(proposed)
	}
	seen := make(map[string]bool)
	var out []string
	for line := range strings.SplitSeq(string(disk), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || base[t] || keep[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == MaxAtRisk {
			break
		}
	}
	return out
}

// IsGuarded reports whether a repo-relative path matches any guard pattern.
func IsGuarded(rel string, patterns []string) bool {
	rel = filepath.ToSlash(rel)
	for _, p := range patterns {
		if p == rel {
			return true
		}
		if ok, err := path.Match(p, rel); err == nil && ok {
			return true
		}
	}
	return false
}

// IsLedgerPath reports whether rel is the intent ledger. Mutating tools
// on this path are refused (🎯T546) — use bullseye tools instead.
func IsLedgerPath(rel string) bool {
	return strings.EqualFold(path.Base(filepath.ToSlash(rel)), "bullseye.yaml")
}

// HashBytes is the content identity used for compare-and-swap.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

const shortHashLen = 12

func shortHash(h string) string {
	if len(h) <= shortHashLen {
		return h
	}
	return h[:shortHashLen]
}

func lineSet(b []byte) map[string]bool {
	if b == nil {
		return nil
	}
	set := make(map[string]bool)
	for line := range strings.SplitSeq(string(b), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			set[t] = true
		}
	}
	return set
}
