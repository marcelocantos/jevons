package attrib

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// LookupAgent names the agent currently running a session, from the live
// registry alone.
//
// Only the live registry, because this runs on every mutating tool call in the
// fleet and the backups exist for a different question: LoadRoster's job is to
// name an agent that has since been removed, which is a read-time concern.
// At record time the agent is by definition still running, so agents.json has
// it, and folding in every backup would re-read the whole registry history on
// each tool call of every worker.
//
// An empty answer is not an error. Records keep their session id regardless,
// and Resolve fills the name in later from the fuller roster.
func LookupAgent(session string) string {
	if session == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".jevons", "agents.json"))
	if err != nil {
		return ""
	}
	var entries []RosterEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return ""
	}
	for _, e := range entries {
		if e.SessionID == session {
			return e.Name
		}
	}
	return ""
}

// Observe records one tool call's writes.
//
// Best-effort by construction: the caller is a hook on the fleet's critical
// path, and a worker must never lose a tool call because the attribution store
// could not be written. A missing record degrades to one unattributed path in
// the pile — the state the whole fleet was in before this package existed —
// whereas a failing hook degrades to a worker that cannot write at all.
func Observe(session, agent, tool string, relPaths []string, now time.Time) error {
	if session == "" || len(relPaths) == 0 {
		return nil
	}
	if agent == "" {
		agent = LookupAgent(session)
	}
	seen := map[string]bool{}
	records := make([]Record, 0, len(relPaths))
	for _, p := range relPaths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		records = append(records, Record{
			Session: session,
			Agent:   agent,
			Path:    p,
			Tool:    tool,
			At:      now.UTC(),
			Via:     ViaHook,
		})
	}
	store := &Store{Root: DefaultRoot()}
	return store.Append(session, records)
}
