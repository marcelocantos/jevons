// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// supervisorpolicy prints what supervisord's config says it does when a
// program exits (🎯T1086).
//
//	supervisorpolicy -program jevonsd (-conf PATH | -supervisord-pid PID)
//
// prints one line on success, exit 0:
//
//	autorestart=false autostart=false file=/opt/homebrew/etc/supervisor.d/jevonsd.ini
//
// and "policy unknown: <why>" on failure, exit 1.
//
// restart-jevonsd reads it before it signals the program's upgrade exit.
// supervisorctl cannot show a program's autorestart, and on 2026-10-20 a
// paused config (autorestart=false) meant nothing relaunched the daemon
// the script had just asked to exit. Go rather than shell because the
// answer lives in an ini file reached through [include] globs (the shared
// bash doctrine's structured-data rule).
//
// With -supervisord-pid the config is the -c argument of that running
// supervisord, read from ps: the file it actually loaded, not a guess at
// supervisord's search path.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/supervise"
)

func main() {
	program := flag.String("program", "jevonsd", "supervisord program name")
	conf := flag.String("conf", "", "supervisord config file")
	pid := flag.Int("supervisord-pid", 0, "read the config path from this running supervisord's command line")
	flag.Parse()

	path := *conf
	if path == "" && *pid > 0 {
		out, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(*pid)).Output()
		if err != nil {
			unknown(fmt.Sprintf("ps for supervisord pid %d: %v", *pid, err))
		}
		path = supervise.SupervisordConfFromArgs(strings.Fields(string(out)))
		if path == "" {
			unknown(fmt.Sprintf("supervisord pid %d was started without -c; its config path is not known", *pid))
		}
	}
	if path == "" {
		unknown("neither -conf nor -supervisord-pid given")
	}
	p, err := supervise.SupervisordProgramPolicy(path, *program)
	if err != nil {
		unknown(err.Error())
	}
	fmt.Println(p.String())
}

func unknown(why string) {
	fmt.Printf("policy unknown: %s\n", why)
	os.Exit(1)
}
