// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// CauseProviderTransport is [claudia.Event.Reason] on a turn that died on the
// provider/client transport before any authored reply. The message text is not
// a cause.
const CauseProviderTransport = "provider_transport"

// providerTransportTurnDeathSpecimens are whole-turn transport failures from
// production transcripts (🎯T782 specimen 2026-09-21). Matching is exact on
// the trimmed body, or a known RetriableError prefix — not substring search
// over arbitrary overseer prose.
var providerTransportTurnDeathSpecimens = []string{
	"Error: RetriableError: Connection stalled",
	"[canceled] http/2 stream closed",
}

// IsProviderTransportTurnDeath reports whether text is solely a provider or
// client transport failure that ended a turn. Quoting these strings in a
// report is not a match — the classifier only runs on events the wire already
// treats as transport evidence (🎯T455 / 🎯T782).
func IsProviderTransportTurnDeath(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return false
	}
	for _, specimen := range providerTransportTurnDeathSpecimens {
		if s == specimen {
			return true
		}
	}
	if strings.HasPrefix(s, "Error: RetriableError:") {
		return true
	}
	return false
}
