// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

// ClassifyReply classifies a whole agent reply as a provider failure (🎯T283).
//
// A reply is text. Text is not a cause. The owner chat path classes a
// transport frame from its provider-failure cause (server/chat_wire.go).
// A direct/deliver caller that only has the aggregated sentence does not
// get a class from the words, including a bare "Internal error".
func ClassifyReply(string) Class {
	return ClassNone
}

// ReplyFailure returns the class and owner-visible copy for a reply that is
// nothing but a provider failure. ok is false when the reply is text: a
// sentence is not a cause.
func ReplyFailure(reply string) (class Class, ownerMsg string, ok bool) {
	class = ClassifyReply(reply)
	if !class.IsFailure() {
		return ClassNone, "", false
	}
	return class, OwnerCopy(class, reply), true
}
