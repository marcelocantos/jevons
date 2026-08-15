package treeguard

// Provider coverage (🎯T391 acceptance 2).
//
// The refusal path is a Claude Code hook. That is not a detail a worker can be
// expected to infer: a grok/claudia worker in the same clone, editing the same
// web/index.html, is told nothing at all today and reasonably assumes the
// guard the repo documents is protecting it too. Silent absence of the guard
// for a whole provider class is the defect — worse than absence, because it
// buys false confidence.
//
// So coverage is a first-class answer with two independent axes, and every
// worker is told which one it has. Detection is universal because the sweep
// runs at the git pre-commit boundary that every provider passes through;
// refusal is not, and says so.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Coverage is what the write guard can do for one provider.
type Coverage struct {
	Provider string
	// Refuses reports whether a clobbering write is stopped BEFORE it lands.
	Refuses bool
	// Detects reports whether a clobber that lands is reported afterwards.
	Detects bool
	// Note is the worker-facing explanation, always non-empty.
	Note string
}

// Providers whose harness carries the PreToolUse/PostToolUse hooks the
// refusal path is built on.
var refusingProviders = []string{"claude", "claude-code", "claudecode", "anthropic"}

// ProviderCoverage answers what protection a worker on this provider has.
// An unknown or empty provider is reported as NOT refusing: claiming coverage
// the guard cannot deliver is the failure this function exists to prevent, and
// an unrecognized harness is exactly the case where that claim would be wrong.
func ProviderCoverage(provider string) Coverage {
	p := strings.ToLower(strings.TrimSpace(provider))
	if slices.Contains(refusingProviders, p) {
		return Coverage{
			Provider: p,
			Refuses:  true,
			Detects:  true,
			Note: "shared hot files are compare-and-swap: a stale Write, Edit " +
				"or recognized shell write (sed -i, redirection, git checkout) is " +
				"REFUSED before it lands, and anything that gets past that is " +
				"reported at commit time (🎯T376 / 🎯T391).",
		}
	}
	name := p
	if name == "" {
		name = "unknown"
	}
	return Coverage{
		Provider: name,
		Refuses:  false,
		Detects:  true,
		Note: "your provider (" + name + ") carries no write-guard hook, so a write " +
			"that clobbers another worker's edit to a shared hot file is NOT refused — " +
			"it is only DETECTED afterwards, by the sweep at git pre-commit. Re-read a " +
			"shared file immediately before writing it, and write the smallest region " +
			"you can (🎯T391).",
	}
}

// ProviderEnv names the provider explicitly. The daemon knows the answer from
// the registry row; a worker asking `treeguard doctor` on its own behalf has
// only its environment, and the harness markers below.
const ProviderEnv = "JEVONS_AGENT_PROVIDER"

// ClaudeCodeEnv is set to "1" in every subprocess Claude Code spawns, hooks
// included. It is the one marker that means the refusal path is present,
// because the refusal path IS a Claude Code hook.
const ClaudeCodeEnv = "CLAUDECODE"

// DetectProvider answers which provider this process is running under, given a
// lookup (os.Getenv in production). An explicit ProviderEnv wins; otherwise the
// harness marker decides; otherwise the answer is "" — reported as unprotected
// by ProviderCoverage, which is the safe direction to be wrong in.
func DetectProvider(lookup func(string) string) string {
	if p := strings.TrimSpace(lookup(ProviderEnv)); p != "" {
		return p
	}
	if strings.TrimSpace(lookup(ClaudeCodeEnv)) != "" {
		return "claude"
	}
	return ""
}

// BriefLine renders coverage as one line for a worker's brief.
func (c Coverage) BriefLine() string {
	if c.Refuses {
		return "WRITE GUARD: active (refuses + detects) — " + c.Note
	}
	return "WRITE GUARD: DETECT-ONLY — " + c.Note
}

// Surfacing the answer.
//
// A worker under a provider with no hooks never runs the hook that would tell
// it so — which is the whole difficulty of acceptance 2. `git commit` is the
// one boundary every provider passes through, so the notice is emitted there,
// alongside the sweep. A worker that HAS the refusal path is told nothing: it
// is protected, and a line printed on every commit is a line nobody reads by
// the second day.

// CoverageNoticeInterval bounds how often the unprotected-provider notice
// repeats. Long enough not to become wallpaper, short enough that a worker
// running for days is reminded.
const CoverageNoticeInterval = 24 * time.Hour

// noticeStamp records when the notice was last shown, under the store root.
const noticeStamp = "coverage-notice"

// CoverageNotice returns the statement a worker must see when its provider has
// no refusal path, or "" when it has one or has been told inside the interval.
// Stamp failures return the notice rather than suppressing it: showing the line
// twice is a nuisance, never showing it is the defect.
func (e *Env) CoverageNotice(provider string, now time.Time) string {
	coverage := ProviderCoverage(provider)
	if coverage.Refuses {
		return ""
	}
	stamp := filepath.Join(e.Store.Root, noticeStamp)
	if info, err := os.Stat(stamp); err == nil && now.Sub(info.ModTime()) < CoverageNoticeInterval {
		return ""
	}
	if err := os.MkdirAll(e.Store.Root, 0o755); err == nil {
		_ = os.WriteFile(stamp, []byte(now.UTC().Format(time.RFC3339)), 0o644)
	}
	return "treeguard: " + coverage.BriefLine() + "\n"
}
