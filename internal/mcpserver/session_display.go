// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "strings"

// sessionDisplay renders a session id for agent_list / start acks so
// concurrent UUID-v7 mints are distinguishable. UUID-v7 packs a
// millisecond timestamp into the leading bits: an 8-char prefix is
// often identical across peers minted together. We show the undashed
// head (time + version nibble) plus a tail of entropy so list lines
// differ when SessionIDs differ.
//
// Format: <8 hex>…<6 hex> of the hyphen-stripped id when long enough;
// otherwise the full original string.
func sessionDisplay(sessionID string) string {
	raw := strings.ReplaceAll(sessionID, "-", "")
	const head, tail = 8, 6
	if len(raw) < head+tail {
		return sessionID
	}
	return raw[:head] + "…" + raw[len(raw)-tail:]
}

// short is the shared truncation used by thread tooling; same policy
// as sessionDisplay so concurrent sessions never look identical.
func short(sessionID string) string {
	return sessionDisplay(sessionID)
}
