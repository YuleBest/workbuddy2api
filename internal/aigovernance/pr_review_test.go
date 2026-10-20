package aigovernance

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type reviewAI struct {
	decision string
	evidence []any
	comment  string
}

func (a *reviewAI) Call(context.Context, string, AIRequest, Config, AICallOptions) (string, error) {
	return "", fmt.Errorf("unexpected raw AI call")
}

func (a *reviewAI) Structured(_ context.Context, _ string, _ AIRequest, _ Config, options AICallOptions, _ map[string]any) (map[string]any, error) {
	switch options.Purpose {
	case "PR 历史语境评审":
		return map[string]any{"decision": a.decision, "reasons": []any{}, "evidence": a.evidence}, nil
	case "PR 评审评论起草":
		return map[string]any{"comment": a.comment}, nil
	default:
		return nil, fmt.Errorf("unexpected structured call: %s", options.Purpose)
	}
}

func TestPRReviewClosesOnlyWithVerifiedEvidence(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			return []CanonicalItem{{
				Number: 57, Title: "cache", Body: "do not cache", State: "closed", StateReason: stringPointer("not_planned"),
			}}, nil
		},
		ListIssueCommentsFunc: func(context.Context, string, string, int, int) ([]HistoryComment, error) {
			return []HistoryComment{{Author: "maintainer", Body: "wontfix"}}, nil
		},
		ListIssueTimelineFunc: func(context.Context, string, string, int, int) ([]TimelineEvent, error) {
			return []TimelineEvent{{Event: "closed", Actor: "maintainer"}}, nil
		},
	}
	ai := &reviewAI{
		decision: "CLOSE",
		evidence: []any{"#57 was closed as not_planned by a maintainer"},
		comment:  "## History\n#57 rejected this approach.",
	}
	govOptions := DefaultGovernanceOptions()
	govOptions.DryRun = false
	review := PRReview{AI: ai, Model: "model", Config: config, Gov: govOptions, Ops: ops}
	result, err := review.Review(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "feat: cache", Body: "add cache", User: User{Login: "someone"},
	}, "a.go(+1/-1)", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Decision != ReviewClose {
		t.Fatalf("result = %+v", result)
	}
	closed := false
	for _, patch := range ops.PRPatches {
		if patch["state"] == "closed" {
			closed = true
		}
	}
	if !closed {
		t.Fatal("PR was not closed after verified evidence")
	}
	if len(ops.Labels) == 0 || ops.Labels[0][0] != "history-rejected" {
		t.Fatalf("history-rejected label missing: %v", ops.Labels)
	}
	if len(ops.Comments) == 0 || !strings.Contains(ops.Comments[0], "governance") &&
		!strings.Contains(ops.Comments[0], "#57") {
		t.Fatalf("review comment missing: %v", ops.Comments)
	}
}

func TestPRReviewHallucinatedEvidenceFallsBack(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			return []CanonicalItem{{Number: 57, Title: "cache", State: "closed", StateReason: stringPointer("not_planned")}}, nil
		},
	}
	ai := &reviewAI{decision: "CLOSE", evidence: []any{"#999 rejected this"}}
	review := PRReview{AI: ai, Model: "model", Config: config, Gov: DefaultGovernanceOptions(), Ops: ops}
	result, err := review.Review(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "feat: cache", Body: "cache", User: User{Login: "someone"},
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil || len(ops.PRPatches) != 0 || len(ops.Comments) != 0 {
		t.Fatalf("hallucinated evidence caused writes: result=%+v ops=%+v", result, ops)
	}
}

func TestPRReviewCitationGateRejectsComment(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			return []CanonicalItem{{Number: 57, Title: "cache", State: "closed", StateReason: stringPointer("not_planned")}}, nil
		},
	}
	ai := &reviewAI{
		decision: "CLOSE",
		evidence: []any{"#57 wontfix", "#57 not_planned"},
		comment:  "bad draft cites #999",
	}
	review := PRReview{AI: ai, Model: "model", Config: config, Gov: DefaultGovernanceOptions(), Ops: ops}
	result, err := review.Review(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "feat: cache", Body: "cache", User: User{Login: "someone"},
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil || len(ops.Comments) != 0 || len(ops.PRPatches) != 0 {
		t.Fatalf("hallucinated comment caused writes: result=%+v ops=%+v", result, ops)
	}
}

func TestPRReviewNoHistoryMakesNoAICall(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{}
	ai := &reviewAI{}
	review := PRReview{AI: ai, Model: "model", Config: config, Gov: DefaultGovernanceOptions(), Ops: ops}
	result, err := review.Review(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "feat: cache", Body: "cache", User: User{Login: "someone"},
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil || len(ops.Comments) != 0 || len(ops.PRPatches) != 0 {
		t.Fatalf("unexpected writes: %+v", ops)
	}
}

func TestEvidenceKeywordsAliasWontfixAndNotPlanned(t *testing.T) {
	item := HistoryDetail{State: "closed", StateReason: stringPointer("not_planned")}
	keywords := EvidenceKeywords(item)
	if !containsFold(keywords, "not_planned") || !containsFold(keywords, "wontfix") {
		t.Fatalf("keywords = %v", keywords)
	}
}
