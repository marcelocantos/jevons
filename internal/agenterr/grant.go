// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import (
	"errors"
	"strings"
)

// overseerNotRunning is the typed "overseer not running" delivery failure.
// Error() stays the sentence logs already match. The class is the type.
type overseerNotRunning struct{}

func (overseerNotRunning) Error() string { return "overseer not running" }

func (overseerNotRunning) NotifyClass() string { return "not_running" }

// ErrOverseerNotRunning is the product's down-overseer delivery failure.
func ErrOverseerNotRunning() error { return overseerNotRunning{} }

// grantRefusal is a broker not_owner refusal after the one boundary read.
// Error() keeps the broker sentence. Controls after NoteBrokerGrant use the type.
type grantRefusal struct {
	text string
	next error
}

func (e *grantRefusal) Error() string {
	if e == nil {
		return ""
	}
	return e.text
}

func (e *grantRefusal) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.next
}

func (e *grantRefusal) NotifyClass() string { return "not_owner" }

// NoteBrokerGrant is the one control that reads the broker's not_owner
// sentence. It returns a typed refusal and leaves every other error as it
// arrived. Later controls use IsGrantRefusal or NotifyClass.
func NoteBrokerGrant(err error) error {
	if err == nil {
		return nil
	}
	var already *grantRefusal
	if errors.As(err, &already) {
		return err
	}
	if strings.Contains(err.Error(), "not_owner") {
		return &grantRefusal{text: err.Error(), next: err}
	}
	return err
}

// IsGrantRefusal reports whether err is a typed broker not_owner refusal.
// A sentence that only contains the words is not one.
func IsGrantRefusal(err error) bool {
	var g *grantRefusal
	return err != nil && errors.As(err, &g) && g != nil
}

// NotifyClass is the structured delivery class of err. Empty when err has
// none. A rendered sentence is not a class.
func NotifyClass(err error) string {
	if err == nil {
		return ""
	}
	var nc interface{ NotifyClass() string }
	if errors.As(err, &nc) && nc != nil {
		return nc.NotifyClass()
	}
	return ""
}
