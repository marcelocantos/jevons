// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import "github.com/marcelocantos/claudia"

// MigrationTransferArgs is the inert-history summary request. The
// published claudia module does not export this type; fixtures inject
// the summary, and the product path records the same shape.
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

// MigrateRequest is one live Agent.Migrate call plus the brief the
// published MigrateArgs struct cannot carry. The product path forwards
// only the published fields to Agent.Migrate.
type MigrateRequest struct {
	Provider           claudia.Provider
	Model              string
	Reason             string
	Force              bool
	ContextBrief       string
	RetainedTranscript string
}

func (r MigrateRequest) migrateArgs() *claudia.MigrateArgs {
	return &claudia.MigrateArgs{
		Provider: r.Provider,
		Model:    r.Model,
		Reason:   r.Reason,
		Force:    r.Force,
	}
}
