package aigovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var duplicatePattern = regexp.MustCompile(`(?i)^DUPLICATE\(#?(\d+)\)`)
var issueCitationPattern = regexp.MustCompile(`#(\d+)`)

type SharedContext struct {
	History *HistoryContext
	Index   []HistoryIndexItem
}

type GovernanceResult struct {
	Decision        string
	CanonicalNumber int
	Closed          bool
	DryRun          bool
	Passed          bool
	Promoted        bool
	Linked          bool
	Created         bool
}

type IssueGovernance struct {
	AI     AI
	Model  string
	Config Config
	Gov    GovernanceOptions
	Ops    GitHub
	Logger Logger
}

func (g IssueGovernance) ExtractKeyPoints(ctx context.Context, issue Issue) (KeyPoints, error) {
	g.Logger.LogTemplate("governance_extract_start", g.Config, map[string]string{"number": itoa(issue.Number)})
	input, _ := json.Marshal(map[string]any{"title": strings.TrimSpace(issue.Title), "body": strings.TrimSpace(issue.Body)})
	result, err := g.AI.Structured(ctx, g.Model, AIRequest{
		Instructions: g.Config.Prompts["governance_extract"],
		Input:        string(input),
	}, g.Config, AICallOptions{Purpose: "要点提炼", Normalize: false}, map[string]any{
		"要点":   "",
		"要做的事": []any{},
	})
	if err != nil {
		return KeyPoints{}, err
	}
	if result == nil {
		return KeyPoints{}, fmt.Errorf("要点提炼返回无法解析的内容")
	}
	key, _ := result["要点"].(string)
	todos := []string{}
	if rawTodos, ok := result["要做的事"].([]any); ok {
		for _, raw := range rawTodos {
			if value, ok := raw.(string); ok {
				todos = append(todos, value)
			}
		}
	}
	points := KeyPoints{Key: key, Todos: todos}
	points.Summary = formatSummary(points)
	return points, nil
}

func (g IssueGovernance) CheckWellFormed(ctx context.Context, issue Issue) (string, error) {
	if g.MeetsDeterministicThreshold(issue) {
		g.Logger.Info("Issue #%d 通过确定性完整性检查，判定为规范", issue.Number)
		return GovernanceWellFormed, nil
	}
	input, _ := json.Marshal(map[string]any{"title": strings.TrimSpace(issue.Title), "body": strings.TrimSpace(issue.Body)})
	decision, err := g.AI.Call(ctx, g.Model, AIRequest{
		Instructions: g.Config.Prompts["governance_well_formed"],
		Input:        string(input),
	}, g.Config, AICallOptions{Purpose: "规范判定", Normalize: true})
	if err != nil {
		return "", err
	}
	if decision == GovernanceWellFormed {
		return GovernanceWellFormed, nil
	}
	return "NEEDS_NORMALIZE", nil
}

func (g IssueGovernance) MeetsDeterministicThreshold(issue Issue) bool {
	if len([]rune(strings.TrimSpace(issue.Title))) < g.Gov.WellFormedMinTitleLen {
		return false
	}
	if len([]rune(strings.TrimSpace(issue.Body))) < g.Gov.WellFormedMinBodyLen {
		return false
	}
	analysis := AnalyzeIssueQuality(issue.Title, issue.Body)
	return analysis.TemplateInfo.HasTemplate &&
		analysis.ContentInfo.ValidSections >= g.Gov.WellFormedMinSections
}

func (g IssueGovernance) MatchCanonical(ctx context.Context, issue Issue, canonical []CanonicalItem) (GovernanceResult, error) {
	g.Logger.LogTemplate("governance_match_start", g.Config, map[string]string{"number": itoa(issue.Number)})
	payload := map[string]any{
		"new_issue": map[string]any{
			"number": issue.Number, "title": strings.TrimSpace(issue.Title), "body": strings.TrimSpace(issue.Body),
		},
		"canonical_index": canonical,
	}
	input, _ := json.Marshal(payload)
	raw, err := g.AI.Call(ctx, g.Model, AIRequest{
		Instructions: g.Config.Prompts["governance_merge_match"],
		Input:        string(input),
	}, g.Config, AICallOptions{Purpose: "归并匹配", Normalize: false})
	if err != nil {
		return GovernanceResult{}, err
	}
	match := strings.TrimSpace(raw)
	duplicate := duplicatePattern.FindStringSubmatch(match)
	if len(duplicate) == 2 {
		number, _ := parseStrictInt(duplicate[1])
		found := false
		for _, item := range canonical {
			if item.Number == number {
				found = true
				break
			}
		}
		if !found {
			g.Logger.LogTemplate("governance_match_candidate_rejected", g.Config, map[string]string{"number": itoa(number)})
			return GovernanceResult{Decision: GovernanceUncertain}, nil
		}
		return GovernanceResult{Decision: GovernanceDuplicate, CanonicalNumber: number}, nil
	}
	if strings.HasPrefix(match, "NEW_TOPIC") {
		return GovernanceResult{Decision: GovernanceNewTopic}, nil
	}
	return GovernanceResult{Decision: GovernanceUncertain}, nil
}

func (g IssueGovernance) DraftWellFormedReview(ctx context.Context, issue Issue, related []map[string]any) (string, error) {
	if len(related) > g.Gov.MaxScreenedCandidates {
		related = related[:g.Gov.MaxScreenedCandidates]
	}
	payload := map[string]any{
		"title": strings.TrimSpace(issue.Title), "body": strings.TrimSpace(issue.Body),
		"author": issue.User.Login, "number": issue.Number, "related_history": related,
	}
	input, _ := json.Marshal(payload)
	raw, err := g.AI.Call(ctx, g.Model, AIRequest{
		Instructions: g.Config.Prompts["governance_well_formed_review"],
		Input:        string(input),
	}, g.Config, AICallOptions{Purpose: "生成规范 issue 评审评论"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}

func (g IssueGovernance) DraftCanonical(ctx context.Context, issue Issue, points KeyPoints, classification string) (string, string, error) {
	input, _ := json.Marshal(map[string]any{
		"number": issue.Number, "title": issue.Title, "body": issue.Body, "key_points": points,
	})
	raw, err := g.AI.Call(ctx, g.Model, AIRequest{
		Instructions: g.Config.Prompts["governance_new_issue"],
		Input:        string(input),
	}, g.Config, AICallOptions{Purpose: "起草规范化 issue"})
	if err != nil {
		return "", "", err
	}
	title, body := SplitCanonical(raw, issue.Title, classification)
	return title, body, nil
}

func SplitCanonical(raw, fallbackTitle, classification string) (string, string) {
	text := strings.TrimSpace(raw)
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	title := ""
	if len(lines) > 0 {
		title = strings.TrimSpace(strings.TrimLeft(lines[0], "# "))
	}
	if title == "" {
		title = fallbackTitle
	}
	if !regexp.MustCompile(`^\[(Feature|Bug|Enhancement)\]`).MatchString(title) {
		prefix := "[Feature] "
		if regexp.MustCompile(`(?i)bug|fix`).MatchString(classification) {
			prefix = "[Bug] "
		}
		title = prefix + regexp.MustCompile(`^\[[^\]]*\]\s*`).ReplaceAllString(title, "")
	}
	body := ""
	if len(lines) > 1 {
		body = strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	return title, body
}

func (g IssueGovernance) Govern(ctx context.Context, owner, repo string, issue Issue, classification string, shared *SharedContext) (GovernanceResult, error) {
	g.Logger.LogTemplate("governance_start", g.Config, map[string]string{"number": itoa(issue.Number)})
	if g.Gov.DryRun {
		g.Logger.LogTemplate("governance_dry_run", g.Config, nil)
	}
	points, err := g.ExtractKeyPoints(ctx, issue)
	if err != nil {
		return GovernanceResult{}, err
	}
	wellFormed, err := g.CheckWellFormed(ctx, issue)
	if err != nil {
		return GovernanceResult{}, err
	}
	canonical, err := g.ObtainCanonicalCorpus(ctx, owner, repo, shared, issue)
	if err != nil {
		return GovernanceResult{}, err
	}
	match := GovernanceResult{Decision: GovernanceNewTopic}
	if len(canonical) > 0 {
		match, err = g.MatchCanonical(ctx, issue, canonical)
		if err != nil {
			return GovernanceResult{}, err
		}
	}
	resultLabel := match.Decision
	if match.CanonicalNumber != 0 {
		resultLabel += fmt.Sprintf("(#%d)", match.CanonicalNumber)
	}
	g.Logger.LogTemplate("governance_match_result", g.Config, map[string]string{"result": resultLabel})

	switch {
	case match.Decision == GovernanceDuplicate:
		return g.RouteDuplicate(ctx, owner, repo, issue, points.Summary, match.CanonicalNumber)
	case match.Decision == GovernanceUncertain:
		return g.RouteUncertain(ctx, owner, repo, issue, points.Summary)
	case wellFormed == GovernanceWellFormed:
		history := g.ObtainRelatedHistory(ctx, issue, shared)
		return g.RoutePromoteInPlace(ctx, owner, repo, issue, points.Summary, classification, history)
	default:
		return g.RouteNormalize(ctx, owner, repo, issue, points.Summary, points, classification)
	}
}

func (g IssueGovernance) ObtainCanonicalCorpus(ctx context.Context, owner, repo string, shared *SharedContext, issue Issue) ([]CanonicalItem, error) {
	if !g.Gov.EnableTwoStage || shared == nil {
		g.Logger.LogTemplate("governance_fetch_canonical", g.Config, map[string]string{"label": g.Gov.CanonicalLabel})
		items, err := g.Ops.ListCanonicalIssues(ctx, owner, repo, g.Gov.CanonicalLabel, g.Gov.MaxCanonicalIndex, g.Gov.CanonicalBodyTruncate, true)
		if err != nil {
			g.Logger.LogTemplate("governance_canonical_fetch_failed", g.Config, map[string]string{"error": err.Error()})
			items = nil
		}
		g.Logger.LogTemplate("governance_canonical_count", g.Config, map[string]string{"count": itoa(len(items))})
		return items, nil
	}

	canonicalIndex := []HistoryIndexItem{}
	for _, item := range shared.Index {
		if item.Kind == "issue" && containsFold(item.Labels, g.Gov.CanonicalLabel) {
			canonicalIndex = append(canonicalIndex, item)
		}
	}
	if len(canonicalIndex) == 0 {
		g.Logger.LogTemplate("governance_canonical_count", g.Config, map[string]string{"count": "0"})
		return nil, nil
	}
	screening := ScreeningService{AI: g.AI, Model: g.Model, Config: g.Config, Gov: g.Gov, Logger: g.Logger}
	screened := screening.Screen(ctx, HistoryIndexItem{
		Kind: "issue", Number: issue.Number, Title: issue.Title, Body: issue.Body,
	}, canonicalIndex)
	if len(screened) == 0 {
		limit := g.Gov.MaxCanonicalIndex
		if limit <= 0 || limit > len(canonicalIndex) {
			limit = len(canonicalIndex)
		}
		items := make([]CanonicalItem, 0, limit)
		for _, item := range canonicalIndex[:limit] {
			items = append(items, CanonicalItem{
				Number: item.Number, Title: item.Title, Body: item.Body, State: item.State,
				StateReason: item.StateReason, ClosedAt: item.ClosedAt,
			})
		}
		return items, nil
	}
	if shared.History == nil {
		return nil, nil
	}
	refs := make([]HistoryIndexItem, 0, len(screened))
	for _, candidate := range screened {
		refs = append(refs, HistoryIndexItem{Number: candidate.Number, Kind: "issue"})
	}
	details := shared.History.Enrich(ctx, owner, repo, refs)
	items := make([]CanonicalItem, 0, len(details))
	for _, detail := range details {
		items = append(items, CanonicalItem{
			Number: detail.Number, Title: detail.Title, Body: compactBody(detail, g.Gov.CanonicalBodyTruncate),
			State: detail.State, StateReason: detail.StateReason, ClosedAt: detail.ClosedAt,
		})
	}
	return items, nil
}

func (g IssueGovernance) ObtainRelatedHistory(ctx context.Context, issue Issue, shared *SharedContext) []map[string]any {
	if !g.Gov.EnableTwoStage || shared == nil || len(shared.Index) == 0 {
		return nil
	}
	screening := ScreeningService{AI: g.AI, Model: g.Model, Config: g.Config, Gov: g.Gov, Logger: g.Logger}
	candidates := screening.Screen(ctx, HistoryIndexItem{
		Kind: "issue", Number: issue.Number, Title: issue.Title, Body: issue.Body,
	}, shared.Index)
	out := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		for _, item := range shared.Index {
			if item.Kind == candidate.Kind && item.Number == candidate.Number {
				out = append(out, map[string]any{
					"number": item.Number, "kind": item.Kind, "title": item.Title,
					"state": item.State, "state_reason": item.StateReason,
					"conclusion_snippet": historyReference(item),
				})
				break
			}
		}
	}
	return out
}

func (g IssueGovernance) RouteDuplicate(ctx context.Context, owner, repo string, issue Issue, summary string, canonicalNumber int) (GovernanceResult, error) {
	comment := g.Render("governance_merge_comment", map[string]string{
		"summary": summary, "canonical": itoa(canonicalNumber),
	}, true)
	if g.Gov.DryRun {
		g.safeComment(ctx, owner, repo, issue.Number, g.dryRunBody(comment, fmt.Sprintf("归并到 #%d", canonicalNumber)))
		return GovernanceResult{Decision: GovernanceDuplicate, CanonicalNumber: canonicalNumber, DryRun: true}, nil
	}
	g.safeLabels(ctx, owner, repo, issue.Number, []string{g.Gov.DuplicateLabel})
	g.safeComment(ctx, owner, repo, issue.Number, comment)
	g.safeClose(ctx, owner, repo, issue.Number)
	g.safeRecordMerge(ctx, owner, repo, canonicalNumber, issue, summary)
	g.Logger.LogTemplate("governance_duplicate_log", g.Config, map[string]string{
		"number": itoa(issue.Number), "canonical": itoa(canonicalNumber),
	})
	return GovernanceResult{Decision: GovernanceDuplicate, CanonicalNumber: canonicalNumber, Closed: true}, nil
}

func (g IssueGovernance) RouteUncertain(ctx context.Context, owner, repo string, issue Issue, summary string) (GovernanceResult, error) {
	comment := g.Render("governance_uncertain_comment", map[string]string{"summary": summary}, true)
	if g.Gov.DryRun {
		g.safeComment(ctx, owner, repo, issue.Number, g.dryRunBody(comment, "放行（UNCERTAIN）"))
		return GovernanceResult{Decision: GovernanceUncertain, DryRun: true}, nil
	}
	g.safeComment(ctx, owner, repo, issue.Number, comment)
	return GovernanceResult{Decision: GovernanceUncertain, Passed: true}, nil
}

func (g IssueGovernance) RoutePromoteInPlace(ctx context.Context, owner, repo string, issue Issue, summary, classification string, related []map[string]any) (GovernanceResult, error) {
	labels := []string{g.Gov.CanonicalLabel}
	if classification != "" {
		labels = append(labels, classification)
	}
	comment := g.BuildWellFormedComment(ctx, issue, summary, related)
	if g.Gov.DryRun {
		g.safeComment(ctx, owner, repo, issue.Number, g.dryRunBody(comment, "无需重开，仅打标 "+strings.Join(labels, ",")))
		return GovernanceResult{Decision: GovernanceWellFormed, DryRun: true}, nil
	}
	g.safeLabels(ctx, owner, repo, issue.Number, labels)
	g.safeComment(ctx, owner, repo, issue.Number, comment)
	g.Logger.LogTemplate("governance_well_formed", g.Config, map[string]string{"number": itoa(issue.Number)})
	return GovernanceResult{Decision: GovernanceWellFormed, Promoted: true}, nil
}

func (g IssueGovernance) BuildWellFormedComment(ctx context.Context, issue Issue, summary string, related []map[string]any) string {
	review, err := g.DraftWellFormedReview(ctx, issue, related)
	if err != nil {
		g.Logger.LogTemplate("governance_review_failed", g.Config, map[string]string{
			"number": itoa(issue.Number), "error": err.Error(),
		})
	} else if review != "" {
		valid := map[int]struct{}{issue.Number: {}}
		for _, item := range related {
			valid[parseIntAny(item["number"])] = struct{}{}
		}
		fabricated := []string{}
		for _, match := range issueCitationPattern.FindAllString(review, -1) {
			number, _ := parseStrictInt(match[1:])
			if _, ok := valid[number]; !ok {
				fabricated = append(fabricated, match)
			}
		}
		if len(fabricated) > 0 {
			g.Logger.LogTemplate("governance_citation_rejected", g.Config, map[string]string{
				"number": itoa(issue.Number), "cited": strings.Join(fabricated, " "),
			})
		} else {
			text := WithLogLine(withBotPrefix(review), "governance_well_formed_comment", g.Config)
			if len(related) > 0 {
				text += "\n\n" + g.Config.Responses["governance_history_reference_note"]
			}
			return text
		}
	}
	return g.Render("governance_well_formed_comment", map[string]string{"summary": summary}, true)
}

func (g IssueGovernance) RouteNormalize(ctx context.Context, owner, repo string, issue Issue, summary string, points KeyPoints, classification string) (GovernanceResult, error) {
	labels := []string{g.Gov.CanonicalLabel}
	if classification != "" {
		labels = append(labels, classification)
	}
	if g.Gov.DryRun {
		comment := g.Render("governance_new_issue_comment", map[string]string{
			"summary": summary, "canonical": "（编号待实际创建后确定）",
		}, true)
		g.safeComment(ctx, owner, repo, issue.Number, g.dryRunBody(comment,
			"创建规范化 canonical issue（[Feature]/[Bug] 前缀）并打标 "+strings.Join(labels, ",")))
		return GovernanceResult{Decision: GovernanceNewTopic, DryRun: true}, nil
	}
	title, body, err := g.DraftCanonical(ctx, issue, points, classification)
	if err != nil {
		return GovernanceResult{}, err
	}
	created, err := g.Ops.CreateIssue(ctx, owner, repo, title, body, labels)
	if err != nil {
		g.Logger.LogTemplate("governance_create_canonical_failed", g.Config, map[string]string{"error": err.Error()})
		return GovernanceResult{}, err
	}
	if created.Number == 0 {
		return GovernanceResult{}, fmt.Errorf("创建 canonical 后未拿到编号，中止以便重试")
	}
	g.Logger.LogTemplate("governance_canonical_created", g.Config, map[string]string{"number": itoa(created.Number)})
	comment := g.Render("governance_new_issue_comment", map[string]string{
		"summary": summary, "canonical": itoa(created.Number),
	}, true)
	g.safeComment(ctx, owner, repo, issue.Number, comment)
	g.safeClose(ctx, owner, repo, issue.Number)
	g.Logger.LogTemplate("governance_new_topic_log", g.Config, map[string]string{"number": itoa(issue.Number)})
	return GovernanceResult{Decision: GovernanceNewTopic, CanonicalNumber: created.Number, Closed: true}, nil
}

func (g IssueGovernance) Render(key string, values map[string]string, withAction bool) string {
	text := Render(g.Config.Responses[key], values)
	if withAction {
		text += "\n\n" + Render(g.Config.Responses["governance_log_prefix"], map[string]string{"action": key})
	}
	return text
}

func (g IssueGovernance) dryRunBody(comment, action string) string {
	return g.Config.Responses["governance_dry_run"] + "\n\n**本应执行**：" + action + "\n\n---\n\n" + comment
}

func (g IssueGovernance) safeComment(ctx context.Context, owner, repo string, number int, body string) {
	if err := g.Ops.AddComment(ctx, owner, repo, number, body); err != nil {
		g.Logger.LogTemplate("governance_comment_failed", g.Config, map[string]string{"error": err.Error()})
	}
}

func (g IssueGovernance) safeLabels(ctx context.Context, owner, repo string, number int, labels []string) {
	if err := g.Ops.AddLabels(ctx, owner, repo, number, labels); err != nil {
		g.Logger.LogTemplate("label_add_failed", g.Config, map[string]string{"error": err.Error()})
	}
}

func (g IssueGovernance) safeClose(ctx context.Context, owner, repo string, number int) {
	reason := "not_planned"
	if err := g.Ops.UpdateIssueState(ctx, owner, repo, number, "closed", &reason); err != nil {
		g.Logger.LogTemplate("issue_close_failed", g.Config, map[string]string{"error": err.Error()})
	}
}

func (g IssueGovernance) safeRecordMerge(ctx context.Context, owner, repo string, canonicalNumber int, issue Issue, summary string) {
	record := g.Render("governance_merge_canonical_record", map[string]string{
		"source": itoa(issue.Number), "author": issue.User.Login, "summary": summary,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, true)
	if err := g.Ops.AddComment(ctx, owner, repo, canonicalNumber, record); err != nil {
		g.Logger.LogTemplate("governance_record_failed", g.Config, map[string]string{
			"number": itoa(canonicalNumber), "error": err.Error(),
		})
	}
}

func withBotPrefix(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "🤖") {
		return trimmed
	}
	return "🤖 " + trimmed
}

func WithLogLine(text, action string, config Config) string {
	return text + "\n\n" + Render(config.Responses["governance_log_prefix"], map[string]string{"action": action})
}
