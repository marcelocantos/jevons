// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import "fmt"

// ReadPath parses a transcript JSONL file the same way Reader.Read does,
// given a path instead of a session id. Handover distill (🎯T392.1.1) uses
// this so the brief is built from the T213 decoder, not a second parser.
func ReadPath(path string) ([]Turn, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	turns := extractTurns(lines, false)
	if len(turns) == 0 {
		turns = extractTurns(lines, true)
	}
	if len(turns) == 0 {
		turns = assistantOnlyTurns(lines)
	}
	if len(turns) == 0 && hasTranscriptPayload(lines) {
		return nil, fmt.Errorf(
			"transcript %s has %d lines and no readable turns — %s",
			path, len(lines), describeLines(lines),
		)
	}
	return turns, nil
}
