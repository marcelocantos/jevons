package attrib

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Kind classifies how a dirty path has to be handled, because git cannot undo
// all three the same way.
type Kind string

const (
	// KindTracked exists in HEAD: `git restore` puts it back, per path.
	KindTracked Kind = "tracked"
	// KindStagedNew is a brand-new file already added to the shared index. It
	// is not in HEAD, so `git restore --source=HEAD` has nothing to restore it
	// FROM and errors; it has to be unstaged and removed instead.
	KindStagedNew Kind = "staged-new"
	// KindUntracked git has never seen. Only a copy saves it — no git object
	// exists anywhere, which is why discard without save is refused outright.
	KindUntracked Kind = "untracked"
)

// SavedSlice is the durable copy of one agent's work, written before anything
// is undone.
type SavedSlice struct {
	Agent     string            `json:"agent"`
	Dir       string            `json:"dir"`
	At        time.Time         `json:"at"`
	RepoRoot  string            `json:"repo_root"`
	HeadSHA   string            `json:"head_sha"`
	Kinds     map[string]Kind   `json:"kinds"`
	Patch     string            `json:"patch,omitempty"`
	IndexDiff string            `json:"index_patch,omitempty"`
	Copies    map[string]string `json:"copies,omitempty"`
}

// patchName and indexPatchName sit beside the manifest so an operator can
// reapply by hand with `git apply` and nothing else.
const (
	patchName      = "slice.patch"
	indexPatchName = "index.patch"
	manifestName   = "manifest.json"
	copiesDir      = "files"
)

// Save writes an agent's slice to outRoot before anything touches the tree.
//
// Everything is captured twice over: a patch against HEAD for what git can
// express, and byte copies for untracked files, which no git object anywhere
// would otherwise hold. The staged state is captured separately, because
// draining the index is itself a change worth being able to undo.
func Save(repoRoot, outRoot, agent string, paths []string, now time.Time) (*SavedSlice, error) {
	if len(paths) == 0 {
		return nil, errors.New("attrib: nothing to save for " + agent)
	}
	head, err := Git(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(outRoot, "slices", sanitize(agent), now.UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	saved := &SavedSlice{
		Agent:    agent,
		Dir:      dir,
		At:       now.UTC(),
		RepoRoot: repoRoot,
		HeadSHA:  trimNewline(head),
		Kinds:    map[string]Kind{},
		Copies:   map[string]string{},
	}

	var gitKnown []string
	for _, p := range paths {
		switch {
		case InHEAD(repoRoot, p):
			saved.Kinds[p] = KindTracked
			gitKnown = append(gitKnown, p)
		case isStaged(repoRoot, p):
			saved.Kinds[p] = KindStagedNew
			gitKnown = append(gitKnown, p)
		default:
			saved.Kinds[p] = KindUntracked
		}
	}

	if len(gitKnown) > 0 {
		patch, err := Git(repoRoot, append([]string{"diff", "HEAD", "--binary", "--"}, gitKnown...)...)
		if err != nil {
			return nil, err
		}
		if patch != "" {
			if err := os.WriteFile(filepath.Join(dir, patchName), []byte(patch), 0o644); err != nil {
				return nil, err
			}
			saved.Patch = patchName
		}
		index, err := Git(repoRoot, append([]string{"diff", "--cached", "--binary", "--"}, gitKnown...)...)
		if err != nil {
			return nil, err
		}
		if index != "" {
			if err := os.WriteFile(filepath.Join(dir, indexPatchName), []byte(index), 0o644); err != nil {
				return nil, err
			}
			saved.IndexDiff = indexPatchName
		}
	}

	// Untracked and staged-new files are copied whole. A patch can express them
	// too, but a copy survives the patch failing to apply against a tree that
	// has moved on — and by the time anyone reads this directory, it has.
	for p, kind := range saved.Kinds {
		if kind == KindTracked {
			continue
		}
		src := filepath.Join(repoRoot, filepath.FromSlash(p))
		data, err := os.ReadFile(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		dst := filepath.Join(dir, copiesDir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return nil, err
		}
		saved.Copies[p] = filepath.Join(copiesDir, p)
	}

	manifest, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), manifest, 0o644); err != nil {
		return nil, err
	}
	return saved, nil
}

// Discard undoes one agent's paths and nothing else.
//
// It refuses without a Save of the same paths. Not as ceremony: for an
// untracked file there is no git object to recover from, so an unsaved discard
// is not reversible by any means at all — which is the property that makes
// "discard that agent's slice" safe to offer in the first place.
//
// Each path is restored individually. `git checkout -- .` would be one command
// and would destroy the other N-1 agents' work in the same breath; that bulk
// form is exactly what 🎯T466 exists to make unnecessary.
func Discard(repoRoot string, paths []string, saved *SavedSlice) error {
	if saved == nil {
		return errors.New("attrib: refusing to discard without a saved slice")
	}
	for _, p := range paths {
		if _, ok := saved.Kinds[p]; !ok {
			return errors.New("attrib: refusing to discard " + p + ": not in the saved slice")
		}
	}
	for _, p := range paths {
		if _, ok := RelPath(repoRoot, filepath.Join(repoRoot, filepath.FromSlash(p))); !ok {
			return errors.New("attrib: refusing to discard " + p + ": outside the repo")
		}
		switch saved.Kinds[p] {
		case KindTracked:
			if _, err := Git(repoRoot, "restore", "--source=HEAD", "--staged", "--worktree", "--", p); err != nil {
				return err
			}
		case KindStagedNew:
			if _, err := Git(repoRoot, "rm", "--force", "--quiet", "--cached", "--", p); err != nil {
				return err
			}
			if err := removeIfExists(filepath.Join(repoRoot, filepath.FromSlash(p))); err != nil {
				return err
			}
		case KindUntracked:
			if err := removeIfExists(filepath.Join(repoRoot, filepath.FromSlash(p))); err != nil {
				return err
			}
		}
	}
	return nil
}

// Restore puts a saved slice back into a tree: the patch for what git tracked,
// the copies for what it did not.
func Restore(repoRoot string, saved *SavedSlice) error {
	if saved == nil {
		return errors.New("attrib: nothing to restore")
	}
	for p, rel := range saved.Copies {
		data, err := os.ReadFile(filepath.Join(saved.Dir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		dst := filepath.Join(repoRoot, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	if saved.Patch == "" {
		return nil
	}
	_, err := Git(repoRoot, "apply", "--3way", filepath.Join(saved.Dir, saved.Patch))
	return err
}

// LoadSaved reads a manifest written by Save.
func LoadSaved(dir string) (*SavedSlice, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return nil, err
	}
	var s SavedSlice
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	s.Dir = dir
	return &s, nil
}

func isStaged(repoRoot, path string) bool {
	out, err := Git(repoRoot, "ls-files", "--cached", "-z", "--", path)
	return err == nil && out != ""
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
