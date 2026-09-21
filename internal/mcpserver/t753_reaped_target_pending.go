package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/attrib"
	"github.com/marcelocantos/jevons/internal/poproactive"
	"github.com/marcelocantos/jevons/internal/reapverify"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// Record before removing the registry row: a failed durable write must not
// create a window in which the unattended sweep can duplicate landed work.
func (s *Server) recordReapedTargetPending(name, reason string) (*reapverify.Pending, error) {
	s.mu.Lock()
	store := s.reapVerify
	s.mu.Unlock()
	def := s.registry.Def(name)
	if store == nil || def == nil || def.TargetID == "" || def.WorkDir == "" {
		return nil, nil
	}
	if status, ok := targetfile.LoadTargetStatusFromCwd(def.WorkDir, def.TargetID); ok && targetfile.IsClosedStatus(status) {
		return nil, nil
	}
	root, err := attrib.RepoRoot(def.WorkDir)
	if err != nil {
		return nil, nil
	} // Non-repository work has no landed commits.
	log, err := attrib.Git(root, "log", "--regexp-ignore-case", "--fixed-strings", "--grep="+reapverify.NormalizeTargetID(def.TargetID), "--format=%H%x00%s%x00%b%x1e", "HEAD")
	if err != nil {
		return nil, err
	}
	p := reapverify.Pending{TargetID: def.TargetID, Seat: name, Owes: def.Parent, Repo: root, WorkDir: def.WorkDir, ReapReason: reason}
	if leaves, _, err := targetfile.LoadFrontierLeavesFromCwd(root); err == nil {
		for _, leaf := range leaves {
			if reapverify.NormalizeTargetID(leaf.ID) == reapverify.NormalizeTargetID(def.TargetID) {
				p.ForceEngageAtReap = poproactive.IsForceEngageTag(leaf.Tags)
			}
		}
	}
	if p.Owes == "" {
		p.Owes = defaultProductPOName
	}
	for _, row := range strings.Split(log, "\x1e") {
		parts := strings.SplitN(strings.TrimSpace(row), "\x00", 3)
		if len(parts) != 3 || !commitMentionsTarget(parts[1]+"\n"+parts[2], def.TargetID) {
			continue
		}
		files, err := attrib.Git(root, "diff-tree", "--root", "--no-commit-id", "--name-only", "-r", parts[0])
		if err != nil {
			return nil, err
		}
		implementation := false
		for _, path := range strings.Fields(files) {
			if path != "bullseye.yaml" {
				implementation = true
			}
		}
		if implementation {
			p.Commits = append(p.Commits, reapverify.Commit{SHA: parts[0], Subject: parts[1]})
		}
	}
	if len(p.Commits) == 0 {
		return nil, nil
	}
	if err := store.Record(p); err != nil {
		return nil, fmt.Errorf("record pending target: %w", err)
	}
	return &p, nil
}

func (s *Server) reapedTargetPending(target, workdir string, force bool) string {
	s.mu.Lock()
	store := s.reapVerify
	s.mu.Unlock()
	if store == nil {
		return ""
	}
	root, err := attrib.RepoRoot(workdir)
	if err != nil {
		root = workdir
	}
	p, ok := store.PendingFor(target, root)
	if !ok {
		return ""
	}
	// force-engage is the existing explicit decision to drive the target again.
	if !force && p.ForceEngageAtReap {
		p.ForceEngageAtReap = false
		if err := store.Record(p); err != nil {
			return fmt.Sprintf("pending target update failed: %v; %s", err, reapverify.FormatOwedDecisionNotice(p))
		}
	}
	if force && !p.ForceEngageAtReap {
		if err := store.Resolve(target, root); err == nil {
			return ""
		}
	}
	return reapverify.FormatOwedDecisionNotice(p)
}
