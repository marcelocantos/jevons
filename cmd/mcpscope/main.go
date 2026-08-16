// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command mcpscope answers, from inside a session that has no fleet tools,
// which of the two indistinguishable situations it is in (🎯T464): the daemon
// is down, or the registration simply does not reach this working directory.
//
// It is a binary rather than an MCP tool for the reason the target exists: an
// agent that has lost every jevons_* tool cannot call one to ask why. Bash is
// the only channel left, so the answer has to arrive through it.
//
//	bin/mcpscope diagnose    # what is wrong, in a sentence, and an exit code
//	bin/mcpscope ensure      # register jevonsmcp user-scoped so it follows the agent
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/marcelocantos/jevons/internal/mcpscope"
)

// Exit codes. The verdict is in the text, but a caller scripting around this
// should not have to grep for it, and a shell that only sees a status must not
// be able to confuse "up but out of scope" with "down".
const (
	exitHealthy    = 0 // ok, or local_only: tools are present in this session
	exitUsage      = 2
	exitOutOfScope = 3 // daemon UP, registration does not reach this directory
	exitDown       = 4 // probe refused: a real control-plane outage
	exitUnknown    = 5 // probe settled nothing; no outage may be claimed
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitUsage)
	}
	switch os.Args[1] {
	case "diagnose":
		os.Exit(diagnose(os.Args[2:]))
	case "ensure":
		os.Exit(ensure(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "mcpscope: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(exitUsage)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mcpscope — is the control plane down, or is this directory out of scope? (🎯T464)

  mcpscope diagnose [-server NAME] [-endpoint URL] [-cwd DIR] [-json]
      Probe the endpoint, read the Claude config, and print which of the two
      situations this session is in. Exit 0 healthy, 3 out of scope (daemon
      UP), 4 down, 5 undetermined.

  mcpscope ensure [-server NAME] [-endpoint URL] [-n]
      Register the server in the user scope of ~/.claude.json, so fleet
      control follows the agent instead of the directory it started in.
      Does not write when the entry is already correct. -n reports what it
      would do.
`)
}

func diagnose(argv []string) int {
	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	server := fs.String("server", mcpscope.ServerName, "MCP server name to look for")
	endpoint := fs.String("endpoint", mcpscope.DefaultEndpoint, "endpoint to probe")
	cwd := fs.String("cwd", "", "working directory to diagnose (default: this process's)")
	asJSON := fs.Bool("json", false, "emit the whole diagnosis as JSON")
	_ = fs.Parse(argv)

	dir := *cwd
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	configPath := mcpscope.ConfigPath()
	if configPath == "" {
		fmt.Fprintf(os.Stderr, "mcpscope: %v — registration is undetermined, which is not evidence of an outage\n", mcpscope.ErrNoConfigPath)
		return exitUnknown
	}

	d := mcpscope.DiagnoseSession(configPath, *server, dir, *endpoint)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(d)
	} else {
		fmt.Printf("verdict: %s\n%s\n", d.Verdict, d.Message)
		if d.Verdict == mcpscope.VerdictOutOfScope && len(d.LocalDirs) > 0 {
			fmt.Printf("\n%s is registered for %d other working director%s, including:\n",
				*server, len(d.LocalDirs), plural(len(d.LocalDirs)))
			for _, dir := range d.LocalDirs {
				fmt.Printf("  %s\n", dir)
			}
			fmt.Printf("\nFix: mcpscope ensure -server %s -endpoint %s\n", *server, *endpoint)
		}
	}
	switch {
	case d.Verdict.Healthy():
		return exitHealthy
	case d.Verdict == mcpscope.VerdictOutOfScope:
		return exitOutOfScope
	case d.Verdict.ClaimsControlPlaneDown():
		return exitDown
	default:
		return exitUnknown
	}
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func ensure(argv []string) int {
	fs := flag.NewFlagSet("ensure", flag.ExitOnError)
	server := fs.String("server", mcpscope.ServerName, "MCP server name to register")
	endpoint := fs.String("endpoint", mcpscope.DefaultEndpoint, "endpoint the server is served on")
	dry := fs.Bool("n", false, "report what would change without writing")
	_ = fs.Parse(argv)

	configPath := mcpscope.ConfigPath()
	if configPath == "" {
		fmt.Fprintf(os.Stderr, "mcpscope: %v\n", mcpscope.ErrNoConfigPath)
		return exitUnknown
	}
	entry := mcpscope.HTTPEntry(strings.TrimSpace(*endpoint))

	if *dry {
		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcpscope: read %s: %v\n", configPath, err)
			return exitUnknown
		}
		_, changed, err := mcpscope.EnsureUserScope(data, *server, entry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcpscope: %v\n", err)
			return exitUnknown
		}
		if changed {
			fmt.Printf("would register %s = %s in the user scope of %s\n", *server, entry.URL, configPath)
		} else {
			fmt.Printf("%s is already registered as %s in the user scope of %s\n", *server, entry.URL, configPath)
		}
		return exitHealthy
	}

	changed, err := mcpscope.WriteEnsure(configPath, *server, entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcpscope: %v\n", err)
		return exitUnknown
	}
	if changed {
		fmt.Printf("registered %s = %s in the user scope of %s — restart a session to pick it up\n",
			*server, entry.URL, configPath)
	} else {
		fmt.Printf("%s already registered as %s in the user scope of %s; nothing written\n",
			*server, entry.URL, configPath)
	}
	return exitHealthy
}
