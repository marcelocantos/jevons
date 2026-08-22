// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/marcelocantos/jevons/internal/transcript"
)

// SetTranscriptReader attaches the multi-provider transcript reader (provider
// session JSONL). Inspect hydrate does not use it — writeInspectReplay reads
// the jevons journal. Kept so MCP/butler discovery still has a reader.
func (s *Server) SetTranscriptReader(r *transcript.Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcriptReader = r
}
