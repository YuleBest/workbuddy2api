package aigovernance

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type governanceAI struct {
	match         string
	review        string
	canonical     string
	screen        string
	structuredErr error
}

func (a *governanceAI) Call(_ context.Context, _ string, _ AIRequest, _ Config, options AICallOptions) (string, error) {
	switch options.Purpose {
	case "规范判定":
		return "NEEDS_NORMALIZE", nil
	case "归并匹配":
		return a.match, nil
	case "生成规范 issue 评审评论":
		return a.review, nil
	case "起草规范化 issue":
		return a.canonical, nil
	default:
		return "", fmt.Errorf("unexpected call %s", options.Purpose)
	}
}

func (a *governanceAI) Structured(_ context.Context, _ string, _ AIRequest, _ Config, options AICallOptions, defaults map[string]any) (map[string]any, error) {
	if a.structuredErr != nil {
		return nil, a.structuredErr
	}
	switch options.Purpose {
	case "要点提炼":
		return map[string]any{"要点": "support cache", "要做的事": []any{"implement cache"}}, nil
	case "历史语境筛选":
		return map[string]any{"candidates": []any{
			map[string]any{"number": float64(57), "kind": "issue", "relevance": "direct"},
		}}, nil
	default:
		return nil, fmt.Errorf("unexpected structured call %s", options.Purpose)
	}
}

func TestIssueGovernanceDuplicateOrdersCanonicalWrites(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			return []CanonicalItem{{Number: 57, Title: "cache", Body: "existing"}}, nil
		},
	}
	ai := &governanceAI{match: "DUPLICATE(#57)"}
	govOptions := DefaultGovernanceOptions()
	govOptions.DryRun = false
	gov := IssueGovernance{
		AI: ai, Model: "model", Config: config, Gov: govOptions, Ops: ops, Logger: Logger{},
	}
	result, err := gov.Govern(context.Background(), "o", "r", Issue{
		Number: 22, Title: "need cache", Body: "please add cache", User: User{Login: "someone"},
	}, "enhancement", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != GovernanceDuplicate || !result.Closed {
		t.Fatalf("unexpected result: %+v", result)
	}
	want := []string{"labels:22", "comment:22", "issue-state:22:closed", "comment:57"}
	if strings.Join(ops.Events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", ops.Events, want)
	}
}

func TestIssueGovernanceCreatesCanonicalBeforeClosing(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{}
	ai := &governanceAI{
		match:     "NEW_TOPIC",
		canonical: "[Feature] cache\n\n## 概述\nAdd cache.",
	}
	govOptions := DefaultGovernanceOptions()
	govOptions.DryRun = false
	gov := IssueGovernance{AI: ai, Model: "model", Config: config, Gov: govOptions, Ops: ops}
	result, err := gov.Govern(context.Background(), "o", "r", Issue{
		Number: 22, Title: "need cache", Body: "please add cache", User: User{Login: "someone"},
	}, "enhancement", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.CanonicalNumber != 99 || !result.Closed {
		t.Fatalf("unexpected result: %+v", result)
	}
	createIndex, closeIndex := -1, -1
	for index, event := range ops.Events {
		if event == "create-issue" {
			createIndex = index
		}
		if event == "issue-state:22:closed" {
			closeIndex = index
		}
	}
	if createIndex < 0 || closeIndex < 0 || createIndex >= closeIndex {
		t.Fatalf("canonical must be created before close: %v", ops.Events)
	}
}

func TestIssueGovernanceCreateFailureNeverCloses(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		CreateIssueFunc: func(context.Context, string, string, string, string, []string) (Issue, error) {
			return Issue{}, fmt.Errorf("create boom")
		},
	}
	ai := &governanceAI{match: "NEW_TOPIC", canonical: "[Feature] x\n\nbody"}
	govOptions := DefaultGovernanceOptions()
	govOptions.DryRun = false
	gov := IssueGovernance{AI: ai, Model: "model", Config: config, Gov: govOptions, Ops: ops}
	_, err := gov.Govern(context.Background(), "o", "r", Issue{
		Number: 22, Title: "title", Body: "body", User: User{Login: "someone"},
	}, "", nil)
	if err == nil {
		t.Fatal("expected create error")
	}
	if len(ops.IssueStates) != 0 {
		t.Fatalf("issue was closed after canonical failure: %v", ops.IssueStates)
	}
}

func TestIssueGovernanceDryRunHasZeroDestructiveWrites(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			return []CanonicalItem{{Number: 57, Title: "cache", Body: "existing"}}, nil
		},
	}
	ai := &governanceAI{match: "DUPLICATE(#57)"}
	gov := IssueGovernance{
		AI: ai, Model: "model", Config: config, Gov: GovernanceOptions{
			CanonicalLabel: "canonical", DuplicateLabel: "duplicate", DryRun: true,
			MaxCanonicalIndex: 50, CanonicalBodyTruncate: 1500,
			MaxScreenedCandidates: 5, WellFormedMinSections: 3,
			WellFormedMinTitleLen: 8, WellFormedMinBodyLen: 80,
		}, Ops: ops,
	}
	result, err := gov.Govern(context.Background(), "o", "r", Issue{
		Number: 22, Title: "need cache", Body: "please add cache", User: User{Login: "someone"},
	}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(ops.IssueStates) != 0 || len(ops.Labels) != 0 || len(ops.Created) != 0 {
		t.Fatalf("dry run wrote destructively: %+v", ops)
	}
	if len(ops.Comments) != 1 || !strings.Contains(ops.Comments[0], "本应执行") {
		t.Fatalf("dry run comment missing: %v", ops.Comments)
	}
}

func TestWellFormedReviewCitationGateFallsBack(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ai := &governanceAI{
		review: "Thanks @someone\n\nSee #999 for details.",
	}
	gov := IssueGovernance{AI: ai, Model: "model", Config: config, Gov: DefaultGovernanceOptions(), Ops: &fakeGitHub{}}
	comment := gov.BuildWellFormedComment(context.Background(), Issue{
		Number: 22, Title: "title", Body: "body", User: User{Login: "someone"},
	}, "summary", []map[string]any{{"number": 57}})
	if strings.Contains(comment, "#999") {
		t.Fatalf("hallucinated citation leaked: %s", comment)
	}
	if !strings.Contains(comment, config.Responses["governance_well_formed_comment"]) &&
		!strings.Contains(comment, "已按模板规范填写") {
		t.Fatalf("did not fall back to fixed template: %s", comment)
	}
}

func TestTwoStageCanonicalUsesSharedIndexWithoutSecondFetch(t *testing.T) {
	config := testConfig(t, "zh-CN")
	listCanonicalCalled := false
	ops := &fakeGitHub{
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			listCanonicalCalled = true
			return nil, nil
		},
	}
	history := HistoryContext{
		GitHub: ops, Config: config, Gov: func() GovernanceOptions {
			gov := DefaultGovernanceOptions()
			gov.EnableTwoStage = true
			return gov
		}(), Logger: Logger{},
	}
	shared := &SharedContext{
		History: &history,
		Index: []HistoryIndexItem{{
			Number: 57, Kind: "issue", Title: "cache", Labels: []string{"canonical"},
			State: "closed", Body: "existing",
		}},
	}
	ai := &governanceAI{match: "DUPLICATE(#57)"}
	gov := IssueGovernance{
		AI: ai, Model: "model", Config: config, Gov: history.Gov, Ops: ops,
	}
	// The shared HistoryContext fetches details through fakeGitHub.
	ops.GetIssueDetailFunc = func(_ context.Context, _ string, _ string, number int) (HistoryDetail, error) {
		return HistoryDetail{Number: number, Kind: "issue", Title: "cache", State: "closed", Body: "existing"}, nil
	}
	result, err := gov.Govern(context.Background(), "o", "r", Issue{
		Number: 22, Title: "need cache", Body: "please add cache", User: User{Login: "someone"},
	}, "", shared)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != GovernanceDuplicate {
		t.Fatalf("result = %+v", result)
	}
	if listCanonicalCalled {
		t.Fatal("two-stage path fetched canonical index a second time")
	}
}
