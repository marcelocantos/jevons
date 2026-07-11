// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package discovery

func processExists(pid int) bool {
	return true // best-effort: trust active_sessions.json
}
