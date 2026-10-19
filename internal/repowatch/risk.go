// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"strings"
)

// RiskClass is the PO triage outcome for one item (🎯T1022.3).
type RiskClass string

const (
	// RiskDirect: PO may act without owner sign-off (conservative list).
	RiskDirect RiskClass = "direct"
	// RiskEscalate: everything else — route via jevons_repo_escalate.
	RiskEscalate RiskClass = "escalate"
)

// RiskInput is the pure classifier's view of an item. Callers project from
// Item (and optional PR-diff metadata) into this shape.
type RiskInput struct {
	Kind   ItemKind
	// IssueClass is optional free-text for issues: "duplicate" | "question" |
	// "bug_report" | "spam" | "" (unknown → escalate unless other signals).
	IssueClass string
	// DocsOnly is true when a PR diff touches only docs/comments/typos
	// (no code, no config). Caller-computed from the diff.
	DocsOnly bool
	// DepBump is true when the PR is a bot dependency bump.
	DepBump bool
	// Semver is "major" | "minor" | "patch" | "" for dep bumps.
	Semver string
	// CIState is success|failure|pending|unknown.
	CIState string
	// ExternalAuthor is true when the author is not a repo collaborator /
	// org member (caller decides).
	ExternalAuthor bool
	// TouchesSensitive is true when the PR touches auth, secrets, release,
	// or .github/ (caller-computed from paths).
	TouchesSensitive bool
	// CodeOrConfig is true when the PR has any non-docs code/config change.
	CodeOrConfig bool
	// IsBot is true for dependabot/renovate-style authors.
	IsBot bool
	// Draft is true for draft PRs (never direct-merge).
	Draft bool
}

// RiskDecision is the classifier output.
type RiskDecision struct {
	Class  RiskClass
	Reason string
	// DirectAction names the permitted action when Class==direct
	// (e.g. "comment_close_duplicate", "approve_squash_merge_docs",
	// "approve_squash_merge_dep_bump", "mirror_bug_report").
	DirectAction string
}

// ClassifyRepoItemRisk applies the conservative direct-action classes from
// docs/design/po-github-watch.md. Pure: hermetic specimens decide it.
//
// An item is **direct** only if every line it touches, and every effect, is
// on the named list; anything else escalates. Merging is Ship-plane and is
// only opened for the two PR merge classes below (🎯T1022 grant; 🎯T104 stays).
func ClassifyRepoItemRisk(in RiskInput) RiskDecision {
	switch in.Kind {
	case KindIssue:
		return classifyIssue(in)
	case KindPR:
		return classifyPR(in)
	default:
		return RiskDecision{Class: RiskEscalate, Reason: "unknown item kind"}
	}
}

func classifyIssue(in RiskInput) RiskDecision {
	switch strings.ToLower(strings.TrimSpace(in.IssueClass)) {
	case "duplicate":
		return RiskDecision{
			Class:        RiskDirect,
			Reason:       "duplicate of an open target or issue",
			DirectAction: "comment_close_duplicate",
		}
	case "question":
		return RiskDecision{
			Class:        RiskDirect,
			Reason:       "question answerable from committed docs",
			DirectAction: "answer_with_doc_link",
		}
	case "bug_report":
		return RiskDecision{
			Class:        RiskDirect,
			Reason:       "clear, reproducible, well-scoped bug report",
			DirectAction: "mirror_and_acknowledge",
		}
	case "spam", "off_topic", "off-topic":
		return RiskDecision{
			Class:        RiskDirect,
			Reason:       "spam/off-topic",
			DirectAction: "close_with_reason",
		}
	default:
		return RiskDecision{
			Class:  RiskEscalate,
			Reason: "issue class not in the conservative direct list (or unset)",
		}
	}
}

func classifyPR(in RiskInput) RiskDecision {
	if in.Draft {
		return RiskDecision{Class: RiskEscalate, Reason: "draft PR — never direct-merge"}
	}
	if in.TouchesSensitive {
		return RiskDecision{
			Class:  RiskEscalate,
			Reason: "touches auth, secrets, release, or .github/ — never merge; escalate",
		}
	}
	ciOK := strings.EqualFold(in.CIState, "success")
	// Docs-only, comments-only, typo fixes: approve + squash-merge if CI green
	// and the diff contains no code or config.
	if in.DocsOnly && !in.CodeOrConfig {
		if !ciOK {
			return RiskDecision{
				Class:  RiskEscalate,
				Reason: "docs-only PR without green CI",
			}
		}
		return RiskDecision{
			Class:        RiskDirect,
			Reason:       "docs-only / comments-only / typo fix with green CI",
			DirectAction: "approve_squash_merge_docs",
		}
	}
	// Dependency bumps from a bot with green CI and minor/patch semver.
	if in.DepBump || (in.IsBot && looksLikeDepBump(in)) {
		if !in.IsBot && !in.DepBump {
			return RiskDecision{Class: RiskEscalate, Reason: "dep bump not from a bot"}
		}
		if !ciOK {
			return RiskDecision{Class: RiskEscalate, Reason: "bot dep bump without green CI"}
		}
		sv := strings.ToLower(strings.TrimSpace(in.Semver))
		if sv == "major" || sv == "" {
			return RiskDecision{
				Class:  RiskEscalate,
				Reason: "bot dep bump is major (or semver unknown) — escalate",
			}
		}
		if sv == "minor" || sv == "patch" {
			return RiskDecision{
				Class:        RiskDirect,
				Reason:       "bot dependency bump, minor/patch, green CI",
				DirectAction: "approve_squash_merge_dep_bump",
			}
		}
		return RiskDecision{Class: RiskEscalate, Reason: "bot dep bump with unrecognised semver"}
	}
	// CI/config-only, any code change, external author outside the two
	// classes above, any PR without green CI: escalate (may still comment).
	if in.ExternalAuthor {
		return RiskDecision{
			Class:  RiskEscalate,
			Reason: "external author outside the two direct-merge classes",
		}
	}
	if in.CodeOrConfig {
		return RiskDecision{
			Class:  RiskEscalate,
			Reason: "code or config change — review/comment if useful, never merge, escalate",
		}
	}
	if !ciOK {
		return RiskDecision{Class: RiskEscalate, Reason: "PR without green CI"}
	}
	return RiskDecision{
		Class:  RiskEscalate,
		Reason: "PR does not match a named direct-action class",
	}
}

func looksLikeDepBump(in RiskInput) bool {
	// Without an explicit DepBump flag, a bot PR is treated as a candidate
	// dep bump only when the caller also set DepBump or Semver — keep this
	// conservative so a random bot PR does not auto-merge.
	return in.DepBump || strings.TrimSpace(in.Semver) != ""
}
