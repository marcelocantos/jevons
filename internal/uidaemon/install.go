// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package uidaemon

import (
	"fmt"
	"os"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// Install writes the React probe plist and asks launchd to hold it.
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
	if err := writePlist(reactPath, ReactPlistXML(spec)); err != nil {
		return err
	}
	if err := supervise.LoadAgent(reactPath, ReactLabel); err != nil {
		return fmt.Errorf("uidaemon: load %s: %w", ReactLabel, err)
	}
	return nil
}

// Uninstall unloads the React probe. A missing job is not an error.
func Uninstall() error {
	return supervise.UnloadAgent(ReactLabel)
}
