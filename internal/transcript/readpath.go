// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

// ReadPath parses a transcript JSONL file the same way Reader.Read does,
// given a path instead of a session id. Handover distill (🎯T392.1.1) uses
// this so the brief is built from the T213 decoder, not a second parser.
// Reconstruction (compact/snip/rollback) is applied here (🎯T621).
func ReadPath(path string) ([]Turn, error) {
	got, err := ReadLogical(path)
	return got.Turns, err
}
