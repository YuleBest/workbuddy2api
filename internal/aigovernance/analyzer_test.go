package aigovernance

import (
	"context"
	"testing"
)

func TestAnalyzeIssueShortCircuitsSpam(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ai := &fakeAI{
		CallFunc: func(_ context.Context, _ string, _ AIRequest, _ Config, options AICallOptions) (string, error) {
			if options.Purpose != "垃圾检测" {
				t.Fatalf("unexpected call after spam: %s", options.Purpose)
			}
			return "SPAM", nil
		},
	}
	analyzer := Analyzer{AI: ai, Model: "model", Config: config, Logger: Logger{}}
	result, err := analyzer.AnalyzeIssue(context.Background(), Issue{Title: "spam", Body: "buy now"}, "", "", "report")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionSpam || result.Step != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(ai.Calls) != 1 {
		t.Fatalf("AI calls = %d", len(ai.Calls))
	}
}

func TestAnalyzeIssueReadmeCoverageShortCircuits(t *testing.T) {
	config := testConfig(t, "zh-CN")
	responses := []string{"NOT_SPAM", "COVERED"}
	ai := &fakeAI{
		CallFunc: func(_ context.Context, _ string, _ AIRequest, _ Config, _ AICallOptions) (string, error) {
			value := responses[0]
			responses = responses[1:]
			return value, nil
		},
	}
	analyzer := Analyzer{AI: ai, Model: "model", Config: config, Logger: Logger{}}
	result, err := analyzer.AnalyzeIssue(context.Background(), Issue{Title: "how", Body: "install"}, "# README", "", "report")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionReadmeCovered || result.Step != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestAnalyzePRInvalidCommitIsDeterministic(t *testing.T) {
	config := testConfig(t, "zh-CN")
	calls := 0
	ai := &fakeAI{
		CallFunc: func(_ context.Context, _ string, _ AIRequest, _ Config, _ AICallOptions) (string, error) {
			calls++
			return "NOT_SPAM", nil
		},
	}
	analyzer := Analyzer{AI: ai, Model: "model", Config: config, Logger: Logger{}}
	result, err := analyzer.AnalyzePR(context.Background(), PullRequest{Title: "Update README.md", Body: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionInvalidCommit || result.Step != 2 {
		t.Fatalf("result = %+v", result)
	}
	if calls != 1 {
		t.Fatalf("commit check used AI: calls=%d", calls)
	}
}

func TestAnalyzePRQualityDecisions(t *testing.T) {
	config := testConfig(t, "zh-CN")
	for _, decision := range []string{DecisionMalicious, DecisionTrivial, DecisionUnclear, DecisionKeep} {
		t.Run(decision, func(t *testing.T) {
			responses := []string{"NOT_SPAM", decision}
			ai := &fakeAI{
				CallFunc: func(_ context.Context, _ string, _ AIRequest, _ Config, _ AICallOptions) (string, error) {
					value := responses[0]
					responses = responses[1:]
					return value, nil
				},
			}
			analyzer := Analyzer{AI: ai, Model: "model", Config: config, Logger: Logger{}}
			result, err := analyzer.AnalyzePR(context.Background(), PullRequest{
				Title: "feat: cache", Body: "add cache",
			}, "a.go(+1/-1)")
			if err != nil {
				t.Fatal(err)
			}
			want := decision
			if decision == "KEEP" {
				want = DecisionKeep
			}
			if result.Decision != want || result.Step != 3 {
				t.Fatalf("result = %+v, want %s", result, want)
			}
		})
	}
}

func TestMatchLabelValidation(t *testing.T) {
	labels := []string{"bug", "enhancement", "question"}
	if got := MatchLabel("BUG", labels); got != "bug" {
		t.Fatalf("exact match = %q", got)
	}
	if got := MatchLabel("type: enhancement", labels); got != "enhancement" {
		t.Fatalf("contains match = %q", got)
	}
	if got := MatchLabel("documentation", labels); got != "" {
		t.Fatalf("unexpected match = %q", got)
	}
}
