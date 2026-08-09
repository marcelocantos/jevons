package treeguard

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// DisableEnv set to "off" (or "0"/"false") disables the guard. Disabling is a
// deliberate, loud choice — the negative-control test uses it to prove the
// clobber really happens without the guard.
const DisableEnv = "JEVONS_TREEGUARD"

// PathsEnv overrides DefaultGuardedPaths with a comma-separated glob list.
const PathsEnv = "JEVONS_TREEGUARD_PATHS"

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
	} `json:"tool_input"`
}

// Env is the resolved runtime the hook entry points act against.
type Env struct {
	Store    *Store
	RepoRoot string
	Guarded  []string
	Disabled bool
	Now      func() time.Time
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

// Post is the PostToolUse entry point: it records what the session now holds
// for a guarded path, establishing the base for its next write.
func (e *Env) Post(p *Payload) error {
	if e.Disabled || !slices.Contains(ObservingTools, p.ToolName) {
		return nil
	}
	abs, rel, ok := e.resolve(p.ToolInput.FilePath)
	if !ok || !IsGuarded(rel, e.Guarded) {
		return nil
	}
	content, err := readFileOrNil(abs)
	if err != nil || content == nil {
		return err
	}
	return e.Store.Record(p.SessionID, abs, content, e.Now())
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
