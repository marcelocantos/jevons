// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package roles

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed builtin/*.md
var builtinFS embed.FS

// Catalog resolves roles: owner override > repo > builtin (🎯T511).
type Catalog struct {
	OwnerDir string // e.g. ~/.jevons/roles (override-wins)
	RepoDir  string // optional repo overlay
	// OverrideDir is an alias of OwnerDir for callers that use that name.
	OverrideDir string
}

func (c Catalog) ownerDir() string {
	if d := strings.TrimSpace(c.OwnerDir); d != "" {
		return d
	}
	return strings.TrimSpace(c.OverrideDir)
}

// Resolve returns the winning definition for name, or an error if unknown /
// malformed on the winning path.
func (c Catalog) Resolve(name string) (Def, error) {
	n := Normalize(name)
	if n == "" {
		return Def{}, fmt.Errorf("role name is required")
	}

	if dir := c.ownerDir(); dir != "" {
		m, err := loadDir(dir, SourceOwner)
		if err != nil {
			return Def{}, err
		}
		if d, ok := m[n]; ok {
			return d, nil
		}
	}
	if c.RepoDir != "" {
		m, err := loadDir(c.RepoDir, SourceRepo)
		if err != nil {
			return Def{}, err
		}
		if d, ok := m[n]; ok {
			return d, nil
		}
	}
	builtins, err := loadBuiltins()
	if err != nil {
		return Def{}, err
	}
	if d, ok := builtins[n]; ok {
		return d, nil
	}
	return Def{}, fmt.Errorf("unknown role %q", n)
}

// List returns every visible role (builtins plus overlays), override-wins
// per name. Sorted by name.
func (c Catalog) List() ([]Def, error) {
	merged := map[string]Def{}
	builtins, err := loadBuiltins()
	if err != nil {
		return nil, err
	}
	for k, v := range builtins {
		merged[k] = v
	}
	if c.RepoDir != "" {
		m, err := loadDir(c.RepoDir, SourceRepo)
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			merged[k] = v
		}
	}
	if dir := c.ownerDir(); dir != "" {
		m, err := loadDir(dir, SourceOwner)
		if err != nil {
			return nil, err
		}
		for k, v := range m {
			merged[k] = v
		}
	}
	names := make([]string, 0, len(merged))
	for k := range merged {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]Def, 0, len(names))
	for _, k := range names {
		out = append(out, merged[k])
	}
	return out, nil
}

// Delete removes an owner-override role file. Built-ins cannot be deleted.
// A role with liveInstances > 0 requires force.
func (c Catalog) Delete(name string, liveInstances int, force bool) error {
	if err := DeleteRefused(name, liveInstances, force); err != nil {
		return err
	}
	dir := c.ownerDir()
	if dir == "" {
		return fmt.Errorf("no owner roles dir configured")
	}
	n := Normalize(name)
	path := filepath.Join(dir, n+".md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Builtin returns the embedded definition (no overlays).
func Builtin(name string) (Def, error) {
	builtins, err := loadBuiltins()
	if err != nil {
		return Def{}, err
	}
	d, ok := builtins[Normalize(name)]
	if !ok {
		return Def{}, fmt.Errorf("unknown built-in role %q", name)
	}
	return d, nil
}

func loadBuiltins() (map[string]Def, error) {
	out := map[string]Def{}
	err := fs.WalkDir(builtinFS, "builtin", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		raw, err := builtinFS.ReadFile(p)
		if err != nil {
			return err
		}
		hint := strings.TrimSuffix(path.Base(p), path.Ext(p))
		def, err := Parse(string(raw), hint)
		if err != nil {
			return fmt.Errorf("builtin %s: %w", p, err)
		}
		def.Source = SourceBuiltin
		def.Path = p
		out[def.Name] = def
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
