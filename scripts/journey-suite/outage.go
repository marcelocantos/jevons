// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// Provider outages are not product defects (🎯T283).
//
// A journey asserts something about Jevons. When the agent backend is down,
// the assertion never got a chance to run — reporting "the shell tool did not
// run" then sends the reader hunting a bug that is not there. Since the MCP
// direct/deliver paths now classify provider failures, a journey can tell the
// two apart and say which one it saw.

// outageError marks a journey step that could not run because the provider
// was unavailable. The suite reports it as OUTAGE, not FAIL.
type outageError struct {
	step  string
	class agenterr.Class
	msg   string
}

func (e *outageError) Error() string {
	return fmt.Sprintf("%s: provider %s — backend outage, not a product defect: %s",
		e.step, e.class, trim(e.msg, 200))
}

// asOutage converts err into an *outageError when it carries a transient
// provider failure class, and returns nil otherwise. Auth and client_bug are
// deliberately excluded: those are real, actionable failures that must keep
// failing the suite.
func asOutage(step string, err error) error {
	if err == nil {
		return nil
	}
	class := agenterr.Classify(err)
	if !class.IsTransient() {
		return nil
	}
	return &outageError{step: step, class: class, msg: err.Error()}
}

// replyOutage reports an outage when a turn's whole reply is a provider
// failure — the Grok ACP shape, where the outage arrives as the assistant
// message rather than as a transport error.
func replyOutage(step, reply string) error {
	class := agenterr.ClassifyReply(reply)
	if !class.IsTransient() {
		return nil
	}
	return &outageError{step: step, class: class, msg: reply}
}

// isOutage reports whether err is an outage verdict.
func isOutage(err error) bool {
	var oe *outageError
	return errors.As(err, &oe)
}
