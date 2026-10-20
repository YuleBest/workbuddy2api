package aigovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type DetectionResult struct {
	Decision string
	Step     int
}

type TriageResult struct {
	Classification string
	Closed         bool
	NeedsInfo      bool
	Error          bool
}

type Analyzer struct {
	AI     AI
	Model  string
	Config Config
	Logger Logger
}

func (a Analyzer) DetectSpam(ctx context.Context, issue Issue, report string) (string, error) {
	return a.callJSONInstruction(ctx, "spam_detection", map[string]any{
		"type": "issue", "title": issue.Title, "body": issue.Body, "templateAnalysis": report,
	}, "垃圾检测")
}

func (a Analyzer) CheckReadmeCoverage(ctx context.Context, issue Issue, readme, pinned string) (string, error) {
	if readme == "" && pinned == "" {
		return "NOT_COVERED", nil
	}
	return a.callJSONInstruction(ctx, "readme_coverage_check", map[string]any{
		"readme": readme, "pinnedIssues": pinned,
		"issue": map[string]any{"title": issue.Title, "body": issue.Body},
	}, "README覆盖检查")
}

func (a Analyzer) CheckContentQuality(ctx context.Context, issue Issue, report string) (string, error) {
	return a.callJSONInstruction(ctx, "content_quality_check", map[string]any{
		"title": issue.Title, "body": issue.Body, "templateAnalysis": report,
	}, "内容质量检查")
}

func (a Analyzer) AnalyzeIssue(ctx context.Context, issue Issue, readme, pinned, report string) (DetectionResult, error) {
	a.Logger.LogTemplate("spam_check_start", a.Config, nil)
	spam, err := a.DetectSpam(ctx, issue, report)
	if err != nil {
		return DetectionResult{}, err
	}
	a.Logger.LogTemplate("spam_check_result", a.Config, map[string]string{"result": spam})
	if spam == DecisionSpam {
		return DetectionResult{Decision: DecisionSpam, Step: 1}, nil
	}
	a.Logger.LogTemplate("readme_check_start", a.Config, nil)
	coverage, err := a.CheckReadmeCoverage(ctx, issue, readme, pinned)
	if err != nil {
		return DetectionResult{}, err
	}
	a.Logger.LogTemplate("readme_check_result", a.Config, map[string]string{"result": coverage})
	if coverage == "COVERED" {
		return DetectionResult{Decision: DecisionReadmeCovered, Step: 2}, nil
	}
	return DetectionResult{Decision: DecisionKeep, Step: 2}, nil
}

func (a Analyzer) GenerateReadmeAnswer(ctx context.Context, issue Issue, readme string) (string, error) {
	instructions := Render(a.Config.Prompts["readme_answer"], map[string]string{
		"readme_answer_length": a.Config.Locale.ReadmeAnswerLength,
		"answer_language":      a.Config.Locale.AnswerLanguage,
	})
	input, _ := json.Marshal(map[string]any{
		"readme": readme,
		"issue":  map[string]any{"title": issue.Title, "body": issue.Body},
	})
	return a.AI.Call(ctx, a.Model, AIRequest{Instructions: instructions, Input: string(input)}, a.Config, AICallOptions{
		Purpose: "README回答生成",
	})
}

func (a Analyzer) GenerateSmartAnswerForUnclear(ctx context.Context, issue Issue, readme string) (string, error) {
	if readme == "" {
		return "", nil
	}
	instructions := strings.ReplaceAll(a.Config.Prompts["unclear_issue_smart_answer"], "{smart_answer_length}", a.Config.Locale.SmartAnswerLength)
	instructions = strings.ReplaceAll(instructions, "{answer_language}", a.Config.Locale.AnswerLanguage)
	input, _ := json.Marshal(map[string]any{
		"readme": readme,
		"issue":  map[string]any{"title": issue.Title, "body": issue.Body},
	})
	result, err := a.AI.Call(ctx, a.Model, AIRequest{Instructions: instructions, Input: string(input)}, a.Config, AICallOptions{
		Purpose: "UNCLEAR智能回答生成",
	})
	if err != nil {
		return "", err
	}
	normalized := strings.ToUpper(result)
	if strings.HasPrefix(normalized, "HELPFUL_ANSWER:") {
		return strings.TrimSpace(result[len("HELPFUL_ANSWER:"):]), nil
	}
	if normalized == "NEED_MORE_INFO" {
		return "", nil
	}
	if len([]rune(result)) > 10 {
		return result, nil
	}
	return "", nil
}

func (a Analyzer) DetectPRSpam(ctx context.Context, pr PullRequest, fileChanges string) (string, error) {
	return a.callJSONInstruction(ctx, "pr_spam_detection", map[string]any{
		"title": pr.Title, "body": pr.Body, "fileChanges": fileChanges,
	}, "PR垃圾检测")
}

func (a Analyzer) CheckPRQuality(ctx context.Context, pr PullRequest, fileChanges string) (string, error) {
	return a.callJSONInstruction(ctx, "pr_quality_check", map[string]any{
		"title": pr.Title, "body": pr.Body, "fileChanges": fileChanges,
	}, "PR质量检查")
}

func (a Analyzer) AnalyzePR(ctx context.Context, pr PullRequest, fileChanges string) (DetectionResult, error) {
	a.Logger.LogTemplate("spam_check_start", a.Config, nil)
	spam, err := a.DetectPRSpam(ctx, pr, fileChanges)
	if err != nil {
		return DetectionResult{}, err
	}
	a.Logger.LogTemplate("spam_check_result", a.Config, map[string]string{"result": spam})
	if spam == DecisionSpam {
		return DetectionResult{Decision: DecisionSpam, Step: 1}, nil
	}
	if !IsValidCommitTitle(pr.Title) {
		return DetectionResult{Decision: DecisionInvalidCommit, Step: 2}, nil
	}
	a.Logger.LogTemplate("pr_quality_check_start", a.Config, nil)
	quality, err := a.CheckPRQuality(ctx, pr, fileChanges)
	if err != nil {
		return DetectionResult{}, err
	}
	a.Logger.LogTemplate("pr_quality_check_result", a.Config, map[string]string{"result": quality})
	switch quality {
	case DecisionUnclear:
		return DetectionResult{Decision: DecisionUnclear, Step: 3}, nil
	case DecisionMalicious:
		return DetectionResult{Decision: DecisionMalicious, Step: 3}, nil
	case DecisionTrivial:
		return DetectionResult{Decision: DecisionTrivial, Step: 3}, nil
	default:
		return DetectionResult{Decision: DecisionKeep, Step: 3}, nil
	}
}

func (a Analyzer) ClassifyIssue(ctx context.Context, issue Issue, content string, labels []string) (string, error) {
	return a.classify(ctx, "issue", issue.Title, content, "", labels)
}

func (a Analyzer) ClassifyPR(ctx context.Context, pr PullRequest, labels []string, fileChanges string) (string, error) {
	return a.classify(ctx, "pr", pr.Title, pr.Body, fileChanges, labels)
}

func (a Analyzer) classify(ctx context.Context, kind, title, content, fileChanges string, labels []string) (string, error) {
	upper := make([]string, 0, len(labels))
	for _, label := range labels {
		upper = append(upper, strings.ToUpper(label))
	}
	labelsOptions := strings.Join(upper, "、")
	promptKey := "issue_classification"
	payload := map[string]any{"title": title, "content": content}
	purpose := "Issue分类"
	if kind == "pr" {
		promptKey = "pr_classification"
		payload = map[string]any{"title": title, "description": content, "fileChanges": fileChanges}
		purpose = "PR分类"
	}
	instructions := Render(a.Config.Prompts[promptKey], map[string]string{"labels_options": labelsOptions})
	input, _ := json.Marshal(payload)
	result, err := a.AI.Call(ctx, a.Model, AIRequest{Instructions: instructions, Input: string(input)}, a.Config, AICallOptions{
		Purpose: purpose, Normalize: true,
	})
	if err != nil {
		return "", err
	}
	a.Logger.LogTemplate("ai_call_result", a.Config, map[string]string{"purpose": purpose, "result": result})
	return result, nil
}

func ResolveLabel(classification string, labels []string) string {
	for _, label := range labels {
		if strings.EqualFold(label, classification) {
			return label
		}
	}
	return classification
}

func MatchLabel(classification string, labels []string) string {
	if strings.TrimSpace(classification) == "" {
		return ""
	}
	normalized := strings.ToLower(strings.TrimSpace(classification))
	for _, label := range labels {
		if strings.ToLower(label) == normalized {
			return label
		}
	}
	for _, label := range labels {
		if strings.Contains(normalized, strings.ToLower(label)) {
			return label
		}
	}
	for _, label := range labels {
		if strings.Contains(strings.ToLower(label), normalized) {
			return label
		}
	}
	return ""
}

func NeedsDetailedInfo(classification string, labels []string) bool {
	lower := strings.ToLower(classification)
	if strings.Contains(lower, "bug") || strings.Contains(lower, "error") || strings.Contains(lower, "fix") {
		return true
	}
	for _, label := range labels {
		labelLower := strings.ToLower(label)
		if (strings.Contains(labelLower, "bug") || strings.Contains(labelLower, "error") || strings.Contains(labelLower, "fix")) &&
			labelLower == lower {
			return true
		}
	}
	return false
}

func TemplateAnalysisReport(analysis QualityAnalysis) string {
	if !analysis.TemplateInfo.HasTemplate {
		return "未使用Issue模板，为自由格式内容"
	}
	return fmt.Sprintf("检测到%s模板，置信度%.1f%%，用户填写了%d个有效段落",
		analysis.TemplateInfo.TemplateType, analysis.TemplateInfo.Confidence, analysis.ContentInfo.ValidSections)
}

func (a Analyzer) callJSONInstruction(ctx context.Context, promptKey string, payload map[string]any, purpose string) (string, error) {
	input, _ := json.Marshal(payload)
	return a.AI.Call(ctx, a.Model, AIRequest{
		Instructions: a.Config.Prompts[promptKey],
		Input:        string(input),
	}, a.Config, AICallOptions{Purpose: purpose, Normalize: true})
}
