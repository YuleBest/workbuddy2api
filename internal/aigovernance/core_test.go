package aigovernance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTemplateDetectionExtractionAndQuality(t *testing.T) {
	title := "[BUG] login fails"
	body := "### Description\nLogin fails after setup.\n\n### Expected behavior\nIt should work.\n\n### Actual behavior\nIt does not."
	info := DetectTemplate(title, body)
	if !info.HasTemplate || info.TemplateType != "bug_report" {
		t.Fatalf("template info = %+v", info)
	}
	content := ExtractUserContent(body, info)
	if content.IsEmpty || content.ValidSections < 3 {
		t.Fatalf("content info = %+v", content)
	}
	analysis := AnalyzeIssueQuality(title, body)
	if analysis.Quality.Level != "high" || analysis.Quality.Score < 70 {
		t.Fatalf("quality = %+v", analysis.Quality)
	}
	if !strings.Contains(GenerateAnalysisReport(info, content), "Has template: Yes") {
		t.Fatal("analysis report missing template")
	}
}

func TestScreeningGateRejectsHallucinationsAndStripsBodies(t *testing.T) {
	config := testConfig(t, "zh-CN")
	index := []HistoryIndexItem{
		{Number: 57, Kind: "issue", Title: "cache", Labels: []string{"canonical"}, State: "closed"},
		{Number: 30, Kind: "pr", Title: "feat: cache", State: "merged"},
	}
	ai := &fakeAI{
		StructuredFunc: func(_ context.Context, _ string, request AIRequest, _ Config, _ AICallOptions, _ map[string]any) (map[string]any, error) {
			var payload struct {
				Index []map[string]any `json:"index"`
			}
			if err := json.Unmarshal([]byte(request.Input), &payload); err != nil {
				t.Fatal(err)
			}
			for _, item := range payload.Index {
				if _, exists := item["body"]; exists {
					t.Fatal("screening index leaked body field")
				}
			}
			return map[string]any{"candidates": []any{
				map[string]any{"number": float64(999), "kind": "issue", "relevance": "direct"},
				map[string]any{"number": float64(57), "kind": "issue", "relevance": "direct"},
				map[string]any{"number": float64(30), "kind": "issue", "relevance": "context"},
			}}, nil
		},
	}
	service := ScreeningService{
		AI: ai, Model: "model", Config: config, Gov: DefaultGovernanceOptions(), Logger: Logger{},
	}
	candidates := service.Screen(context.Background(), HistoryIndexItem{Kind: "pr", Number: 42, Title: "cache"}, index)
	if len(candidates) != 1 || candidates[0].Number != 57 || candidates[0].Kind != "issue" {
		t.Fatalf("candidates = %+v", candidates)
	}
}

func TestHistoryIndexCombinesSearchAndCanonicalOnce(t *testing.T) {
	config := testConfig(t, "zh-CN")
	searchCalls := 0
	canonicalCalls := 0
	ops := &fakeGitHub{
		SearchIssuesAndPRsFunc: func(context.Context, string, string, int) ([]HistoryIndexItem, error) {
			searchCalls++
			return []HistoryIndexItem{
				{Number: 10, Kind: "issue", Title: "search issue", Labels: []string{"bug"}, State: "closed"},
				{Number: 30, Kind: "pr", Title: "merged PR", State: "merged"},
			}, nil
		},
		ListCanonicalIssuesFunc: func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error) {
			canonicalCalls++
			return []CanonicalItem{
				{Number: 10, Title: "canonical wins", Body: "canonical body", State: "closed"},
				{Number: 57, Title: "canonical only", Body: "body", State: "closed"},
			}, nil
		},
	}
	history := HistoryContext{GitHub: ops, Config: config, Gov: DefaultGovernanceOptions(), Logger: Logger{}}
	index := history.BuildIndex(context.Background(), "o", "r")
	if searchCalls != 1 || canonicalCalls != 1 {
		t.Fatalf("search=%d canonical=%d", searchCalls, canonicalCalls)
	}
	if len(index) != 3 {
		t.Fatalf("index = %+v", index)
	}
	var item10 HistoryIndexItem
	for _, item := range index {
		if item.Number == 10 {
			item10 = item
		}
	}
	if item10.Title != "canonical wins" || !containsFold(item10.Labels, "canonical") {
		t.Fatalf("canonical did not take precedence: %+v", item10)
	}
}

func TestHistoryEnrichFiltersTimelineAndAddsPRContext(t *testing.T) {
	config := testConfig(t, "zh-CN")
	ops := &fakeGitHub{
		GetIssueDetailFunc: func(_ context.Context, _ string, _ string, number int) (HistoryDetail, error) {
			return HistoryDetail{
				Number: number, Kind: "pr", Title: "cache", Body: strings.Repeat("x", 2000),
				State: "merged",
			}, nil
		},
		ListIssueCommentsFunc: func(context.Context, string, string, int, int) ([]HistoryComment, error) {
			return []HistoryComment{{Author: "a", Body: strings.Repeat("b", 800)}}, nil
		},
		ListIssueTimelineFunc: func(context.Context, string, string, int, int) ([]TimelineEvent, error) {
			return []TimelineEvent{
				{Event: "merged", Actor: "maintainer"},
				{Event: "subscribed", Actor: "noise"},
			}, nil
		},
		ListPRFilesSummaryFunc: func(context.Context, string, string, int, int, int) (PRFilesSummary, error) {
			return PRFilesSummary{Summary: "a.go(+1/-1)", Total: 1}, nil
		},
		ListPRCommitsFunc: func(context.Context, string, string, int, int) ([]PRCommit, error) {
			return []PRCommit{{Message: "feat: cache", Author: "someone"}}, nil
		},
	}
	history := HistoryContext{GitHub: ops, Config: config, Gov: DefaultGovernanceOptions(), Logger: Logger{}}
	details := history.Enrich(context.Background(), "o", "r", []HistoryIndexItem{{Number: 30, Kind: "pr"}})
	if len(details) != 1 {
		t.Fatalf("details = %+v", details)
	}
	if details[0].FilesChanged != "a.go(+1/-1)" || len(details[0].Commits) != 1 {
		t.Fatalf("missing PR context: %+v", details[0])
	}
	if len(details[0].Timeline) != 1 || details[0].Timeline[0].Event != "merged" {
		t.Fatalf("timeline filter failed: %+v", details[0].Timeline)
	}
	if len([]rune(details[0].Comments[0].Body)) > 505 {
		t.Fatalf("comment was not truncated: %d", len([]rune(details[0].Comments[0].Body)))
	}
}
