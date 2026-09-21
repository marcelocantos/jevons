// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestT811FaultSeamConsumesOneRefusalPerOwnerAttempt(t *testing.T) {
	dir := t.TempDir()
	s := &Server{}
	owner := userTurnPrefix + "hi"
	if s.injectedBrokerFault(owner) != nil {
		t.Fatal("unarmed seam injected a fault")
	}
	s.EnableBrokerFaultSeam(dir)
	if s.injectedBrokerFault(owner) != nil {
		t.Fatal("armed seam without a fault file injected a fault")
	}
	if err := os.WriteFile(filepath.Join(dir, brokerFaultFile), []byte("2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.injectedBrokerFault("[event: worker-idle]") != nil {
		t.Fatal("fleet note consumed the owner fault")
	}
	for i := 0; i < 2; i++ {
		err := s.injectedBrokerFault(owner)
		if err == nil || !strings.Contains(err.Error(), "not_owner") || notifyErrClass(err) != "not_owner" {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if s.injectedBrokerFault(owner) != nil {
		t.Fatal("fault outlived its count")
	}
}
