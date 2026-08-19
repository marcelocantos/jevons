package attrib

import (
	"log/slog"
	"time"
)

// DrainOnStop is the chokepoint every agent-stop path calls: it empties the
// shared index of the repo containing workdir, saving what it removed first
// (🎯T466 acceptance: `git diff --cached --name-only` is clean after any agent
// stops).
//
// Best-effort by contract, like the hook feed: a stop must never fail because
// the drain could not run. A workdir outside any git repo is the normal case
// for non-repo agents and returns nil silently; a real drain failure is logged
// and swallowed, because the alternative — a stop that errors — would leave
// the agent running for the sake of index hygiene, which inverts the priority.
// The failure is not silent to the operator: the staged pile it failed to
// drain is exactly what `attrib list` reports.
func DrainOnStop(workdir, session, agent string) *Drain {
	if workdir == "" {
		return nil
	}
	root, err := RepoRoot(workdir)
	if err != nil {
		return nil // not a git repo: nothing to drain
	}
	d, err := DrainIndex(root, DefaultRoot(), session, agent, DrainReasonStop, time.Now())
	if err != nil {
		slog.Warn("attrib: index drain on agent stop failed",
			"component", "attrib", "agent", agent, "repo", root, "err", err)
		return d
	}
	if d != nil {
		slog.Info("attrib: shared index drained on agent stop",
			"component", "attrib", "agent", agent, "repo", root,
			"paths", len(d.Paths), "saved", d.Dir)
	}
	return d
}
