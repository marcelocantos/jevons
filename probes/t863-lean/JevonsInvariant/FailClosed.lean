/-!
# 🎯T863 / 🎯T862.17 — Malformed deny/guard configuration fails closed

Models the reliability invariant already named on the Jevons frontier
(`internal/writconf`: `Parse` + `CLIExecutor.Exec` + `ClassifyAccess`):

1. A malformed policy never loads as a usable `Manifest`.
2. Execution under a policy path requires a successful load; load failure
   refuses the run (no CLI launch).
3. A missing/`none` manifest fails closed on net and fs classification —
   never "no restriction".

This is a *probe*, not a Lean rewrite of Jevons. The concrete Go oracles
remain `internal/writconf/t862_17_test.go`.
-/

namespace Jevons.Writ.FailClosed

/-- Simplified policy document after a successful parse+validate. -/
structure Manifest where
  /-- Schema version matched the only supported value. -/
  schemaOk : Bool
  /-- At least one host-granularity net grant (probe simplification). -/
  hasNetGrant : Bool
  deriving DecidableEq, Repr

/-- Outcome of loading a policy document from bytes/text. -/
inductive LoadResult where
  | ok  (m : Manifest)
  | err (reason : String)
  deriving DecidableEq, Repr

/-- Structural tags for the malformed shapes in `t862_17_test.go`. -/
inductive Malform where
  | unknownField
  | badSchema
  | emptyGlob
  | negativeBudget
  | emptyNetGrant
  | multiJSON
  deriving DecidableEq, Repr

/-- Abstract policy source: either a known-good document or a tagged malformation. -/
inductive Source where
  | valid (m : Manifest)
  | malformed (kind : Malform)
  deriving DecidableEq, Repr

/--
`parse` mirrors `writconf.Parse`: validate before a policy can load.
Malformed sources never produce `.ok`.
-/
def parse : Source → LoadResult
  | .valid m =>
      if m.schemaOk then .ok m else .err "schema"
  | .malformed _ => .err "malformed"

/-- True when the source is one of the tagged malformed shapes. -/
def isMalformed : Source → Bool
  | .malformed _ => true
  | .valid _ => false

/--
Execution is permitted only when a validated snapshot exists.
Mirrors `CLIExecutor.Exec`: read → Parse → snapshot → run; Parse error
returns before the CLI is touched.
-/
def mayExecute : LoadResult → Bool
  | .ok _ => true
  | .err _ => false

/-- Net allow-check with optional manifest (nil = unloadable). -/
def netAllowed : Option Manifest → String → Bool
  | none, _ => false
  | some m, _ => m.hasNetGrant

/-- FS allow-check with optional manifest. -/
def fsAllowed : Option Manifest → String → Bool
  | none, _ => false
  | some _, _ => true  -- probe: a loaded manifest that reached here already validated fs

/-- Lift a load result into the optional-manifest view used by classifiers. -/
def asOption : LoadResult → Option Manifest
  | .ok m => some m
  | .err _ => none

/-! ## Target theorems (initially `sorry` — close via skill + oracle) -/

/-- **I1** Malformed source never loads as `.ok`. -/
theorem malformed_never_ok (s : Source) (h : isMalformed s = true) :
    ¬ ∃ m, parse s = .ok m := by
  sorry

/-- **I2** Malformed source refuses execution (no CLI launch). -/
theorem malformed_refuses_execute (s : Source) (h : isMalformed s = true) :
    mayExecute (parse s) = false := by
  sorry

/-- **I3** Missing manifest fails closed on network. -/
theorem nil_manifest_denies_net (host : String) :
    netAllowed none host = false := by
  sorry

/-- **I4** Missing manifest fails closed on filesystem. -/
theorem nil_manifest_denies_fs (path : String) :
    fsAllowed none path = false := by
  sorry

/-- **I5** Load error and missing manifest agree on classification. -/
theorem err_load_is_nil_for_net (reason host : String) :
    netAllowed (asOption (.err reason)) host = false := by
  sorry

/-- **I6** A schema-ok valid source with a net grant may execute. -/
theorem valid_schema_may_execute (m : Manifest) (hs : m.schemaOk = true) :
    mayExecute (parse (.valid m)) = true := by
  sorry

end Jevons.Writ.FailClosed
