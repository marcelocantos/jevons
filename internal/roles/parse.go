// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package roles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFile parses a role markdown file with optional YAML frontmatter.
// Malformed frontmatter is a hard error (never a silent reset).
func ParseFile(path, source string, raw []byte) (Def, error) {
	nameHint := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	d, err := Parse(string(raw), nameHint)
	if err != nil {
		return Def{}, fmt.Errorf("%s: %w", path, err)
	}
	d.Source = source
	d.Path = path
	return d, nil
}

// Parse parses role markdown. Frontmatter is optional; when present it must
// be valid YAML between leading --- fences. Accepts name= or role= for the
// role id.
func Parse(raw, nameHint string) (Def, error) {
	body := raw
	meta := map[string]any{}
	if strings.HasPrefix(strings.TrimLeft(raw, "\ufeff"), "---") {
		rest := strings.TrimLeft(raw, "\ufeff")
		parts := strings.SplitN(rest, "---", 3)
		if len(parts) < 3 {
			return Def{}, fmt.Errorf("malformed frontmatter: missing closing ---")
		}
		fm := strings.TrimSpace(parts[1])
		if fm != "" {
			if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
				return Def{}, fmt.Errorf("malformed frontmatter: %w", err)
			}
		}
		body = parts[2]
		if strings.HasPrefix(body, "\n") {
			body = body[1:]
		}
	}

	name := strMeta(meta, "role")
	if name == "" {
		name = strMeta(meta, "name")
	}
	if name == "" {
		name = nameHint
	}
	name = Normalize(name)
	if name == "" {
		return Def{}, fmt.Errorf("role name is required (frontmatter role=/name= or filename)")
	}

	purpose := strMeta(meta, "purpose")
	if purpose == "" {
		purpose = "work"
	}
	readonly := boolMeta(meta, "readonly")
	if name == Auditor {
		readonly = true
	}

	return Def{
		Name:     name,
		Purpose:  purpose,
		ReadOnly: readonly,
		Summary:  strMeta(meta, "summary"),
		Body:     strings.TrimSpace(body),
	}, nil
}

func strMeta(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func boolMeta(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

// loadDir reads *.md role files from dir. Missing dir is empty, not an error.
// Malformed files fail closed.
func loadDir(dir, source string) (map[string]Def, error) {
	out := map[string]Def{}
	if strings.TrimSpace(dir) == "" {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		d, err := ParseFile(path, source, raw)
		if err != nil {
			return nil, err
		}
		out[d.Name] = d
	}
	return out, nil
}
