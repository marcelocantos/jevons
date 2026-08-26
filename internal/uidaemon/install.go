// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package uidaemon

import (
	"fmt"
	"os"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// Install writes both UI plists and asks launchd to hold them.
// The vanilla job binds DailyVanillaPort; if that port is still the
// in-process sidecar, bootstrap will fail to listen and KeepAlive will
// retry — callers should bounce daily with -vanilla-port 0 first.
func Install(spec Spec) error {
	if spec.Binary == "" {
		return fmt.Errorf("uidaemon: binary path is required")
	}
	if spec.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		spec.Home = home
	}
	if spec.StateDir == "" {
		spec.StateDir = os.Getenv("HOME") + "/.jevons"
	}
	if err := os.MkdirAll(spec.StateDir, 0o755); err != nil {
		return err
	}

	reactPath := reactPlistPath(spec.Home)
	vanillaPath := vanillaPlistPath(spec.Home)
	if err := writePlist(reactPath, ReactPlistXML(spec)); err != nil {
		return err
	}
	if err := writePlist(vanillaPath, VanillaPlistXML(spec)); err != nil {
		return err
	}
	if err := supervise.LoadAgent(reactPath, ReactLabel); err != nil {
		return fmt.Errorf("uidaemon: load %s: %w", ReactLabel, err)
	}
	if err := supervise.LoadAgent(vanillaPath, VanillaLabel); err != nil {
		return fmt.Errorf("uidaemon: load %s: %w", VanillaLabel, err)
	}
	return nil
}

// Uninstall unloads both UI jobs. Missing jobs are not an error.
func Uninstall() error {
	if err := supervise.UnloadAgent(ReactLabel); err != nil {
		return err
	}
	return supervise.UnloadAgent(VanillaLabel)
}
