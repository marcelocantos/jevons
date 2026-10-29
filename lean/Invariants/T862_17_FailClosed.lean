/-!
# 🎯T862.17 — writ policy fail-closed (abstract model)
-/

namespace Jevons.T862_17

structure Manifest where
  schemaVersion : Nat

inductive ParseResult
  | ok (m : Manifest)
  | err (msg : String)

inductive PolicySample
  | malformedTypoFs
  | malformedTypoReed
  | malformedMaxFileZero
  | malformedEmptyEdit
  | malformedNullEdit
  | malformedBadGlob
  | malformedNegMaxFiles
  | malformedTypoHost
  | malformedConcatJSON
  | validNetHost
  | validFSReadEdit

def isMalformed : PolicySample → Bool
  | .validNetHost | .validFSReadEdit => false
  | _ => true

def parseSample : PolicySample → ParseResult
  | .malformedTypoFs => .err "unknown field fss"
  | .malformedTypoReed => .err "unknown field reed"
  | .malformedMaxFileZero => .err "max_file must be positive"
  | .malformedEmptyEdit => .err "empty edit glob"
  | .malformedNullEdit => .err "null edit entry"
  | .malformedBadGlob => .err "invalid glob syntax"
  | .malformedNegMaxFiles => .err "max_files must be non-negative"
  | .malformedTypoHost => .err "unknown field hsot"
  | .malformedConcatJSON => .err "trailing document after first object"
  | .validNetHost => .ok ⟨1⟩
  | .validFSReadEdit => .ok ⟨1⟩

def manifestFromParse (r : ParseResult) : Option Manifest :=
  match r with
  | .ok m => some m
  | .err _ => none

def cliWouldLaunch (p : PolicySample) : Bool :=
  match parseSample p with
  | .ok _ => true
  | .err _ => false

theorem malformed_refuses_cli (p : PolicySample) (h : isMalformed p) :
    cliWouldLaunch p = false := by
  rcases p with
    ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | validNetHost | validFSReadEdit
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · simp [isMalformed] at h
  · simp [isMalformed] at h

theorem malformed_yields_no_manifest (p : PolicySample) (h : isMalformed p) :
    manifestFromParse (parseSample p) = none := by
  rcases p with
    ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | validNetHost | validFSReadEdit
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · rfl
  · simp [isMalformed] at h
  · simp [isMalformed] at h

theorem valid_samples_parse (p : PolicySample) (h : ¬ isMalformed p) :
    ∃ m, parseSample p = .ok m := by
  rcases p with
    ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | ⟨⟩ | validNetHost | validFSReadEdit
  · exact absurd h (by decide)
  · exact absurd h (by decide)
  · exact absurd h (by decide)
  · exact absurd h (by decide)
  · exact absurd h (by decide)
  · exact absurd h (by decide)
  · exact absurd h (by decide)
  · exact absurd h (by decide)
  · exact absurd h (by decide)
  · exact ⟨⟨1⟩, rfl⟩
  · exact ⟨⟨1⟩, rfl⟩

inductive FsDecision
  | allow
  | denyNoManifest

def classifyFsAccess (m : Option Manifest) (_path : String) : FsDecision :=
  match m with
  | none => .denyNoManifest
  | some _ => .allow

theorem nil_manifest_denies_fs (path : String) :
    classifyFsAccess none path = .denyNoManifest := rfl

def netAllowed (m : Option Manifest) : Bool :=
  match m with
  | none => false
  | some _ => true

theorem net_fail_closed (m : Option Manifest) (h : m = none) : netAllowed m = false := by
  rw [h]
  rfl


end Jevons.T862_17
