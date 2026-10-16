// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import "testing"

func TestClassifyIssueDirectClasses(t *testing.T) {
	cases := []struct {
		class  string
		action string
	}{
		{"duplicate", "comment_close_duplicate"},
		{"question", "answer_with_doc_link"},
		{"bug_report", "mirror_and_acknowledge"},
		{"spam", "close_with_reason"},
		{"off-topic", "close_with_reason"},
	}
	for _, c := range cases {
		d := ClassifyRepoItemRisk(RiskInput{Kind: KindIssue, IssueClass: c.class})
		if d.Class != RiskDirect || d.DirectAction != c.action {
			t.Fatalf("%s: got %+v", c.class, d)
		}
	}
}

func TestClassifyIssueUnknownEscalates(t *testing.T) {
	d := ClassifyRepoItemRisk(RiskInput{Kind: KindIssue, IssueClass: ""})
	if d.Class != RiskEscalate {
		t.Fatalf("empty class should escalate: %+v", d)
	}
	d = ClassifyRepoItemRisk(RiskInput{Kind: KindIssue, IssueClass: "feature_request"})
	if d.Class != RiskEscalate {
		t.Fatalf("feature_request should escalate: %+v", d)
	}
}

func TestClassifyPRDocsOnlyGreen(t *testing.T) {
	d := ClassifyRepoItemRisk(RiskInput{
		Kind: KindPR, DocsOnly: true, CodeOrConfig: false, CIState: "success",
	})
	if d.Class != RiskDirect || d.DirectAction != "approve_squash_merge_docs" {
		t.Fatalf("docs-only green: %+v", d)
	}
}

func TestClassifyPRDocsOnlyNoCI(t *testing.T) {
	d := ClassifyRepoItemRisk(RiskInput{
		Kind: KindPR, DocsOnly: true, CIState: "failure",
	})
	if d.Class != RiskEscalate {
		t.Fatalf("docs-only red CI must escalate: %+v", d)
	}
}

func TestClassifyPRBotDepBumpMinor(t *testing.T) {
	d := ClassifyRepoItemRisk(RiskInput{
		Kind: KindPR, DepBump: true, IsBot: true, Semver: "patch", CIState: "success",
	})
	if d.Class != RiskDirect || d.DirectAction != "approve_squash_merge_dep_bump" {
		t.Fatalf("bot patch bump: %+v", d)
	}
	d = ClassifyRepoItemRisk(RiskInput{
		Kind: KindPR, DepBump: true, IsBot: true, Semver: "major", CIState: "success",
	})
	if d.Class != RiskEscalate {
		t.Fatalf("major bump must escalate: %+v", d)
	}
}

func TestClassifyPRSensitiveAlwaysEscalates(t *testing.T) {
	d := ClassifyRepoItemRisk(RiskInput{
		Kind: KindPR, DocsOnly: true, CIState: "success", TouchesSensitive: true,
	})
	if d.Class != RiskEscalate {
		t.Fatalf("sensitive path must escalate even if docs-shaped: %+v", d)
	}
}

func TestClassifyPRExternalAuthorEscalates(t *testing.T) {
	d := ClassifyRepoItemRisk(RiskInput{
		Kind: KindPR, CodeOrConfig: true, ExternalAuthor: true, CIState: "success",
	})
	if d.Class != RiskEscalate {
		t.Fatalf("external code PR: %+v", d)
	}
}

func TestClassifyPRDraftEscalates(t *testing.T) {
	d := ClassifyRepoItemRisk(RiskInput{
		Kind: KindPR, DocsOnly: true, CIState: "success", Draft: true,
	})
	if d.Class != RiskEscalate {
		t.Fatalf("draft: %+v", d)
	}
}

func TestClassifyPRCodeChangeEscalates(t *testing.T) {
	d := ClassifyRepoItemRisk(RiskInput{
		Kind: KindPR, CodeOrConfig: true, CIState: "success",
	})
	if d.Class != RiskEscalate {
		t.Fatalf("code change: %+v", d)
	}
}
