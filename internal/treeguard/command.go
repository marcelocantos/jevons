package treeguard

// Non-tool write paths (🎯T391).
//
// 🎯T376 put compare-and-swap on the Write/Edit tool boundary, which is only
// one of the ways a worker mutates a file. `sed -i`, a python heredoc, a `>`
// redirection and `git checkout -- <path>` all reach the same bytes and none
// of them reaches PreToolUse, so the loss they cause is silent again — exactly
// the class 🎯T376 exists to eliminate.
//
// A shell command is not statically decidable, so this is a recognizer, not a
// parser: it names the write FORMS that a fleet worker actually uses and
// reports the paths they target. Two properties keep the heuristic honest:
//
//   - It never has to be right about the path. A recognized target is only
//     acted on after the caller matches it against the guarded set, so an
//     over-inclusive read of a `sed` script argument costs nothing.
//   - It must not be over-inclusive about the FORM. A refusal on a read-only
//     command teaches workers to disable the guard, which is worse than the
//     gap. `grep`, `cat`, `git diff` and a `sed` without `-i` are allowed by
//     construction, and command_test.go pins that direction as hard as it
//     pins detection.
//
// Everything here is pure: no filesystem, no environment. The caller resolves
// paths against the repo root (hook.go).

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Write forms, named so a refusal can tell the worker which construct was
// recognized rather than quoting the whole command back at it.
const (
	FormRedirect    = "shell redirection"
	FormInPlace     = "in-place editor (-i)"
	FormTee         = "tee"
	FormGitRestore  = "git checkout/restore"
	FormCopyDest    = "copy/move destination"
	FormTruncate    = "truncate"
	FormPatch       = "patch"
	FormRemove      = "removal"
	FormDD          = "dd of="
	FormScriptWrite = "interpreter write"
)

// CommandWrite is one path a shell command appears to mutate, with the form
// that makes it a write.
type CommandWrite struct {
	// Path is the token as the command wrote it: possibly relative, possibly
	// carrying $CLAUDE_PROJECT_DIR or ~. The caller resolves it.
	Path string
	Form string
}

// inPlaceEditors rewrite their argument files when given -i.
var inPlaceEditors = []string{"sed", "gsed", "perl", "ruby", "gawk"}

// destOnlyWriters write their LAST path argument and only read the rest.
var destOnlyWriters = []string{"mv", "cp", "install", "rsync", "ln"}

// argWriters rewrite every path argument they are given.
var argWriters = []string{"tee", "truncate", "patch", "sponge", "ed", "ex", "rm"}

// interpreters get their whole segment (and any heredoc body) scanned for
// write intent, because the path they touch is inside the program text.
var interpreters = []string{"python", "python3", "node", "ruby", "perl", "php", "deno", "bun"}

// commandPrefixes wrap another command without changing what it writes.
var commandPrefixes = []string{"sudo", "env", "nohup", "time", "command", "builtin", "exec", "xargs", "nice", "stdbuf"}

// scriptWrite recognizes write intent inside interpreter program text. It is
// deliberately loose — inside a python/node one-liner that already names a
// guarded path, an "open(…, 'w')" shape is the only reason to name it.
var scriptWrite = regexp.MustCompile(
	`(?i)open\s*\([^)]*['"][rwab+x]*w|write_text|write_bytes|writeFileSync|writeFile|\.write\(|File\.write|os\.replace|shutil\.(copy|move)|fs\.rename`)

// quotedLiteral pulls string literals out of interpreter program text, where
// the path is data rather than a shell word.
var quotedLiteral = regexp.MustCompile(`['"]([^'"\n]{2,})['"]`)

// ScanCommand reports every path the command appears to mutate. Paths are
// returned as written; the caller resolves and filters them against the
// guarded set. Order follows the command, and duplicates are collapsed on
// (path, form).
func ScanCommand(command string) []CommandWrite {
	code, body := splitHeredocs(command)
	var out []CommandWrite
	for _, seg := range segments(lex(code)) {
		scanSegment(seg, body, &out)
	}
	return out
}

// scanSegment applies the form rules to one pipeline stage. The order is the
// policy: a redirection counts wherever it appears, then the command word
// decides what its arguments mean.
func scanSegment(toks []token, heredoc string, out *[]CommandWrite) {
	redirTarget := make(map[int]bool)
	for i, t := range toks {
		if t.kind != tokRedirOut {
			continue
		}
		if i+1 < len(toks) && toks[i+1].kind == tokWord {
			redirTarget[i+1] = true
			add(out, toks[i+1].text, FormRedirect)
		}
	}

	var words []string
	for i, t := range toks {
		if t.kind == tokWord && !redirTarget[i] {
			words = append(words, t.text)
		}
	}
	words = stripPrefixes(words)
	if len(words) == 0 {
		return
	}
	cmd := filepath.Base(words[0])
	args := words[1:]

	switch {
	case slices.Contains(inPlaceEditors, cmd):
		if !hasInPlaceFlag(args) {
			return // a read-only sed/perl is the common case and stays allowed
		}
		for _, a := range positional(args) {
			add(out, a, FormInPlace)
		}
	case cmd == "tee":
		for _, a := range positional(args) {
			add(out, a, FormTee)
		}
	case cmd == "git":
		scanGit(args, out)
	case slices.Contains(destOnlyWriters, cmd):
		if p := positional(args); len(p) >= 2 {
			add(out, p[len(p)-1], FormCopyDest)
		}
	case cmd == "dd":
		for _, a := range args {
			if of, ok := strings.CutPrefix(a, "of="); ok {
				add(out, of, FormDD)
			}
		}
	case slices.Contains(argWriters, cmd):
		form := FormTruncate
		switch cmd {
		case "patch", "ed", "ex", "sponge":
			form = FormPatch
		case "rm":
			form = FormRemove
		}
		for _, a := range positional(args) {
			add(out, a, form)
		}
	case slices.Contains(interpreters, cmd):
		text := strings.Join(words, " ") + "\n" + heredoc
		if !scriptWrite.MatchString(text) {
			return
		}
		for _, a := range positional(args) {
			add(out, a, FormScriptWrite)
		}
		for _, m := range quotedLiteral.FindAllStringSubmatch(text, -1) {
			add(out, m[1], FormScriptWrite)
		}
	}
}

// scanGit handles the one git subcommand family that rewrites working-tree
// content. `git add` is deliberately absent: the shared INDEX is 🎯T377's
// problem, and refusing staging here would duplicate that guard badly.
func scanGit(args []string, out *[]CommandWrite) {
	var sub string
	var rest []string
	for _, a := range args {
		if sub == "" {
			if strings.HasPrefix(a, "-") {
				continue
			}
			sub = a
			continue
		}
		rest = append(rest, a)
	}
	switch sub {
	case "checkout", "restore", "clean", "stash":
		for _, a := range positional(rest) {
			add(out, a, FormGitRestore)
		}
	}
}

func add(out *[]CommandWrite, path, form string) {
	path = strings.TrimSpace(path)
	if path == "" || path == "--" || strings.HasPrefix(path, "-") {
		return
	}
	for _, w := range *out {
		if w.Path == path && w.Form == form {
			return
		}
	}
	*out = append(*out, CommandWrite{Path: path, Form: form})
}

// positional drops flags. A flag's VALUE is kept: it may be a path (`-o out`),
// and a non-path value simply fails to match the guarded set.
func positional(args []string) []string {
	var out []string
	for _, a := range args {
		if a == "--" || strings.HasPrefix(a, "-") {
			continue
		}
		out = append(out, a)
	}
	return out
}

func hasInPlaceFlag(args []string) bool {
	for _, a := range args {
		if a == "--in-place" || strings.HasPrefix(a, "--in-place=") {
			return true
		}
		// -i, -i'', -i.bak, and bundles like -ri.
		if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a[1:], 'i') {
			return true
		}
	}
	return false
}

func stripPrefixes(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		if strings.Contains(w, "=") && !strings.HasPrefix(w, "-") &&
			!strings.ContainsAny(w[:strings.Index(w, "=")], "/. ") {
			words = words[1:] // FOO=bar cmd …
			continue
		}
		if slices.Contains(commandPrefixes, filepath.Base(w)) {
			words = words[1:]
			continue
		}
		break
	}
	return words
}

// --- lexing -----------------------------------------------------------------

type tokKind int

const (
	tokWord tokKind = iota
	tokRedirOut
	tokSep
)

type token struct {
	kind tokKind
	text string
}

// lex splits a command into words, output redirections and separators,
// respecting quoting. It is not a shell: it knows just enough to keep a
// quoted `;` from splitting a command and a `2>&1` from reading as a file.
func lex(s string) []token {
	var toks []token
	var cur strings.Builder
	started := false
	flush := func() {
		if started {
			toks = append(toks, token{kind: tokWord, text: cur.String()})
			cur.Reset()
			started = false
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '\'', '"':
			started = true
			quote := c
			for i++; i < len(runes) && runes[i] != quote; i++ {
				if runes[i] == '\\' && quote == '"' && i+1 < len(runes) {
					i++
				}
				cur.WriteRune(runes[i])
			}
		case '\\':
			if i+1 < len(runes) {
				i++
				started = true
				cur.WriteRune(runes[i])
			}
		case ' ', '\t':
			flush()
		case ';', '\n', '&', '|':
			// `&>file` is a redirection; `2>&1`'s `&` is consumed below.
			if c == '&' && i+1 < len(runes) && runes[i+1] == '>' {
				flush()
				i++
				if i+1 < len(runes) && runes[i+1] == '>' {
					i++
				}
				toks = append(toks, token{kind: tokRedirOut})
				continue
			}
			flush()
			if (c == '&' || c == '|') && i+1 < len(runes) && runes[i+1] == c {
				i++
			}
			toks = append(toks, token{kind: tokSep})
		case '>':
			// A bare `>` after a digit is that fd's redirection: 2>err.log.
			flush()
			if i+1 < len(runes) && (runes[i+1] == '>' || runes[i+1] == '|') {
				i++
			}
			if i+1 < len(runes) && runes[i+1] == '&' {
				// 2>&1 — a descriptor dup, not a file.
				i += 2
				continue
			}
			toks = append(toks, token{kind: tokRedirOut})
		case '<':
			flush()
			if i+1 < len(runes) && runes[i+1] == '<' {
				i++
				if i+1 < len(runes) && runes[i+1] == '-' {
					i++
				}
			}
			// The next word is an input source or a heredoc tag, never a
			// write target: consume it.
			for i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\t') {
				i++
			}
			for i+1 < len(runes) && !strings.ContainsRune(" \t\n;|&", runes[i+1]) {
				i++
			}
		default:
			started = true
			cur.WriteRune(c)
		}
	}
	flush()
	return toks
}

func segments(toks []token) [][]token {
	var out [][]token
	cur := []token{}
	for _, t := range toks {
		if t.kind == tokSep {
			if len(cur) > 0 {
				out = append(out, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, t)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// Go's RE2 has no backreferences, so the quote around a heredoc tag cannot be
// required to match itself. Accepting an unbalanced quote costs nothing here:
// the tag is only used to find where the body ends.
var heredocStart = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)

// splitHeredocs returns the command with heredoc BODIES removed, plus those
// bodies concatenated. Bodies are unquoted text that would otherwise lex as
// commands; keeping them aside lets the interpreter rule read the program
// without letting a `;` inside it split the command.
func splitHeredocs(command string) (code, body string) {
	lines := strings.Split(command, "\n")
	var codeLines, bodyLines []string
	for i := 0; i < len(lines); i++ {
		codeLines = append(codeLines, lines[i])
		m := heredocStart.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		term := m[1]
		for i++; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == term {
				break
			}
			bodyLines = append(bodyLines, lines[i])
		}
	}
	return strings.Join(codeLines, "\n"), strings.Join(bodyLines, "\n")
}
