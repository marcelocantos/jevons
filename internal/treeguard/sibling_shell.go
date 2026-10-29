package treeguard

// ShellWriteSite is one write a shell command may perform, with the working
// directory that segment runs in (🎯T1151.8). WorkDir "." means the seat's
// project root; Target empty means the write lands in WorkDir itself (make,
// go build without -o). Paths are unresolved tokens for hook.go to expand.
type ShellWriteSite struct {
	WorkDir string
	Target  string
	Form    string
}

// ShellWriteSites walks a command's &&/; segments, tracking `cd`, and reports
// write targets including go build/install and make (🎯T1151.8).
func ShellWriteSites(command string) []ShellWriteSite {
	code, body := splitHeredocs(command)
	var sites []ShellWriteSite
	workDir := "."
	for _, seg := range segments(lex(code)) {
		redirTarget := make(map[int]bool)
		for i, t := range seg {
			if t.kind == tokRedirOut && i+1 < len(seg) && seg[i+1].kind == tokWord {
				redirTarget[i+1] = true
			}
		}
		words := stripPrefixes(wordsOf(seg, redirTarget))
		if len(words) >= 2 && words[0] == "cd" {
			if args := positional(words[1:]); len(args) > 0 {
				workDir = args[len(args)-1]
			}
			continue
		}
		var writes []CommandWrite
		scanSegment(seg, body, &writes)
		for _, w := range writes {
			target := w.Path
			switch w.Form {
			case FormGoBuildCwd, FormMake:
				target = ""
			}
			sites = append(sites, ShellWriteSite{WorkDir: workDir, Target: target, Form: w.Form})
		}
	}
	return sites
}
