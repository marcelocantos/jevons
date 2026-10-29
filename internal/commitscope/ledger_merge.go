// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// UnionLedgerYAML merges two bullseye.yaml blobs by target id. Keys present in
// both sides keep the worker (theirs) record. Used by integrate when only the
// ledger conflicts (🎯T1145).
func UnionLedgerYAML(base, worker []byte) ([]byte, error) {
	baseTargets, err := parseTargets(base)
	if err != nil {
		return nil, fmt.Errorf("ledger union: base: %w", err)
	}
	workerTargets, err := parseTargets(worker)
	if err != nil {
		return nil, fmt.Errorf("ledger union: worker: %w", err)
	}
	merged := make(map[string][]byte, len(baseTargets)+len(workerTargets))
	for id, rec := range baseTargets {
		merged[id] = rec
	}
	for id, rec := range workerTargets {
		merged[id] = rec
	}
	var baseDoc struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(base, &baseDoc); err != nil {
		return nil, fmt.Errorf("ledger union: base schema: %w", err)
	}
	if baseDoc.SchemaVersion == 0 {
		baseDoc.SchemaVersion = 1
	}
	out := struct {
		SchemaVersion int            `yaml:"schema_version"`
		Targets       map[string]any `yaml:"targets"`
	}{SchemaVersion: baseDoc.SchemaVersion, Targets: make(map[string]any, len(merged))}
	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var rec any
		if err := json.Unmarshal(merged[id], &rec); err != nil {
			return nil, fmt.Errorf("ledger union: target %s: %w", id, err)
		}
		out.Targets[id] = rec
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
