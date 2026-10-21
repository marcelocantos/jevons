// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Reading a supervisord program's restart policy (🎯T1086).
//
// restart-jevonsd's upgrade path asks supervisord's program to exit and
// counts on something to launch the replacement. On 2026-10-20 that
// program had autorestart=false, left by a runaway pause, so nothing did:
// the script waited 90 s for a daemon no one would start and left the
// development daemon down. supervisorctl cannot show a program's
// autorestart, so the policy comes from the config supervisord loaded.
//
// This reads the file as it is on disk now. supervisord applies an edit
// only on `supervisorctl update`, so what it holds in memory can differ.
// The restart script therefore treats this as the plan it logs, and still
// checks the program's state after the exit (EXITED or FATAL means it
// starts the program itself), whatever the file said.

// SupervisordAutorestart values, as supervisord spells them.
const (
	AutorestartTrue       = "true"
	AutorestartFalse      = "false"
	AutorestartUnexpected = "unexpected"
)

// supervisordDefaultAutorestart is supervisord's default when a program
// section does not say: relaunch only on an exit code not in exitcodes.
// An upgrade exit is expected (0), so the default does not relaunch it.
const supervisordDefaultAutorestart = AutorestartUnexpected

// ProgramPolicy is what supervisord's config says it does when the
// program exits.
type ProgramPolicy struct {
	Program     string
	File        string // the file holding [program:<Program>]
	Autorestart string // true | false | unexpected
	Autostart   string // true | false
}

// Relaunches reports whether supervisord relaunches the program after
// any exit, including the expected exit an upgrade bounce makes.
func (p ProgramPolicy) Relaunches() bool { return p.Autorestart == AutorestartTrue }

// String is the one line the restart script logs and parses.
func (p ProgramPolicy) String() string {
	return fmt.Sprintf("autorestart=%s autostart=%s file=%s", p.Autorestart, p.Autostart, p.File)
}

// SupervisordConfFromArgs returns the config file named by a supervisord
// command line (-c PATH, -cPATH, --configuration PATH or
// --configuration=PATH), or "" when it names none.
func SupervisordConfFromArgs(args []string) string {
	for i, a := range args {
		switch {
		case a == "-c" || a == "--configuration":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--configuration="):
			return strings.TrimPrefix(a, "--configuration=")
		case strings.HasPrefix(a, "-c") && len(a) > 2 && !strings.HasPrefix(a, "--"):
			return a[2:]
		}
	}
	return ""
}

// SupervisordProgramPolicy finds [program:<program>] in the supervisord
// config at conf, following its [include] files, and returns the restart
// policy that section declares.
func SupervisordProgramPolicy(conf, program string) (ProgramPolicy, error) {
	if conf == "" {
		return ProgramPolicy{}, fmt.Errorf("no supervisord config path")
	}
	if program == "" {
		return ProgramPolicy{}, fmt.Errorf("no program name")
	}
	main, err := parseINI(conf)
	if err != nil {
		return ProgramPolicy{}, err
	}
	files := []string{conf}
	if inc, ok := main["include"]; ok {
		here := filepath.Dir(conf)
		for _, pattern := range strings.Fields(inc["files"]) {
			pattern = strings.ReplaceAll(pattern, "%(here)s", here)
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(here, pattern)
			}
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return ProgramPolicy{}, fmt.Errorf("%s: include %q: %w", conf, pattern, err)
			}
			files = append(files, matches...)
		}
	}

	section := "program:" + program
	var found []ProgramPolicy
	for _, f := range files {
		sections := main
		if f != conf {
			if sections, err = parseINI(f); err != nil {
				return ProgramPolicy{}, err
			}
		}
		keys, ok := sections[section]
		if !ok {
			continue
		}
		p := ProgramPolicy{
			Program:     program,
			File:        f,
			Autorestart: supervisordDefaultAutorestart,
			Autostart:   "true",
		}
		if v, ok := keys["autorestart"]; ok {
			switch strings.ToLower(v) {
			case AutorestartTrue, AutorestartFalse, AutorestartUnexpected:
				p.Autorestart = strings.ToLower(v)
			default:
				return ProgramPolicy{}, fmt.Errorf("%s: [%s] autorestart=%q is not true, false or unexpected", f, section, v)
			}
		}
		if v, ok := keys["autostart"]; ok {
			p.Autostart = strings.ToLower(v)
		}
		found = append(found, p)
	}
	switch len(found) {
	case 0:
		return ProgramPolicy{}, fmt.Errorf("no [%s] in %s or its includes", section, conf)
	case 1:
		return found[0], nil
	default:
		return ProgramPolicy{}, fmt.Errorf("[%s] is defined in both %s and %s", section, found[0].File, found[1].File)
	}
}

// parseINI reads the subset of configparser syntax supervisord configs
// use: [section] headers, key=value or key: value, ';' and '#' comment
// lines, inline ' ;' comments, and indented continuation lines. Section
// and key names are case-folded as configparser folds keys.
func parseINI(path string) (map[string]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]map[string]string{}
	var section, lastKey string
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if raw[0] == ' ' || raw[0] == '\t' {
			if section != "" && lastKey != "" {
				out[section][lastKey] = strings.TrimSpace(out[section][lastKey] + " " + stripInlineComment(line))
			}
			continue
		}
		if strings.HasPrefix(line, "[") {
			end := strings.Index(line, "]")
			if end < 0 {
				return nil, fmt.Errorf("%s:%d: unterminated section header", path, n)
			}
			section = strings.TrimSpace(line[1:end])
			lastKey = ""
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			continue
		}
		if section == "" {
			return nil, fmt.Errorf("%s:%d: key outside any section", path, n)
		}
		i := strings.IndexAny(line, "=:")
		if i < 0 {
			return nil, fmt.Errorf("%s:%d: not key=value", path, n)
		}
		lastKey = strings.ToLower(strings.TrimSpace(line[:i]))
		out[section][lastKey] = stripInlineComment(strings.TrimSpace(line[i+1:]))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// stripInlineComment drops a " ;" comment, as supervisord does.
func stripInlineComment(v string) string {
	if i := strings.Index(v, " ;"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
