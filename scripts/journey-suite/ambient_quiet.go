package main

import (
	"os"

	"github.com/marcelocantos/jevons/internal/audit"
	"github.com/marcelocantos/jevons/internal/research"
	"github.com/marcelocantos/jevons/internal/rsi"
)

// quietAmbient turns the ambient research cycle, the RSI coach and the
// periodic audit off in an isolate's state dir before its daemon starts
// (🎯T1276). Left on, the research cycle briefs the overseer about three
// minutes after boot. That turn lands in the middle of whichever journey is
// running: J3 saw the overseer "working" before its cancel test, J30 and J45
// waited behind a brief. The audit runs a full scan on a paid model at boot
// (183 files on claude-fable-5 in one J51 isolate). No journey exercises
// these loops, and each pass is a paid turn.
func quietAmbient(stateDir string) error {
	rc := research.DefaultConfig()
	rc.Enabled = false
	rc.FeedEnabled = false
	if err := research.SaveConfig(stateDir, rc); err != nil {
		return err
	}
	cc := rsi.DefaultCoachConfig()
	cc.Enabled = false
	cc.RetroEnabled = false
	if err := rsi.SaveCoachConfig(stateDir, cc); err != nil {
		return err
	}
	ac := audit.DefaultConfig(stateDir, os.Getenv("HOME"))
	ac.Enabled = false
	return audit.SaveConfig(stateDir, ac)
}
