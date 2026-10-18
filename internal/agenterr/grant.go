// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import (
	"errors"
	"reflect"
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

// brokerCodeNotOwner is the broker protocol code for a grant this
// connection does not own. It is ProtocolError.Code, not a sentence.
const brokerCodeNotOwner = "not_owner"

// protocolCodeWalkLimit bounds how far a grant read follows Unwrap.
// The refusal is the error itself or one wrap around it.
const protocolCodeWalkLimit = 8

// GrantRefusal marks err as a broker not_owner refusal. The rendered text
// stays err.Error(). Callers that already know the refusal — the fault
// seam, a test double — use this. A sentence is not a reason to call it.
func GrantRefusal(err error) error {
	if err == nil {
		return nil
	}
	var already *grantRefusal
	if errors.As(err, &already) {
		return err
	}
	return &grantRefusal{text: err.Error(), next: err}
}

// NoteBrokerGrant types a broker not_owner refusal. The machine code is
// ProtocolError.Code on the value claudia's broker returns. This module
// does not import that package. A sentence that contains the words is
// left as it arrived. Later controls use IsGrantRefusal or NotifyClass.
func NoteBrokerGrant(err error) error {
	if err == nil {
		return nil
	}
	var already *grantRefusal
	if errors.As(err, &already) {
		return err
	}
	if protocolCode(err) != brokerCodeNotOwner {
		return err
	}
	return &grantRefusal{text: err.Error(), next: err}
}

// protocolCode reads ProtocolError.Code. The rendered sentence is not read.
func protocolCode(err error) string {
	for range protocolCodeWalkLimit {
		if err == nil {
			return ""
		}
		v := reflect.ValueOf(err)
		if v.Kind() == reflect.Pointer && v.IsNil() {
			return ""
		}
		if code, ok := exportedProtocolCode(err); ok {
			return code
		}
		err = errors.Unwrap(err)
	}
	return ""
}

// exportedProtocolCode reads an exported Code string on a value whose
// concrete type is named ProtocolError. Any other type is not a broker
// refusal, even when it has a Code field or the words in Error.
func exportedProtocolCode(err error) (string, bool) {
	v := reflect.ValueOf(err)
	if !v.IsValid() {
		return "", false
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct || v.Type().Name() != "ProtocolError" {
		return "", false
	}
	f := v.FieldByName("Code")
	if !f.IsValid() || f.Kind() != reflect.String || !f.CanInterface() {
		return "", false
	}
	return f.String(), true
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
