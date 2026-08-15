// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

// Source names where a candidate failure string came from (🎯T455).
//
// ClassifyText reads words, and words are not evidence of an outage — an
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
// So the classifier is told which side of the wire the string arrived on, and
// refuses to read authored content at all. The discrimination lives here
// rather than in an `if` at each call site because a caller that forgets is
// exactly the failure being fixed.
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
// Authored content is ClassNone unconditionally — no substring is consulted,
// so no quotation of a refusal can ever record one.
//
// Transport-sourced text classifies exactly as ClassifyText does: narrowing
// the source must not blind the detector (🎯T406 clause 4 depends on the
// genuine signal still arriving with its class intact).
func ClassifyFrom(src Source, msg string) Class {
	if src != SourceTransport {
		return ClassNone
	}
	return ClassifyText(msg)
}
