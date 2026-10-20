package aigovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var keywordSplit = regexp.MustCompile(`[\s,，。:：;；/|()（）\-_*#>` + "`" + `\t\r\n]+`)
var conventionalPrefix = regexp.MustCompile(`^\w+(\(.+?\))?:\s*`)
var citationPattern = regexp.MustCompile(`#(\d+)`)

type PRReview struct {
	AI     AI
	Model  string
	Config Config
	Gov    GovernanceOptions
	Ops    GitHub
	Logger Logger
}

func (r PRReview) Review(ctx context.Context, owner, repo string, pr PullRequest, fileChanges string, shared *SharedContext) (*GovernanceResult, error) {
	r.Logger.LogTemplate("pr_review_start", r.Config, map[string]string{"number": itoa(pr.Number)})
	related, err := r.CollectRelated(ctx, owner, repo, pr, shared)
	if err != nil {
		r.Logger.LogTemplate("pr_review_fetch_failed", r.Config, map[string]string{
			"number": itoa(pr.Number), "error": err.Error(),
		})
		return nil, nil
	}
	if len(related) == 0 {
		r.Logger.LogTemplate("pr_review_no_related", r.Config, map[string]string{"number": itoa(pr.Number)})
		return nil, nil
	}
	var details []HistoryDetail
	if r.Gov.EnableTwoStage {
		details = r.enrichViaHistory(ctx, owner, repo, shared, related)
	} else {
		details = r.enrichRelated(ctx, owner, repo, related)
	}
	if len(details) == 0 {
		r.Logger.LogTemplate("pr_review_no_related", r.Config, map[string]string{"number": itoa(pr.Number)})
		return nil, nil
	}
	contextPackage, err := r.BuildContextPackage(ctx, owner, repo, pr, fileChanges, details)
	if err != nil {
		return nil, err
	}
	verdict, err := r.Judge(ctx, contextPackage)
	if err != nil {
		return nil, err
	}
	verified := r.VerifyEvidence(verdict, details)
	resultText := verdict.Decision
	if verdict.Decision == ReviewClose && !verified {
		resultText += "（证据未通过校验，回落）"
	}
	r.Logger.LogTemplate("pr_review_result", r.Config, map[string]string{
		"number": itoa(pr.Number), "result": resultText,
	})
	if verdict.Decision != ReviewClose || !verified {
		return nil, nil
	}
	return r.ExecuteClose(ctx, owner, repo, pr, verdict, details)
}

func (r PRReview) CollectRelated(ctx context.Context, owner, repo string, pr PullRequest, shared *SharedContext) ([]HistoryDetail, error) {
	if !r.Gov.EnableTwoStage {
		return r.collectRelatedIssues(ctx, owner, repo, pr), nil
	}
	index := []HistoryIndexItem(nil)
	if shared != nil && shared.History != nil {
		index = shared.Index
	} else {
		history := HistoryContext{GitHub: r.Ops, Config: r.Config, Gov: r.Gov, Logger: r.Logger}
		index = history.BuildIndex(ctx, owner, repo)
	}
	if len(index) == 0 {
		return r.collectRelatedIssues(ctx, owner, repo, pr), nil
	}
	screening := ScreeningService{AI: r.AI, Model: r.Model, Config: r.Config, Gov: r.Gov, Logger: r.Logger}
	candidates := screening.Screen(ctx, HistoryIndexItem{
		Kind: "pr", Number: pr.Number, Title: pr.Title, Body: pr.Body,
	}, index)
	screened := []HistoryDetail{}
	for _, candidate := range candidates {
		for _, item := range index {
			if item.Kind == candidate.Kind && item.Number == candidate.Number {
				screened = append(screened, HistoryDetail{
					Number: item.Number, Kind: item.Kind, Title: item.Title, Body: item.Body,
					State: item.State, StateReason: item.StateReason, ClosedAt: item.ClosedAt,
				})
				break
			}
		}
	}
	keywords := r.collectRelatedIssues(ctx, owner, repo, pr)
	merged := []HistoryDetail{}
	seen := map[string]struct{}{}
	for _, group := range [][]HistoryDetail{screened, keywords} {
		for _, item := range group {
			key := item.Kind + ":" + itoa(item.Number)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, item)
		}
	}
	limit := r.Gov.MaxRelatedIssues
	if len(screened) > limit {
		limit = len(screened)
	}
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func (r PRReview) collectRelatedIssues(ctx context.Context, owner, repo string, pr PullRequest) []HistoryDetail {
	candidates := []HistoryDetail{}
	seen := map[int]struct{}{}
	canonical, err := r.Ops.ListCanonicalIssues(ctx, owner, repo, r.Gov.CanonicalLabel, r.Gov.MaxCanonicalIndex, r.Gov.RelatedBodyTruncate, true)
	if err != nil {
		r.Logger.LogTemplate("pr_review_canonical_failed", r.Config, map[string]string{"error": err.Error()})
	}
	for _, item := range canonical {
		if item.State != "closed" {
			continue
		}
		candidates = append(candidates, HistoryDetail{
			Number: item.Number, Kind: "issue", Title: item.Title, Body: item.Body,
			State: "closed", StateReason: item.StateReason, ClosedAt: item.ClosedAt,
		})
		seen[item.Number] = struct{}{}
	}
	keywords := r.ExtractKeywords(pr)
	if len(keywords) > 0 {
		hits, searchErr := r.Ops.SearchRelatedClosedIssues(ctx, owner, repo, keywords, r.Gov.MaxRelatedIssues*2)
		if searchErr != nil {
			r.Logger.LogTemplate("pr_review_search_failed", r.Config, map[string]string{"error": searchErr.Error()})
		}
		for _, item := range hits {
			if item.IsPR {
				continue
			}
			if _, exists := seen[item.Number]; exists {
				continue
			}
			candidates = append(candidates, HistoryDetail{
				Number: item.Number, Kind: "issue", Title: item.Title, State: item.State,
				StateReason: item.StateReason, ClosedAt: item.ClosedAt,
			})
			seen[item.Number] = struct{}{}
		}
	}
	if r.Gov.MaxRelatedIssues > 0 && len(candidates) > r.Gov.MaxRelatedIssues {
		candidates = candidates[:r.Gov.MaxRelatedIssues]
	}
	return candidates
}

func (r PRReview) ExtractKeywords(pr PullRequest) []string {
	seen := map[string]struct{}{}
	out := []string{}
	appendWords := func(text string) {
		for _, word := range keywordSplit.Split(text, -1) {
			word = strings.TrimSpace(word)
			if len([]rune(word)) <= 1 {
				continue
			}
			if _, exists := seen[word]; exists {
				continue
			}
			seen[word] = struct{}{}
			out = append(out, word)
		}
	}
	appendWords(conventionalPrefix.ReplaceAllString(pr.Title, ""))
	appendWords(pr.Body)
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func (r PRReview) enrichRelated(ctx context.Context, owner, repo string, related []HistoryDetail) []HistoryDetail {
	out := []HistoryDetail{}
	for _, item := range related {
		comments, err := r.Ops.ListIssueComments(ctx, owner, repo, item.Number, r.Gov.RelatedCommentsPerIssue)
		if err != nil {
			r.Logger.LogTemplate("pr_review_enrich_failed", r.Config, map[string]string{
				"number": itoa(item.Number), "error": err.Error(),
			})
			continue
		}
		timeline, err := r.Ops.ListIssueTimeline(ctx, owner, repo, item.Number, 30)
		if err != nil {
			r.Logger.LogTemplate("pr_review_enrich_failed", r.Config, map[string]string{
				"number": itoa(item.Number), "error": err.Error(),
			})
			continue
		}
		item.Comments = comments
		item.Timeline = timeline
		if item.Kind == "" {
			item.Kind = "issue"
		}
		out = append(out, item)
	}
	return out
}

func (r PRReview) enrichViaHistory(ctx context.Context, owner, repo string, shared *SharedContext, related []HistoryDetail) []HistoryDetail {
	history := HistoryContext{GitHub: r.Ops, Config: r.Config, Gov: r.Gov, Logger: r.Logger}
	if shared != nil && shared.History != nil {
		history = *shared.History
	}
	refs := make([]HistoryIndexItem, 0, len(related))
	for _, item := range related {
		kind := item.Kind
		if kind == "" {
			kind = "issue"
		}
		refs = append(refs, HistoryIndexItem{Number: item.Number, Kind: kind})
	}
	return history.Enrich(ctx, owner, repo, refs)
}

func (r PRReview) BuildContextPackage(ctx context.Context, owner, repo string, pr PullRequest, fileChanges string, related []HistoryDetail) (map[string]any, error) {
	commits, err := r.Ops.ListPRCommits(ctx, owner, repo, pr.Number, 20)
	if err != nil {
		r.Logger.LogTemplate("pr_review_commits_failed", r.Config, map[string]string{"error": err.Error()})
		commits = nil
	}
	commitMessages := make([]string, 0, len(commits))
	for _, commit := range commits {
		commitMessages = append(commitMessages, truncateMarked(commit.Message, 200))
	}
	relatedItems := make([]map[string]any, 0, len(related))
	for _, item := range related {
		comments := make([]map[string]any, 0, len(item.Comments))
		for _, comment := range item.Comments {
			comments = append(comments, map[string]any{
				"author": comment.Author, "body": truncateMarked(comment.Body, 500),
			})
		}
		timeline := filterTimeline(item.Timeline)
		timelineOut := make([]map[string]any, 0, len(timeline))
		for _, event := range timeline {
			timelineOut = append(timelineOut, map[string]any{
				"event": event.Event, "actor": event.Actor, "commit_id": event.CommitID,
			})
		}
		kind := item.Kind
		if kind == "" {
			kind = "issue"
		}
		relatedItems = append(relatedItems, map[string]any{
			"number": item.Number, "kind": kind, "title": item.Title,
			"state": item.State, "state_reason": item.StateReason,
			"body":     truncateMarked(item.Body, r.Gov.RelatedBodyTruncate),
			"comments": comments, "timeline": timelineOut,
		})
	}
	return map[string]any{
		"pr": map[string]any{
			"number": pr.Number, "title": pr.Title,
			"body":   truncateMarked(pr.Body, r.Gov.RelatedBodyTruncate),
			"author": pr.User.Login, "files_changed": truncateMarked(fileChanges, r.Gov.RelatedBodyTruncate),
			"commits": commitMessages,
		},
		"related_issues": relatedItems,
	}, nil
}

type ReviewVerdict struct {
	Decision string
	Reasons  []string
	Evidence []string
}

func (r PRReview) Judge(ctx context.Context, contextPackage map[string]any) (ReviewVerdict, error) {
	input, _ := json.Marshal(contextPackage)
	result, err := r.AI.Structured(ctx, r.Model, AIRequest{
		Instructions: r.Config.Prompts["pr_historical_review"],
		Input:        string(input),
	}, r.Config, AICallOptions{Purpose: "PR 历史语境评审", Normalize: false}, map[string]any{
		"decision": "UNCERTAIN", "reasons": []any{}, "evidence": []any{},
	})
	if err != nil {
		return ReviewVerdict{}, err
	}
	if result == nil {
		return ReviewVerdict{Decision: ReviewUncertain}, nil
	}
	decision, _ := result["decision"].(string)
	decision = strings.ToUpper(strings.TrimSpace(decision))
	if decision != ReviewClose && decision != ReviewKeep && decision != ReviewUncertain {
		decision = ReviewUncertain
	}
	return ReviewVerdict{
		Decision: decision,
		Reasons:  stringSlice(result["reasons"]),
		Evidence: stringSlice(result["evidence"]),
	}, nil
}

func (r PRReview) VerifyEvidence(verdict ReviewVerdict, related []HistoryDetail) bool {
	if verdict.Decision != ReviewClose {
		return true
	}
	byNumber := map[int]HistoryDetail{}
	for _, item := range related {
		byNumber[item.Number] = item
	}
	type validLine struct {
		text    string
		numbers []int
	}
	valid := []validLine{}
	for _, line := range verdict.Evidence {
		numbers := []int{}
		for _, match := range citationPattern.FindAllStringSubmatch(line, -1) {
			number, _ := parseStrictInt(match[1])
			if _, exists := byNumber[number]; exists {
				numbers = append(numbers, number)
			}
		}
		if len(numbers) > 0 {
			valid = append(valid, validLine{text: line, numbers: numbers})
		}
	}
	if len(valid) == 0 {
		r.Logger.LogTemplate("pr_review_evidence_rejected", r.Config, nil)
		return false
	}
	if len(valid) >= 2 {
		return true
	}
	line := valid[0]
	lower := strings.ToLower(line.text)
	for _, number := range line.numbers {
		for _, keyword := range EvidenceKeywords(byNumber[number]) {
			if strings.Contains(lower, keyword) {
				return true
			}
		}
	}
	r.Logger.LogTemplate("pr_review_evidence_rejected", r.Config, nil)
	return false
}

func EvidenceKeywords(item HistoryDetail) []string {
	aliases := map[string][]string{
		"wontfix":     {"wontfix", "not_planned"},
		"not_planned": {"not_planned", "wontfix"},
		"duplicate":   {"duplicate"},
		"completed":   {"completed"},
		"merged":      {"merged"},
	}
	haystack := []string{}
	if item.StateReason != nil {
		haystack = append(haystack, *item.StateReason)
	}
	for _, event := range item.Timeline {
		haystack = append(haystack, event.Event)
	}
	if item.State == "merged" {
		haystack = append(haystack, "merged")
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range haystack {
		for _, keyword := range aliases[strings.ToLower(value)] {
			if _, exists := seen[keyword]; exists {
				continue
			}
			seen[keyword] = struct{}{}
			out = append(out, keyword)
		}
	}
	return out
}

func (r PRReview) ValidateCitations(comment string, related []HistoryDetail) bool {
	valid := map[int]struct{}{}
	for _, item := range related {
		valid[item.Number] = struct{}{}
	}
	for _, match := range citationPattern.FindAllStringSubmatch(comment, -1) {
		number, _ := parseStrictInt(match[1])
		if _, exists := valid[number]; !exists {
			return false
		}
	}
	return true
}

func (r PRReview) ExecuteClose(ctx context.Context, owner, repo string, pr PullRequest, verdict ReviewVerdict, related []HistoryDetail) (*GovernanceResult, error) {
	comment, err := r.DraftReviewComment(ctx, pr, verdict, related)
	if err != nil {
		r.Logger.LogTemplate("pr_review_comment_failed", r.Config, map[string]string{
			"number": itoa(pr.Number), "error": err.Error(),
		})
		return nil, nil
	}
	if !r.ValidateCitations(comment, related) {
		r.Logger.LogTemplate("pr_review_citation_rejected", r.Config, map[string]string{"number": itoa(pr.Number)})
		return nil, nil
	}
	if len(citationPattern.FindAllString(comment, -1)) > 0 {
		comment += "\n\n" + r.Config.Responses["governance_history_reference_note"]
	}
	if r.Gov.DryRun {
		body := r.Config.Responses["governance_dry_run"] +
			fmt.Sprintf("\n\n**本应执行**：关闭 PR（历史语境评审判定 CLOSE，证据已校验；打标签 %s）\n\n---\n\n%s",
				r.Gov.HistoryRejectedLabel, comment)
		r.safeComment(ctx, owner, repo, pr.Number, body)
		return &GovernanceResult{Decision: ReviewClose, DryRun: true}, nil
	}
	if err := r.Ops.AddLabels(ctx, owner, repo, pr.Number, []string{r.Gov.HistoryRejectedLabel}); err != nil {
		r.Logger.LogTemplate("pr_review_label_failed", r.Config, map[string]string{"error": err.Error()})
	}
	r.safeComment(ctx, owner, repo, pr.Number, comment)
	if err := r.Ops.UpdatePullRequest(ctx, owner, repo, pr.Number, map[string]any{"state": "closed"}); err != nil {
		r.Logger.LogTemplate("pr_review_close_failed", r.Config, map[string]string{
			"number": itoa(pr.Number), "error": err.Error(),
		})
	} else {
		r.Logger.LogTemplate("pr_review_closed", r.Config, map[string]string{"number": itoa(pr.Number)})
	}
	return &GovernanceResult{Decision: ReviewClose, Closed: true}, nil
}

func (r PRReview) DraftReviewComment(ctx context.Context, pr PullRequest, verdict ReviewVerdict, related []HistoryDetail) (string, error) {
	relatedInput := make([]map[string]any, 0, len(related))
	for _, item := range related {
		kind := item.Kind
		if kind == "" {
			kind = "issue"
		}
		relatedInput = append(relatedInput, map[string]any{
			"number": item.Number, "kind": kind, "title": item.Title,
			"state": item.State, "state_reason": item.StateReason,
		})
	}
	input, _ := json.Marshal(map[string]any{
		"pr_number": pr.Number, "author": pr.User.Login, "decision": verdict.Decision,
		"reasons": verdict.Reasons, "evidence": verdict.Evidence, "related_issues": relatedInput,
	})
	result, err := r.AI.Structured(ctx, r.Model, AIRequest{
		Instructions: r.Config.Prompts["pr_review_comment"],
		Input:        string(input),
	}, r.Config, AICallOptions{Purpose: "PR 评审评论起草", Normalize: false, MaxTokens: 2000}, map[string]any{"comment": ""})
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("评审评论草稿为空")
	}
	text, _ := result["comment"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("评审评论草稿为空")
	}
	return WithLogLine(withBotPrefix(text), "pr_historical_review_close", r.Config), nil
}

func (r PRReview) safeComment(ctx context.Context, owner, repo string, number int, body string) {
	if err := r.Ops.AddComment(ctx, owner, repo, number, body); err != nil {
		r.Logger.LogTemplate("governance_comment_failed", r.Config, map[string]string{"error": err.Error()})
	}
}

func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, fmt.Sprint(item))
	}
	return out
}
