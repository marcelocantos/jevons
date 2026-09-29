// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "fmt"

// 🎯T942: a jevons_* tool answers into the caller's context, and a
// long-lived seat's context is the budget every later request pays for. On
// 2026-09-30 two jevons_transcript_read answers of 673 KB made the overseer
// compact three times in 30 minutes. Tools whose answers grow with history
// return a bounded page and say what they left out.

// toolListLimit is the most items a history-sized list returns: the newest.
const toolListLimit = 50

// boundText keeps text within the page budget. A longer text is cut at the
// budget and ends with what was cut and how to get the rest.
func boundText(text, rest string) string {
	if len(text) <= transcriptPageBytes {
		return text
	}
	return fmt.Sprintf("%s\n\n[… %d more bytes not shown (%d total) — %s]",
		text[:transcriptPageBytes], len(text)-transcriptPageBytes, len(text), rest)
}

// newestItems is the newest limit items of a list kept oldest first, and
// how many older ones it left out.
func newestItems[T any](items []T, limit int) ([]T, int) {
	if len(items) <= limit {
		return items, 0
	}
	return items[len(items)-limit:], len(items) - limit
}
