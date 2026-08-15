package attrib

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Drain is the record of one emptying of the shared index.
type Drain struct {
	At      time.Time `json:"at"`
	Session string    `json:"session,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	Reason  string    `json:"reason"`
	Paths   []string  `json:"paths"`
	HeadSHA string    `json:"head_sha"`
	Dir     string    `json:"dir"`
	Patch   string    `json:"patch,omitempty"`
}

// DrainReasonStop is the drain performed when an agent stops.
const DrainReasonStop = "agent-stop"

// DrainIndex empties the shared index, saving what it removed first.
//
// Why empty it at all, and why all of it rather than one agent's share: the
// clone has exactly one index, so an entry left staged is not that agent's
// state — it is a pending contribution to whatever the NEXT worker commits
// (🎯T457). 🎯T377 already requires every worker to commit as
// `git commit --only <paths>`, which builds its own index and needs nothing
// pre-staged. So a staged entry carries no benefit for its author and an
// unbounded hazard for everyone else, and the honest resolution is to leave the
// index empty rather than to reason about whose entry may safely remain.
//
// Nothing is destroyed. Unstaging moves no bytes in the working tree — the
// content stays exactly where its author left it, and this is why the drain can
// be automatic where discarding never could be. The index state is saved as a
// patch anyway, so even the staging itself is reversible.
//
// A clean index returns (nil, nil): the common case writes nothing at all.
func DrainIndex(repoRoot, outRoot, session, agent, reason string, now time.Time) (*Drain, error) {
	staged, err := StagedPaths(repoRoot)
	if err != nil {
		return nil, err
	}
	if len(staged) == 0 {
		return nil, nil
	}
	head, err := Git(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	name := now.UTC().Format("20060102T150405Z")
	if agent != "" {
		name += "-" + sanitize(agent)
	}
	dir := filepath.Join(outRoot, "drains", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	drain := &Drain{
		At:      now.UTC(),
		Session: session,
		Agent:   agent,
		Reason:  reason,
		Paths:   staged,
		HeadSHA: trimNewline(head),
		Dir:     dir,
	}
	patch, err := Git(repoRoot, "diff", "--cached", "--binary")
	if err != nil {
		return nil, err
	}
	if patch != "" {
		if err := os.WriteFile(filepath.Join(dir, indexPatchName), []byte(patch), 0o644); err != nil {
			return nil, err
		}
		drain.Patch = indexPatchName
	}
	manifest, err := json.MarshalIndent(drain, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), manifest, 0o644); err != nil {
		return nil, err
	}

	// Unstage by path rather than `git reset`, so a path git cannot unstage
	// fails loudly on its own name instead of taking the whole index with it.
	args := append([]string{"restore", "--staged", "--"}, staged...)
	if _, err := Git(repoRoot, args...); err != nil {
		// An index.lock held by a concurrent git is the expected contention in
		// a shared clone. Report it rather than forcing: removing another
		// process's lock is how a half-written index becomes a corrupt one.
		if strings.Contains(err.Error(), "index.lock") {
			return drain, errors.New("attrib: index busy (another git is running); saved to " + dir + " but did not drain: " + err.Error())
		}
		return drain, err
	}
	return drain, nil
}

// IndexClean reports whether the shared index is empty — the property 🎯T466
// requires to hold after any agent stops.
func IndexClean(repoRoot string) (bool, error) {
	staged, err := StagedPaths(repoRoot)
	if err != nil {
		return false, err
	}
	return len(staged) == 0, nil
}
