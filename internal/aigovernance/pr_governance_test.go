package aigovernance

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type prGovernanceAI struct {
	match    string
	newTitle string
	draft    string
}

func (a *prGovernanceAI) Call(_ context.Context, _ string, _ AIRequest, _ Config, options AICallOptions) (string, error) {
	switch options.Purpose {
	case "归并匹配":
		return a.match, nil
	case "PR 标题规范化":
		return a.newTitle, nil
	case "起草规范化 issue":
		return a.draft, nil
	default:
		return "", fmt.Errorf("unexpected call: %s", options.Purpose)
	}
}

func (a *prGovernanceAI) Structured(_ context.Context, _ string, _ AIRequest, _ Config, options AICallOptions, _ map[string]any) (map[string]any, error) {
	if options.Purpose == "要点提炼" {
		return map[string]any{"要点": "add cache", "要做的事": []any{"implement"}}, nil
	}
	return nil, fmt.Errorf("unexpected structured call: %s", options.Purpose)
}

func TestPRGovernanceLinkExistingNeverClosesPR(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			return []CanonicalItem{{Number: 57, Title: "cache", Body: "topic"}}, nil
		},
	}
	ai := &prGovernanceAI{match: "DUPLICATE(#57)", newTitle: "feat: add cache"}
	govOptions := DefaultGovernanceOptions()
	govOptions.DryRun = false
	gov := NewPRGovernance(ai, "model", config, govOptions, ops, Logger{})
	result, err := gov.Govern(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "add cache", Body: "please add cache", User: User{Login: "someone"},
	}, "enhancement", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != GovernanceDuplicate || !result.Linked {
		t.Fatalf("unexpected result: %+v", result)
	}
	var titleUpdated, bodyUpdated bool
	for _, patch := range ops.PRPatches {
		if patch["state"] == "closed" {
			t.Fatalf("PR governance closed PR: %v", patch)
		}
		if patch["title"] == "feat: add cache" {
			titleUpdated = true
		}
		if strings.Contains(fmt.Sprint(patch["body"]), "Related to #57") &&
			strings.Contains(fmt.Sprint(patch["body"]), PRLinkAnchor) {
			bodyUpdated = true
		}
	}
	if !titleUpdated || !bodyUpdated {
		t.Fatalf("missing PR updates: %v", ops.PRPatches)
	}
	if len(ops.IssueStates) != 0 {
		t.Fatalf("issue state changed: %v", ops.IssueStates)
	}
}

func TestPRGovernanceNewTopicCreatesCanonicalAndClosesLink(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{}
	ai := &prGovernanceAI{
		match: "NEW_TOPIC", newTitle: "feat: support zzz",
		draft: "[Feature] support zzz\n\n## 概述\nImplement zzz.",
	}
	govOptions := DefaultGovernanceOptions()
	govOptions.DryRun = false
	gov := NewPRGovernance(ai, "model", config, govOptions, ops, Logger{})
	result, err := gov.Govern(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "support zzz", Body: "body", User: User{Login: "someone"},
	}, "enhancement", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.CanonicalNumber != 99 || !result.Created {
		t.Fatalf("unexpected result: %+v", result)
	}
	bodyUpdated := false
	for _, patch := range ops.PRPatches {
		if strings.Contains(fmt.Sprint(patch["body"]), "Closes #99") {
			bodyUpdated = true
		}
		if patch["state"] == "closed" {
			t.Fatal("PR governance unexpectedly closed PR")
		}
	}
	if !bodyUpdated {
		t.Fatalf("missing Closes relation: %v", ops.PRPatches)
	}
}

func TestPRGovernanceDryRunOnlyComments(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			return []CanonicalItem{{Number: 57, Title: "cache", Body: "topic"}}, nil
		},
	}
	ai := &prGovernanceAI{match: "DUPLICATE(#57)", newTitle: "feat: cache"}
	govOpts := DefaultGovernanceOptions()
	govOpts.DryRun = true
	gov := NewPRGovernance(ai, "model", config, govOpts, ops, Logger{})
	result, err := gov.Govern(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "cache", Body: "body", User: User{Login: "someone"},
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(ops.PRPatches) != 0 || len(ops.Created) != 0 || len(ops.IssueStates) != 0 {
		t.Fatalf("dry run wrote: result=%+v ops=%+v", result, ops)
	}
	if len(ops.Comments) != 1 {
		t.Fatalf("dry run comments = %v", ops.Comments)
	}
}

func TestPRLinkAnchorIsIdempotent(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			return []CanonicalItem{{Number: 57, Title: "cache", Body: "topic"}}, nil
		},
	}
	ai := &prGovernanceAI{match: "DUPLICATE(#57)", newTitle: "already valid"}
	govOptions := DefaultGovernanceOptions()
	govOptions.DryRun = false
	gov := NewPRGovernance(ai, "model", config, govOptions, ops, Logger{})
	_, err := gov.Govern(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "feat: cache", Body: PRLinkAnchor + "\n\nexisting", User: User{Login: "someone"},
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range ops.PRPatches {
		if _, ok := patch["body"]; ok {
			t.Fatalf("body was updated despite anchor: %v", patch)
		}
	}
}
