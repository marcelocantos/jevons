// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

// Source names where a candidate failure string came from (🎯T455).
//
// Words are not evidence of an outage — an
// agent that *writes about* a refusal produces the same characters as the
// backend that *issued* one. On 2026-08-15 the overseer told the owner
// "**The whole fleet is dead — Claude account monthly spend limit reached.**"
// and quoted the refusal verbatim as evidence; the daemon logged that honest
// report as a fresh `provider_failure … surface=chat_wire` whose raw text was
// the overseer's own prose. Two earlier entries on 2026-08-10 are the same
// shape. Every report about an outage manufactured another outage record.
//
// That is load-bearing, not cosmetic: 🎯T406 clause 4 clears the provider
// hard-block on evidence that calls are being served again, and 🎯T407 clause 2
// draws blocked-vs-unattended from recent provider_failure events. Both read a
// signal a talkative overseer can pollute indefinitely, in the one direction
// that keeps a cleared block looking un-cleared.
//
// So the classifier refuses to read the words, on either side of the wire.
// A provider failure is a typed cause. Quoting a refusal is not one, and
// neither is the same sentence arriving as transport text.
type Source string

const (
	// SourceAuthored is message content composed by an agent or the overseer:
	// assistant prose, a streamed token, a report body, a chat bubble. Never a
	// provider failure, however the words read.
	SourceAuthored Source = "authored"

	// SourceTransport is the response the backend actually returned on the
	// call — a JSON-RPC/ACP error frame, an HTTP failure, a returned Go error.
	// This is the only evidence a provider refused to serve.
	SourceTransport Source = "transport"
)

// ClassifyFrom classifies msg only when it arrived as transport evidence.
// Authored content is ClassNone. Transport text is also ClassNone: a
// sentence is not a cause. The class arrives through Classify on a typed
// cause (🎯T406 clause 4 reads that cause, not the words).
func ClassifyFrom(src Source, msg string) Class {
	if src != SourceTransport {
		return ClassNone
	}
	return ClassifyText(msg)
}
