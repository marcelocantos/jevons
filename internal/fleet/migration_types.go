// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import "github.com/marcelocantos/claudia"

// MigrationTransferArgs is the inert-history summary request: the shape of
// claudia.MigrationTransferArgs, which the product path calls through.
type MigrationTransferArgs struct {
	Destination claudia.Provider
	Goal        string
	Transcript  string
}

// MigrationTransferResult is the brief a transfer summary returned.
type MigrationTransferResult struct {
	Brief string
}

// StoppedMigration is the registry rewrite a stopped-seat move reports.
type StoppedMigration struct {
	Source      claudia.AgentDef
	Destination claudia.AgentDef
	Transfer    MigrationTransferResult
}

// MigrateRequest is one live Agent.Migrate call: the target and the brief
// and retained history the successor starts from.
type MigrateRequest struct {
	Provider           claudia.Provider
	Model              string
	Reason             string
	Force              bool
	ContextBrief       string
	RetainedTranscript string
}

func (r MigrateRequest) migrateArgs() *claudia.MigrateArgs {
	// The brief and retained history must reach Claudia: dropping them, as a
	// stand-in for an older pin did, started every live successor blank.
	return &claudia.MigrateArgs{
		Provider:           r.Provider,
		Model:              r.Model,
		Reason:             r.Reason,
		Force:              r.Force,
		ContextBrief:       r.ContextBrief,
		RetainedTranscript: r.RetainedTranscript,
	}
}
