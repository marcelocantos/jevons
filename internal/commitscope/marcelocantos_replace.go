// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"github.com/marcelocantos/jevons/internal/gomodreplace"
)

// MarcelocantosReplaceCommitRefusal reports whether staging go.mod adds or
// changes a replace of a github.com/marcelocantos/… module (🎯T448 / 🎯T1151.6).
func MarcelocantosReplaceCommitRefusal(head, staged []byte) (refuse bool, message string) {
	newLines := gomodreplace.NewMarcelocantosReplaceLines(string(head), string(staged))
	if len(newLines) == 0 {
		return false, ""
	}
	return true, gomodreplace.CommitscopeRefusalMessage(newLines[0])
}
