// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/marcelocantos/jevons/internal/transcript"
)

// SetTranscriptReader attaches the multi-provider transcript reader (provider
// session JSONL). Inspect hydrate prefers a T621-reconstructed read of that
// file (compact/snip/rollback / Grok updates.jsonl) and falls back to the
// jevons journal when the reader cannot produce turns.
func (s *Server) SetTranscriptReader(r *transcript.Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcriptReader = r
}
