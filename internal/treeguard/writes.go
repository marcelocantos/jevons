package treeguard

import "slices"

// RepoWrites names the repo-relative paths a tool call writes, for the
// attribution feed (🎯T466). The guard half of this hook decides whether a
// write may proceed; this half only reports where it landed, so that the
// session's touches accumulate somewhere an operator can read after the
// worker is stopped.
//
// Same recognizers as the guard: the tool boundary's file_path for the
// mutating tools, ScanCommand for shell writes. Paths outside the repo are
// dropped — attribution answers for the shared clone, not for ~/.zshrc — and
// a shell path the recognizer cannot resolve statically is dropped rather
// than guessed at, exactly as the guard treats it.
func (e *Env) RepoWrites(p *Payload) []string {
	if p.ToolName == ToolBash {
		var out []string
		for _, w := range ScanCommand(p.ToolInput.Command) {
			abs := e.expand(w.Path)
			if abs == "" {
				continue
			}
			if _, rel, ok := e.resolve(abs); ok {
				out = append(out, rel)
			}
		}
		return out
	}
	if !slices.Contains(MutatingTools, p.ToolName) {
		return nil
	}
	if _, rel, ok := e.resolve(p.ToolInput.FilePath); ok {
		return []string{rel}
	}
	return nil
}
