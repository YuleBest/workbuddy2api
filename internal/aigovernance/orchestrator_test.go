package aigovernance

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type flowAI struct {
	failPurpose          string
	contentFilterPurpose string
}

func (a *flowAI) Call(_ context.Context, _ string, _ AIRequest, _ Config, options AICallOptions) (string, error) {
	if options.Purpose == a.contentFilterPurpose {
		return "", &AIError{
			Message:    "content_filter blocked",
			StatusCode: 400,
			Body:       map[string]any{"error": map[string]any{"code": "content_filter"}},
		}
	}
	if options.Purpose == a.failPurpose {
		return "", fmt.Errorf("AI failure")
	}
	switch options.Purpose {
	case "垃圾检测", "PR垃圾检测":
		return "NOT_SPAM", nil
	case "README覆盖检查":
		return "NOT_COVERED", nil
	case "Issue分类":
		return "UNKNOWN", nil
	case "PR分类":
		return "NONE", nil
	case "PR质量检查":
		return "KEEP", nil
	case "规范判定":
		return "WELL_FORMED", nil
	case "归并匹配":
		return "UNCERTAIN", nil
	case "生成规范 issue 评审评论":
		return "Thanks for the detailed issue.\n\n## Analysis\nLooks actionable.\n\n## Next\nPlease open a PR.", nil
	default:
		return "", fmt.Errorf("unexpected AI call: %s", options.Purpose)
	}
}

func (a *flowAI) Structured(_ context.Context, _ string, _ AIRequest, _ Config, options AICallOptions, _ map[string]any) (map[string]any, error) {
	if options.Purpose == a.contentFilterPurpose {
		return nil, &AIError{
			Message:    "content_filter blocked",
			StatusCode: 400,
			Body:       map[string]any{"error": map[string]any{"code": "content_filter"}},
		}
	}
	if options.Purpose == a.failPurpose {
		return nil, fmt.Errorf("AI failure")
	}
	switch options.Purpose {
	case "要点提炼":
		return map[string]any{"要点": "support cache", "要做的事": []any{"implement"}}, nil
	case "历史语境筛选":
		return map[string]any{"candidates": []any{
			map[string]any{"number": float64(57), "kind": "issue", "relevance": "direct"},
		}}, nil
	case "PR 历史语境评审":
		return map[string]any{"decision": "KEEP", "reasons": []any{}, "evidence": []any{}}, nil
	default:
		return nil, fmt.Errorf("unexpected structured AI call: %s", options.Purpose)
	}
}

func TestRunnerProcessesLocalIssueEventWithoutDestructiveWrites(t *testing.T) {
	payload := []byte(`{
	  "action":"opened",
	  "issue":{"number":22,"title":"Please support cache","body":"A useful cache feature.","user":{"login":"someone"}},
	  "repository":{"full_name":"owner/repo"}
	}`)
	env := map[string]string{
		"GITHUB_EVENT_NAME":       "issues",
		"GITHUB_REPOSITORY":       "owner/repo",
		"INPUT_GITHUB_TOKEN":      "token",
		"INPUT_LABELS":            "enhancement",
		"INPUT_DRY_RUN":           "true",
		"INPUT_MAINTAINER_EXEMPT": "false",
	}
	ops := &fakeGitHub{}
	logger, _, _ := testLogger()
	runner := Runner{
		Getenv:       func(key string) string { return env[key] },
		Logger:       logger,
		EventPayload: payload,
		ReadGitHub:   ops, WriteGitHub: ops,
		AI: &flowAI{},
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ops.IssueStates) != 0 || len(ops.Labels) != 0 || len(ops.Created) != 0 {
		t.Fatalf("dry run had destructive writes: %+v", ops)
	}
	if len(ops.Comments) != 1 || !strings.Contains(ops.Comments[0], "本应执行") {
		t.Fatalf("expected dry-run comment, got %v", ops.Comments)
	}
}

func TestIssueAIFailureFailOpenDoesNotClose(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{}
	logger, _, _ := testLogger()
	orchestrator := Orchestrator{
		Config: config,
		Inputs: Inputs{
			Labels: []string{"enhancement"},
			Governance: GovernanceOptions{
				SkipUsers: DefaultSkipUsers, MaintainerExempt: false,
			},
		},
		Read: ops, Write: ops, AI: &flowAI{failPurpose: "垃圾检测"}, Logger: logger,
	}
	err := orchestrator.HandleIssue(context.Background(), "o", "r", Issue{
		Number: 22, Title: "title", Body: "body", User: User{Login: "someone"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops.IssueStates) != 0 {
		t.Fatalf("fail-open closed issue: %v", ops.IssueStates)
	}
	if len(ops.Comments) == 0 || !strings.Contains(ops.Comments[0], "放行") {
		t.Fatalf("fail-open comment missing: %v", ops.Comments)
	}
}

func TestPRGovernanceFailureFailOpenDoesNotClose(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{}
	logger, _, _ := testLogger()
	orchestrator := Orchestrator{
		Config: config,
		Inputs: Inputs{
			Labels: []string{"enhancement"},
			Governance: GovernanceOptions{
				SkipUsers: DefaultSkipUsers, MaintainerExempt: false,
			},
		},
		Read: ops, Write: ops, AI: &flowAI{failPurpose: "要点提炼"}, Logger: logger,
	}
	err := orchestrator.HandlePR(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "feat: cache", Body: "body", User: User{Login: "someone"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range ops.PRPatches {
		if patch["state"] == "closed" {
			t.Fatalf("PR was closed on governance failure: %v", patch)
		}
	}
	if len(ops.Comments) == 0 || !strings.Contains(ops.Comments[0], "放行") {
		t.Fatalf("fail-open comment missing: %v", ops.Comments)
	}
}

func TestTwoStageBuildsHistoryIndexOncePerRun(t *testing.T) {
	config := testConfig(t, "zh-CN")
	searchCalls := 0
	canonicalCalls := 0
	ops := &fakeGitHub{
		SearchIssuesAndPRsFunc: func(context.Context, string, string, int) ([]HistoryIndexItem, error) {
			searchCalls++
			return []HistoryIndexItem{{
				Number: 57, Kind: "issue", Title: "cache", Labels: []string{"canonical"},
				State: "closed", Body: "existing",
			}}, nil
		},
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			canonicalCalls++
			return []CanonicalItem{{
				Number: 57, Title: "cache", Body: "existing", State: "closed",
				StateReason: stringPointer("not_planned"),
			}}, nil
		},
		GetIssueDetailFunc: func(_ context.Context, _ string, _ string, number int) (HistoryDetail, error) {
			return HistoryDetail{Number: number, Kind: "issue", Title: "cache", Body: "existing", State: "closed",
				StateReason: stringPointer("not_planned")}, nil
		},
	}
	inputs, err := ParseInputs(&config, func(key string) string {
		switch key {
		case "INPUT_ENABLE_TWO_STAGE":
			return "true"
		case "INPUT_PR_REVIEW_CLOSE":
			return "true"
		case "INPUT_ENABLE_PR_GOVERNANCE":
			return "true"
		case "INPUT_LABELS":
			return "none"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	logger, _, _ := testLogger()
	orchestrator := Orchestrator{
		Config: config, Inputs: inputs, Read: ops, Write: ops, AI: &flowAI{}, Logger: logger,
	}
	if err := orchestrator.HandlePR(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "feat: cache", Body: "add cache", User: User{Login: "someone"},
	}); err != nil {
		t.Fatal(err)
	}
	if searchCalls != 1 {
		t.Fatalf("history search calls = %d, want 1", searchCalls)
	}
	if canonicalCalls == 0 {
		t.Fatal("canonical fallback was not consulted")
	}
}

func TestIssueContentFilterClosesWithoutLock(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{}
	logger, _, _ := testLogger()
	orchestrator := Orchestrator{
		Config: config,
		Inputs: Inputs{
			Labels: []string{"enhancement"},
			Governance: GovernanceOptions{
				SkipUsers: DefaultSkipUsers, MaintainerExempt: false,
			},
		},
		Read: ops, Write: ops, AI: &flowAI{contentFilterPurpose: "垃圾检测"}, Logger: logger,
	}
	if err := orchestrator.HandleIssue(context.Background(), "o", "r", Issue{
		Number: 22, Title: "title", Body: "body", User: User{Login: "someone"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(ops.IssueStates) != 1 || ops.IssueStates[0] != "closed" {
		t.Fatalf("content-filtered issue not closed: %v", ops.IssueStates)
	}
	if len(ops.Locks) != 0 {
		t.Fatalf("content-filtered issue was locked: %v", ops.Locks)
	}
	if len(ops.Comments) == 0 || !strings.Contains(ops.Comments[0], "内容安全策略") {
		t.Fatalf("missing content-filter comment: %v", ops.Comments)
	}
}

func TestPRContentFilterClosesWithoutLock(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{}
	logger, _, _ := testLogger()
	orchestrator := Orchestrator{
		Config: config,
		Inputs: Inputs{
			Labels: []string{"enhancement"},
			Governance: GovernanceOptions{
				SkipUsers: DefaultSkipUsers, MaintainerExempt: false,
			},
		},
		Read: ops, Write: ops, AI: &flowAI{contentFilterPurpose: "要点提炼"}, Logger: logger,
	}
	if err := orchestrator.HandlePR(context.Background(), "o", "r", PullRequest{
		Number: 42, Title: "feat: cache", Body: "body", User: User{Login: "someone"},
	}); err != nil {
		t.Fatal(err)
	}
	closed := false
	for _, patch := range ops.PRPatches {
		if patch["state"] == "closed" {
			closed = true
		}
	}
	if !closed {
		t.Fatal("content-filtered PR was not closed")
	}
	if len(ops.Locks) != 0 {
		t.Fatalf("content-filtered PR was locked: %v", ops.Locks)
	}
}
