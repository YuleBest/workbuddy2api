package aigovernance

import (
	"context"
	"fmt"
	"strings"
)

type Orchestrator struct {
	Config Config
	Inputs Inputs
	Read   GitHub
	Write  GitHub
	AI     AI
	Logger Logger
}

func (o Orchestrator) Handle(ctx context.Context, event EventContext) error {
	switch {
	case event.Name == "issues" && event.Action == "opened":
		if event.Issue == nil {
			return fmt.Errorf("issues.opened event has no issue payload")
		}
		return o.HandleIssue(ctx, event.Owner, event.Repo, *event.Issue)
	case event.Name == "pull_request_target" && event.Action == "opened":
		if event.PullRequest == nil {
			return fmt.Errorf("pull_request_target.opened event has no pull_request payload")
		}
		if !o.Inputs.Governance.EnablePRGovernance {
			o.Logger.Info("PR 治理默认关闭（enable-pr-governance=false），跳过")
			return nil
		}
		return o.HandlePR(ctx, event.Owner, event.Repo, *event.PullRequest)
	default:
		o.Logger.LogTemplate("event_no_match", o.Config, nil)
		return nil
	}
}

func (o Orchestrator) HandleIssue(ctx context.Context, owner, repo string, issue Issue) error {
	author := strings.ToLower(issue.User.Login)
	o.Logger.LogTemplate("issue_check_start", o.Config, map[string]string{"title": issue.Title})
	o.Logger.LogTemplate("target_repo", o.Config, map[string]string{"owner": owner, "repo": repo})
	if containsFold(o.Inputs.Governance.SkipUsers, author) {
		o.Logger.LogTemplate("governance_skip_user", o.Config, map[string]string{"number": itoa(issue.Number)})
		return nil
	}
	if containsFold(o.Inputs.BlacklistUsers, author) {
		o.closeIssueWithType(ctx, owner, repo, issue, "issue_blacklist", "issue_blacklist_log", true, "not_planned")
		return nil
	}
	skipGovernance := false
	if o.Inputs.Governance.MaintainerExempt {
		isMaintainer, _ := o.Read.CheckCollaborator(ctx, owner, repo, author)
		if isMaintainer {
			o.Logger.LogTemplate("governance_skip_maintainer", o.Config, map[string]string{"number": itoa(issue.Number)})
			skipGovernance = true
		}
	}

	readme, err := o.Read.GetReadme(ctx, owner, repo)
	if err != nil {
		o.Logger.LogTemplate("readme_fetch_failed", o.Config, nil)
		readme = ""
	}
	if readme != "" {
		o.Logger.LogTemplate("readme_found", o.Config, nil)
	} else {
		o.Logger.LogTemplate("readme_not_found", o.Config, nil)
	}
	pinned, err := o.Read.PinnedIssuesContent(ctx, owner, repo)
	if err != nil {
		o.Logger.LogTemplate("pinned_issues_fetch_failed", o.Config, nil)
		pinned = ""
	}
	if pinned != "" {
		o.Logger.LogTemplate("pinned_issues_found", o.Config, nil)
	} else {
		o.Logger.LogTemplate("pinned_issues_not_found", o.Config, nil)
	}

	quality := AnalyzeIssueQuality(issue.Title, issue.Body)
	report := GenerateAnalysisReport(quality.TemplateInfo, quality.ContentInfo)
	o.logTemplateAnalysis(quality)
	analyzer := Analyzer{AI: o.AI, Model: o.Inputs.AIModel, Config: o.Config, Logger: o.Logger}
	detection, err := analyzer.AnalyzeIssue(ctx, issue, readme, pinned, report)
	if err != nil {
		o.Logger.LogTemplate("issue_process_error", o.Config, map[string]string{"error": err.Error()})
		if IsContentFilterError(err) {
			o.Logger.LogTemplate("ai_content_filtered", o.Config, map[string]string{"number": itoa(issue.Number)})
			o.closeIssueWithType(ctx, owner, repo, issue, "issue_content_filtered", "issue_content_filtered_log", false, "not_planned")
			return nil
		}
		o.failOpenComment(ctx, owner, repo, issue.Number)
		o.Logger.LogTemplate("governance_ai_fallback_log", o.Config, nil)
		return nil
	}
	switch detection.Decision {
	case DecisionSpam:
		o.closeIssueWithType(ctx, owner, repo, issue, "issue_spam", "issue_spam_log", true, "not_planned")
		return nil
	case DecisionReadmeCovered:
		answer, answerErr := analyzer.GenerateReadmeAnswer(ctx, issue, readme)
		if answerErr != nil {
			o.Logger.LogTemplate("readme_answer_failed", o.Config, map[string]string{
				"number": itoa(issue.Number), "error": answerErr.Error(),
			})
			return nil
		}
		if err := o.Write.AddComment(ctx, owner, repo, issue.Number, o.Config.Responses["readme_answer_prefix"]+answer); err != nil {
			o.Logger.LogTemplate("readme_answer_failed", o.Config, map[string]string{
				"number": itoa(issue.Number), "error": err.Error(),
			})
			return nil
		}
		o.Logger.LogTemplate("readme_answer_generated", o.Config, map[string]string{"number": itoa(issue.Number)})
		o.closeIssueWithType(ctx, owner, repo, issue, "issue_readme_covered", "issue_readme_covered_log", false, "completed")
		return nil
	}

	triage := o.classifyAndHandleIssue(ctx, owner, repo, issue, quality, readme, analyzer)
	if triage.Closed || triage.NeedsInfo {
		return nil
	}
	if triage.Error {
		o.Logger.LogTemplate("classification_failed", o.Config, map[string]string{"error": "triage error, skip governance"})
		o.failOpenComment(ctx, owner, repo, issue.Number)
		return nil
	}
	if skipGovernance {
		o.Logger.LogTemplate("issue_passed_log", o.Config, map[string]string{"number": itoa(issue.Number)})
		return nil
	}
	shared := o.buildSharedContext(ctx, owner, repo)
	governance := IssueGovernance{
		AI: o.AI, Model: o.Inputs.AIModel, Config: o.Config, Gov: o.Inputs.Governance,
		Ops: o.Write, Logger: o.Logger,
	}
	classification := ResolveLabel(triage.Classification, o.Inputs.Labels)
	if _, err := governance.Govern(ctx, owner, repo, issue, classification, shared); err != nil {
		o.Logger.LogTemplate("issue_process_error", o.Config, map[string]string{"error": err.Error()})
		if IsContentFilterError(err) {
			o.closeIssueWithType(ctx, owner, repo, issue, "issue_content_filtered", "issue_content_filtered_log", false, "not_planned")
			return nil
		}
		o.failOpenComment(ctx, owner, repo, issue.Number)
		o.Logger.LogTemplate("governance_ai_fallback_log", o.Config, nil)
	}
	return nil
}

func (o Orchestrator) classifyAndHandleIssue(ctx context.Context, owner, repo string, issue Issue, quality QualityAnalysis, readme string, analyzer Analyzer) TriageResult {
	o.Logger.LogTemplate("classification_start", o.Config, map[string]string{"number": itoa(issue.Number)})
	o.Logger.LogTemplate("available_labels", o.Config, map[string]string{"labels": strings.Join(o.Inputs.Labels, ", ")})
	content := issue.Body
	if quality.ContentInfo.UserContent != "" {
		content = quality.ContentInfo.UserContent
	}
	classification, err := analyzer.ClassifyIssue(ctx, issue, content, o.Inputs.Labels)
	if err != nil {
		o.Logger.LogTemplate("classification_failed", o.Config, map[string]string{"error": err.Error()})
		return TriageResult{Error: true}
	}
	o.addClassificationLabel(ctx, owner, repo, issue.Number, classification, false)
	if !NeedsDetailedInfo(classification, o.Inputs.Labels) {
		return TriageResult{Classification: classification}
	}
	qualityDecision, err := analyzer.CheckContentQuality(ctx, issue, TemplateAnalysisReport(quality))
	if err != nil {
		return TriageResult{Classification: classification, Error: true}
	}
	switch qualityDecision {
	case DecisionUnclear:
		o.handleUnclear(ctx, owner, repo, issue, readme, analyzer)
		return TriageResult{Classification: classification, NeedsInfo: true}
	case DecisionBasic:
		o.closeIssueWithType(ctx, owner, repo, issue, "issue_basic", "issue_basic_log", true, "not_planned")
		return TriageResult{Classification: classification, Closed: true}
	default:
		return TriageResult{Classification: classification}
	}
}

func (o Orchestrator) handleUnclear(ctx context.Context, owner, repo string, issue Issue, readme string, analyzer Analyzer) {
	if readme != "" {
		answer, err := analyzer.GenerateSmartAnswerForUnclear(ctx, issue, readme)
		if err == nil && answer != "" {
			body := o.Config.Responses["unclear_answer_prefix"] + answer
			if err := o.Write.AddComment(ctx, owner, repo, issue.Number, body); err == nil {
				o.Logger.LogTemplate("unclear_smart_answer_generated", o.Config, map[string]string{"number": itoa(issue.Number)})
				return
			}
		}
	}
	o.Logger.LogTemplate("unclear_fallback_to_standard", o.Config, map[string]string{"number": itoa(issue.Number)})
	if err := o.Write.AddComment(ctx, owner, repo, issue.Number, o.Config.Responses["issue_unclear"]); err != nil {
		o.Logger.LogTemplate("issue_unclear_comment_failed", o.Config, map[string]string{"error": err.Error()})
	}
}

func (o Orchestrator) HandlePR(ctx context.Context, owner, repo string, pr PullRequest) error {
	author := strings.ToLower(pr.User.Login)
	o.Logger.LogTemplate("pr_check_start", o.Config, map[string]string{"title": pr.Title})
	if containsFold(o.Inputs.Governance.SkipUsers, author) {
		o.Logger.LogTemplate("governance_pr_skip_user", o.Config, map[string]string{"number": itoa(pr.Number)})
		return nil
	}
	if containsFold(o.Inputs.BlacklistUsers, author) {
		o.closePRWithType(ctx, owner, repo, pr, "pr_closed", "pr_closed_log", true)
		return nil
	}
	fileChanges := o.analyzeFileChanges(ctx, owner, repo, pr)
	analyzer := Analyzer{AI: o.AI, Model: o.Inputs.AIModel, Config: o.Config, Logger: o.Logger}
	detection, err := analyzer.AnalyzePR(ctx, pr, fileChanges)
	if err != nil {
		return o.handlePRError(ctx, owner, repo, pr, err)
	}
	switch detection.Decision {
	case DecisionSpam:
		o.closePRWithType(ctx, owner, repo, pr, "pr_closed", "pr_closed_log", true)
		return nil
	case DecisionMalicious:
		o.closePRWithType(ctx, owner, repo, pr, "pr_malicious", "pr_malicious_log", true)
		return nil
	case DecisionTrivial:
		o.closePRWithType(ctx, owner, repo, pr, "pr_trivial", "pr_trivial_log", false)
		return nil
	}

	classification := ""
	if len(o.Inputs.Labels) > 0 {
		o.Logger.LogTemplate("classification_start", o.Config, map[string]string{"number": itoa(pr.Number)})
		o.Logger.LogTemplate("available_labels", o.Config, map[string]string{"labels": strings.Join(o.Inputs.Labels, ", ")})
		value, classifyErr := analyzer.ClassifyPR(ctx, pr, o.Inputs.Labels, fileChanges)
		if classifyErr != nil {
			o.Logger.LogTemplate("classification_failed", o.Config, map[string]string{"error": classifyErr.Error()})
		} else {
			classification = o.addClassificationLabel(ctx, owner, repo, pr.Number, value, true)
		}
	}
	if o.Inputs.Governance.MaintainerExempt {
		maintainer, _ := o.Read.CheckCollaborator(ctx, owner, repo, author)
		if maintainer {
			o.Logger.LogTemplate("governance_pr_skip_maintainer", o.Config, map[string]string{"number": itoa(pr.Number)})
			return nil
		}
	}
	shared := o.buildSharedContext(ctx, owner, repo)
	if o.Inputs.Governance.PRReviewClose {
		review := PRReview{AI: o.AI, Model: o.Inputs.AIModel, Config: o.Config, Gov: o.Inputs.Governance, Ops: o.Write, Logger: o.Logger}
		result, reviewErr := review.Review(ctx, owner, repo, pr, fileChanges, shared)
		if reviewErr != nil {
			return o.handlePRError(ctx, owner, repo, pr, reviewErr)
		}
		if result != nil {
			return nil
		}
	}
	governance := NewPRGovernance(o.AI, o.Inputs.AIModel, o.Config, o.Inputs.Governance, o.Write, o.Logger)
	if _, err := governance.Govern(ctx, owner, repo, pr, classification, shared); err != nil {
		return o.handlePRError(ctx, owner, repo, pr, err)
	}
	return nil
}

func (o Orchestrator) handlePRError(ctx context.Context, owner, repo string, pr PullRequest, err error) error {
	o.Logger.LogTemplate("pr_process_error", o.Config, map[string]string{"error": err.Error()})
	if IsContentFilterError(err) {
		o.Logger.LogTemplate("ai_content_filtered", o.Config, map[string]string{"number": itoa(pr.Number)})
		o.closePRWithType(ctx, owner, repo, pr, "pr_content_filtered", "pr_content_filtered_log", false)
		return nil
	}
	o.failOpenComment(ctx, owner, repo, pr.Number)
	o.Logger.LogTemplate("governance_pr_ai_fallback_log", o.Config, nil)
	return nil
}

func (o Orchestrator) analyzeFileChanges(ctx context.Context, owner, repo string, pr PullRequest) string {
	if !o.Config.AISettings.AnalyzeFileChanges {
		o.Logger.LogTemplate("file_analysis_disabled_info", o.Config, nil)
		return o.Config.Logging["file_analysis_disabled"]
	}
	summary, err := o.Read.ListPRFilesSummary(
		ctx, owner, repo, pr.Number,
		o.Config.AISettings.MaxFilesToAnalyze,
		o.Config.AISettings.MaxPatchLinesPerFile,
	)
	if err != nil {
		o.Logger.LogTemplate("file_changes_error", o.Config, map[string]string{"error": err.Error()})
		return o.Config.Logging["file_changes_unavailable"]
	}
	if summary.Total == 0 {
		return o.Config.Logging["no_file_changes"]
	}
	result := summary.Summary
	maxFiles := o.Config.AISettings.MaxFilesToAnalyze
	if summary.Total > maxFiles {
		result += "\n" + Render(o.Config.Logging["file_changes_truncated"], map[string]string{
			"total": itoa(summary.Total), "shown": itoa(maxFiles),
		})
	}
	o.Logger.LogTemplate("file_changes_count", o.Config, map[string]string{"count": itoa(summary.Total)})
	return result
}

func (o Orchestrator) addClassificationLabel(ctx context.Context, owner, repo string, number int, classification string, pr bool) string {
	label := MatchLabel(classification, o.Inputs.Labels)
	if label == "" {
		if pr {
			o.Logger.LogTemplate("pr_label_no_match", o.Config, map[string]string{
				"number": itoa(number), "classification": classification,
			})
		} else {
			o.Logger.LogTemplate("label_no_match", o.Config, map[string]string{
				"number": itoa(number), "classification": classification,
			})
		}
		return ""
	}
	if err := o.Write.AddLabels(ctx, owner, repo, number, []string{label}); err != nil {
		o.Logger.LogTemplate("label_add_failed", o.Config, map[string]string{"error": err.Error()})
		return ""
	}
	o.Logger.LogTemplate("label_added", o.Config, map[string]string{
		"number": itoa(number), "label": label,
	})
	return label
}

func (o Orchestrator) closeIssueWithType(ctx context.Context, owner, repo string, issue Issue, responseKey, logKey string, lock bool, reason string) {
	comment := o.Config.Responses[responseKey]
	if comment == "" && responseKey == "issue_blacklist" {
		comment = o.Config.Responses["issue_spam"]
	}
	if err := o.Write.AddComment(ctx, owner, repo, issue.Number, comment); err != nil {
		o.Logger.LogTemplate("issue_comment_failed", o.Config, map[string]string{"error": err.Error()})
	}
	if err := o.Write.UpdateIssueState(ctx, owner, repo, issue.Number, "closed", &reason); err != nil {
		o.Logger.LogTemplate("issue_close_failed", o.Config, map[string]string{"error": err.Error()})
	}
	if lock {
		if err := o.Write.LockIssue(ctx, owner, repo, issue.Number, o.Config.Defaults.LockReason); err != nil {
			o.Logger.LogTemplate("issue_lock_failed", o.Config, map[string]string{"error": err.Error()})
		}
	}
	o.Logger.LogTemplate(logKey, o.Config, map[string]string{"number": itoa(issue.Number)})
}

func (o Orchestrator) closePRWithType(ctx context.Context, owner, repo string, pr PullRequest, responseKey, logKey string, lock bool) {
	if err := o.Write.AddComment(ctx, owner, repo, pr.Number, o.Config.Responses[responseKey]); err != nil {
		o.Logger.LogTemplate("pr_comment_failed", o.Config, map[string]string{"error": err.Error()})
	}
	if err := o.Write.UpdatePullRequest(ctx, owner, repo, pr.Number, map[string]any{"state": "closed"}); err != nil {
		o.Logger.LogTemplate("pr_close_failed", o.Config, map[string]string{"error": err.Error()})
	}
	if lock {
		if err := o.Write.LockIssue(ctx, owner, repo, pr.Number, o.Config.Defaults.LockReason); err != nil {
			o.Logger.LogTemplate("pr_lock_failed", o.Config, map[string]string{"error": err.Error()})
		}
	}
	o.Logger.LogTemplate(logKey, o.Config, map[string]string{"number": itoa(pr.Number)})
}

func (o Orchestrator) failOpenComment(ctx context.Context, owner, repo string, number int) {
	if err := o.Write.AddComment(ctx, owner, repo, number, o.Config.Responses["governance_ai_fallback_comment"]); err != nil {
		o.Logger.LogTemplate("governance_comment_failed", o.Config, map[string]string{"error": err.Error()})
	}
}

func (o Orchestrator) buildSharedContext(ctx context.Context, owner, repo string) *SharedContext {
	if !o.Inputs.Governance.EnableTwoStage {
		return nil
	}
	history := HistoryContext{GitHub: o.Read, Config: o.Config, Gov: o.Inputs.Governance, Logger: o.Logger}
	return &SharedContext{
		History: &history,
		Index:   history.BuildIndex(ctx, owner, repo),
	}
}

func (o Orchestrator) logTemplateAnalysis(analysis QualityAnalysis) {
	if analysis.TemplateInfo.HasTemplate {
		o.Logger.LogTemplate("template_detected", o.Config, map[string]string{
			"type":       analysis.TemplateInfo.TemplateType,
			"confidence": fmt.Sprintf("%.1f", analysis.TemplateInfo.Confidence),
		})
		o.Logger.LogTemplate("template_analysis", o.Config, map[string]string{
			"sections": itoa(analysis.ContentInfo.ValidSections),
		})
		if analysis.ContentInfo.UserContent != "" {
			o.Logger.LogTemplate("user_content_extracted", o.Config, map[string]string{
				"length": itoa(len([]rune(analysis.ContentInfo.UserContent))),
			})
		}
	}
	o.Logger.LogTemplate("quality_analysis", o.Config, map[string]string{
		"level": analysis.Quality.Level, "score": itoa(analysis.Quality.Score),
	})
}
