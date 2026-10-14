# T1032 sandbox migration scout and contract

Scout: `PrepareMigrationPinned` checks T763's refusal policy, but delegates
execution to Claudia v0.52.0. Its stopped migration clones the source definition
into the destination without clearing Codex sandbox fields. Launch then rejects
`sandbox_policy` on Claude. The legacy `rotate` test did not exercise this path;
the wrapper's refusal test used workspace-write and mistook that launch failure
for a policy refusal.

The host now reconciles only a definition already persisted on the destination
provider. It uses the existing T763 reset contract: workspace-write,
danger-full-access, and widening roots/network/git grants may reset to the
fleet's fresh destination-provider baseline. This is a baseline reset, not an
equivalent sandbox translation: Claude does not enforce Codex's workspace
boundary. Read-only, unknown modes, and operator extra arguments still refuse
before transfer or source mutation. Reset details remain visible in dropped_caps
and structured logs. Failed transfer keeps the original source policy intact.

A capability failure with a persisted migration seed retries Claudia's pending
destination once. The transfer, destination session, and handover remain owned
by Claudia; Jevons does not clear the source policy or create another migration
record. Successful migrations also reconcile their destination definition for
later rehydration.

Pinned hermetics: TestT1032StoppedSandboxMigration crosses the real registry
persistence and retry path with a capability-enforcing launcher, asserting one
transfer and one destination session. TestT1032SandboxRestrictionRefusedBeforeTransfer
covers read-only and unknown modes; TestT1032FailedTransferKeepsSourceSandbox
covers transfer failure. Existing fleet tests cover operator ExtraArgs refusal.

Journey exception: the owner explicitly lifted the integration hold; no live
provider migration or development-daemon activation was authorized. These
hermetics establish the registry field-reset contract; real provider/broker
migration and development activation remain unobserved class-3 residuals. No
claim of owner-visible activation or target achievement.
