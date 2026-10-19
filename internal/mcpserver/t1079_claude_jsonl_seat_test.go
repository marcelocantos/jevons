// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "github.com/marcelocantos/claudia"

// claudeJSONLSeat is the provider a fixture stores for a seat whose records
// are a Claude Code JSONL. Since claudia v0.58 every named plan, claude
// included, runs on the Oh My Pi sidecar, so a row storing claude is the
// anthropic sidecar seat (🎯T1079) and keeps no JSONL; only a row with no
// stored provider still takes that convention. The Claude-JSONL readers the
// T412, T444, T679, T797 and T804 fixtures pin stay exercised on the one row
// shape that still reaches them.
const claudeJSONLSeat claudia.Provider = ""
