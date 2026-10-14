package treeguard

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// DisableEnv set to "off" (or "0"/"false") disables the guard. Disabling is a
// deliberate, loud choice — the negative-control test uses it to prove the
// clobber really happens without the guard.
const DisableEnv = "JEVONS_TREEGUARD"

// PathsEnv overrides DefaultGuardedPaths with a comma-separated glob list.
const PathsEnv = "JEVONS_TREEGUARD_PATHS"

// BashEnv set to "off" disables the shell-command half of the guard (🎯T391)
// while leaving the tool boundary (🎯T376) intact. That combination is exactly
// the pre-fix tree, which is why the acceptance oracle drives its control
// through this knob rather than through DisableEnv: a red that comes from
// turning the whole guard off would not show that the SHELL coverage is what
// makes the difference.
const BashEnv = "JEVONS_TREEGUARD_BASH"

// ProjectDirEnv is the repo root Claude Code exports to hook commands.
const ProjectDirEnv = "CLAUDE_PROJECT_DIR"

// ObservationTTL bounds how long a session's observations are kept.
const ObservationTTL = 7 * 24 * time.Hour

// Payload is the subset of the Claude Code hook JSON the guard needs. Unknown
// fields are ignored, so a payload shape change degrades to a missing field
// rather than a parse failure.
type Payload struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath string `json:"file_path"`
		// Content is the whole proposed file for Write. Edit/MultiEdit leave
		// it empty, so the guard treats their result as unknown.
		Content *string `json:"content"`
		// Command is the shell command for Bash (🎯T391).
		Command string `json:"command"`
	} `json:"tool_input"`
}

// Env is the resolved runtime the hook entry points act against.
type Env struct {
	Store    *Store
	RepoRoot string
	Guarded  []string
	Disabled bool
	// BashOff disables the shell-command half only (🎯T391).
	BashOff bool
	Now     func() time.Time
}

// NewEnv resolves the guard's runtime from the process environment and the
// payload's cwd. repoRoot falls back through CLAUDE_PROJECT_DIR, the payload
// cwd, then the process working directory.
func NewEnv(p *Payload) *Env {
	root := os.Getenv(ProjectDirEnv)
	if root == "" {
		root = p.CWD
	}
	if root == "" {
		root, _ = os.Getwd()
	}
	guarded := DefaultGuardedPaths
	if raw := os.Getenv(PathsEnv); raw != "" {
		guarded = nil
		for p := range strings.SplitSeq(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				guarded = append(guarded, p)
			}
		}
	}
	return &Env{
		Store:    &Store{Root: DefaultStoreRoot()},
		RepoRoot: root,
		Guarded:  guarded,
		Disabled: isOff(os.Getenv(DisableEnv)),
		BashOff:  isOff(os.Getenv(BashEnv)),
		Now:      time.Now,
	}
}

// DecodePayload reads one hook payload from r.
func DecodePayload(r io.Reader) (*Payload, error) {
	var p Payload
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Pre is the PreToolUse entry point: it decides whether a mutating tool call
// may proceed.
func (e *Env) Pre(p *Payload) (Decision, error) {
	if e.Disabled {
		return Decision{Verdict: Allow, Reason: "disabled"}, nil
	}
	if p.ToolName == ToolBash {
		return e.preBash(p)
	}
	abs, rel, ok := e.resolve(p.ToolInput.FilePath)
	if !ok {
		return Decision{Verdict: Allow, Reason: "outside-repo"}, nil
	}
	onDisk, err := readFileOrNil(abs)
	if err != nil {
		return Decision{}, err
	}
	obs, obsContent, err := e.Store.Lookup(p.SessionID, abs)
	if err != nil {
		return Decision{}, err
	}
	var proposed []byte
	if p.ToolInput.Content != nil {
		proposed = []byte(*p.ToolInput.Content)
	}
	return Decide(&DecideArgs{
		Tool:            p.ToolName,
		RelPath:         rel,
		Guarded:         e.Guarded,
		OnDisk:          onDisk,
		Observed:        obs,
		ObservedContent: obsContent,
		Proposed:        proposed,
	}), nil
}

// preBash is the shell half of the same policy (🎯T391). The command is not a
// file path, so the first question is which paths it writes; command.go answers
// that, and each answer that lands on a guarded path goes through the identical
// compare-and-swap the tool boundary uses.
//
// Proposed content is left nil on purpose: a shell command's result is not
// predictable from its text, so the guard treats it the way it already treats
// Edit — every line another worker added since this session looked is at risk.
// The first denial wins; a command that writes two guarded files is refused on
// whichever it would clobber first, and the worker has to re-read anyway.
func (e *Env) preBash(p *Payload) (Decision, error) {
	if e.BashOff {
		return Decision{Verdict: Allow, Reason: "bash-guard-off"}, nil
	}
	if ref, ok, definite := DetectBranchCheckout(p.ToolInput.Command); ok {
		if d, checked := e.decideBranchCheckout(ref, definite); checked {
			return d, nil
		}
	}
	for _, write := range ScanCommand(p.ToolInput.Command) {
		if write.Form == FormGitDiscardAll {
			if d, checked := e.decideLedgerDiscard(); checked {
				return d, nil
			}
			continue
		}
		abs, rel, ok := e.resolve(e.expand(write.Path))
		if !ok || !IsGuarded(rel, e.Guarded) {
			continue
		}
		onDisk, err := readFileOrNil(abs)
		if err != nil {
			return Decision{}, err
		}
		obs, obsContent, err := e.Store.Lookup(p.SessionID, abs)
		if err != nil {
			return Decision{}, err
		}
		decision := Decide(&DecideArgs{
			Tool:            ToolBash,
			Form:            write.Form,
			RelPath:         rel,
			Guarded:         e.Guarded,
			OnDisk:          onDisk,
			Observed:        obs,
			ObservedContent: obsContent,
		})
		if decision.Verdict == Deny {
			return decision, nil
		}
	}
	return Decision{Verdict: Allow, Reason: "bash-no-guarded-write"}, nil
}

// decideLedgerDiscard is the 🎯T1011 guard: a `git reset --hard/--merge/--keep`
// or path-less `git clean -f` names no pathspec, so command.go cannot tell
// which guarded file is at risk — it can only report that EVERYTHING dirty in
// the working tree is about to be discarded. This asks git directly whether
// the ledger (bullseye.yaml) is currently dirty against HEAD; if so, the
// command would silently wipe uncommitted ledger rows exactly the way the
// 2026-10-06 incident did, and is refused. checked is false when the repo
// cannot be queried (not a git repo at all) — the guard has nothing to say
// and the caller falls through to its normal per-path handling.
func (e *Env) decideLedgerDiscard() (Decision, bool) {
	dirty, ledgerRel, err := ledgerDirty(e.RepoRoot)
	if err != nil {
		return Decision{}, false
	}
	if !dirty {
		return Decision{Verdict: Allow, Reason: "ledger-clean"}, true
	}
	return Decision{
		Verdict: Deny,
		Reason:  "ledger-discard-all",
		Message: "treeguard: refusing git reset/clean — " + ledgerRel +
			" (the bullseye intent ledger) has uncommitted changes that this" +
			" command would silently discard, the same way the 2026-10-06" +
			" incident lost the T1012/T1013/T1014/T1015 rows. Commit " +
			ledgerRel + " first (`git commit --only " + ledgerRel + " -m '...'`)," +
			" then retry. 🎯T1011",
	}, true
}

// ledgerDirty reports whether the ledger file differs from HEAD in the
// working tree of the repo at root, and the repo-relative path git used to
// find it. err is non-nil only when root is not inside a git repo (or git
// itself cannot be run); a clean exec failure on `git diff` for any other
// reason is treated as "cannot tell, so do not deny" by the caller via ok.
func ledgerDirty(root string) (dirty bool, ledgerRel string, err error) {
	top, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return false, "", err
	}
	repoRoot := strings.TrimSpace(string(top))
	ledgerRel = "bullseye.yaml"
	ledgerAbs := filepath.Join(repoRoot, ledgerRel)
	if _, statErr := os.Stat(ledgerAbs); statErr != nil {
		// No ledger at the repo root: nothing for this guard to protect.
		return false, ledgerRel, nil
	}
	cmd := exec.Command("git", "-C", repoRoot, "diff", "--quiet", "HEAD", "--", ledgerRel)
	runErr := cmd.Run()
	if runErr == nil {
		return false, ledgerRel, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		// git diff --quiet exits 1 when there IS a difference.
		return exitErr.ExitCode() == 1, ledgerRel, nil
	}
	return false, ledgerRel, runErr
}

// Post is the PostToolUse entry point: it records what the session now holds
// for a guarded path, establishing the base for its next write, and journals
// that content as guard-seen so the sweep can tell an unguarded change apart
// from this one (🎯T391).
//
// The returned report is worker-facing and empty in the ordinary case; a
// non-empty report names content that disappeared without passing the guard.
func (e *Env) Post(p *Payload) (string, error) {
	if e.Disabled {
		return "", nil
	}
	if p.ToolName == ToolBash {
		return e.postBash(p)
	}
	if !slices.Contains(ObservingTools, p.ToolName) {
		return "", nil
	}
	abs, rel, ok := e.resolve(p.ToolInput.FilePath)
	if !ok || !IsGuarded(rel, e.Guarded) {
		return "", nil
	}
	content, err := readFileOrNil(abs)
	if err != nil || content == nil {
		return "", err
	}
	if err := e.Store.Record(p.SessionID, abs, content, e.Now()); err != nil {
		return "", err
	}
	return "", e.Store.Journal(abs, content, e.Now(), p.SessionID, ViaTool)
}

// postBash closes the loop after an allowed shell command: the paths the
// recognizer named are re-read and journalled as guard-seen, and then the sweep
// asks whether any OTHER guarded file changed behind the guard's back — a path
// the recognizer missed, or a write from a session that never passed through
// this hook at all. Journalling first is what keeps the command's own
// legitimate edit from being reported as an unguarded one.
func (e *Env) postBash(p *Payload) (string, error) {
	if e.BashOff {
		return "", nil
	}
	now := e.Now()
	for _, write := range ScanCommand(p.ToolInput.Command) {
		abs, rel, ok := e.resolve(e.expand(write.Path))
		if !ok || !IsGuarded(rel, e.Guarded) {
			continue
		}
		content, err := readFileOrNil(abs)
		if err != nil || content == nil {
			continue // removed by the command: the sweep reports it instead
		}
		if err := e.Store.Record(p.SessionID, abs, content, now); err != nil {
			return "", err
		}
		if err := e.Store.Journal(abs, content, now, p.SessionID, ViaBash); err != nil {
			return "", err
		}
	}
	findings, err := e.Sweep(now)
	if err != nil {
		return "", err
	}
	if err := e.RecordSweep(findings); err != nil {
		return "", err
	}
	return FormatSweepReport(findings), nil
}

// decideBranchCheckout applies DecideBranchCheckout (🎯T955) when e.RepoRoot
// is the shared clone itself, not one of its isolated worker worktrees — a
// worker switching branches inside its OWN linked worktree is not this
// guard's business, only the shared clone's checked-out branch is. checked
// is false when the guard has nothing to say (linked worktree, detached
// HEAD, already on ref, or — for an ambiguous `git checkout <arg>` — <arg>
// does not actually resolve to a ref git would switch to).
func (e *Env) decideBranchCheckout(ref string, definite bool) (Decision, bool) {
	if worktree.IsLinkedWorktree(e.RepoRoot) {
		return Decision{}, false
	}
	branch, err := gitSymbolicRefShort(e.RepoRoot)
	if err != nil {
		return Decision{}, false // detached HEAD or not a repo: no integration branch to protect
	}
	if ref == branch {
		return Decision{}, false
	}
	if !definite && !isRefLike(e.RepoRoot, ref) {
		// A bare `git checkout <arg>` where <arg> is not a real ref is a path
		// restore (`git checkout .`, `git checkout some/file.go`), not a
		// branch switch.
		return Decision{}, false
	}
	return DecideBranchCheckout(&BranchGuardArgs{CurrentBranch: branch, TargetRef: ref}), true
}

// gitSymbolicRefShort is the shared clone's checked-out branch, or an error
// when HEAD is detached (no branch to protect).
func gitSymbolicRefShort(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isRefLike reports whether ref resolves to a commit git could check out —
// the disambiguator for a bare `git checkout <arg>` (🎯T955).
func isRefLike(dir, ref string) bool {
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() == nil
}

// expand resolves the variables a fleet worker's command actually carries.
// A path the guard cannot resolve is returned empty rather than guessed at:
// an unresolved variable makes the target undecidable, and inventing one would
// either refuse a write to a file nobody named or miss the one that matters.
// Undecidable is the sweep's half of the target, not the refusal's.
func (e *Env) expand(p string) string {
	for _, form := range []string{"${" + ProjectDirEnv + "}", "$" + ProjectDirEnv} {
		p = strings.ReplaceAll(p, form, e.RepoRoot)
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	if strings.ContainsAny(p, "$*?") {
		return ""
	}
	return p
}

// resolve maps a tool's file_path to (absolute, repo-relative) and reports
// whether it lies inside the repo.
func (e *Env) resolve(filePath string) (abs, rel string, ok bool) {
	if filePath == "" {
		return "", "", false
	}
	abs = filePath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(e.RepoRoot, abs)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(e.RepoRoot, abs)
	if err != nil {
		return "", "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", "", false
	}
	return abs, rel, true
}

func readFileOrNil(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func isOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "0", "false", "no":
		return true
	}
	return false
}
