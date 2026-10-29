// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/gomodreplace"
)

// ErrMarcelocantosReplace is returned when a worker branch tip's go.mod
// carries a replace of a github.com/marcelocantos/… module.
var ErrMarcelocantosReplace = errors.New("go.mod commits a replace of a github.com/marcelocantos module")

// refuseMarcelocantosReplaceInMod reads go.mod at commit in workdir and
// refuses when it commits a marcelocantos replace.
func refuseMarcelocantosReplaceInMod(workdir, commit, agentName string) error {
	body, err := gitOut(workdir, "show", commit+":go.mod")
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "does not exist in") || strings.Contains(msg, "exists on disk, but not in") {
			return nil // no go.mod at this commit — not a Go landing concern
		}
		return fmt.Errorf("integrate: %s: read go.mod at %s: %w", agentName, short(commit), err)
	}
	if lines := gomodreplace.MarcelocantosReplaceLines(body); len(lines) > 0 {
		return fmt.Errorf("integrate: %s: tip %s go.mod: %s: %w",
			agentName, short(commit), strings.TrimSpace(lines[0]), ErrMarcelocantosReplace)
	}
	return nil
}
