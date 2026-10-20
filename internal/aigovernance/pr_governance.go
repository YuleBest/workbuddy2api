package aigovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var excessNewlines = regexp.MustCompile(`\n{3,}`)

type PRGovernance struct {
	AI       AI
	Model    string
	Config   Config
	Gov      GovernanceOptions
	Ops      GitHub
	Logger   Logger
	IssueGov IssueGovernance
}

func NewPRGovernance(ai AI, model string, config Config, gov GovernanceOptions, ops GitHub, logger Logger) PRGovernance {
	return PRGovernance{
		AI: ai, Model: model, Config: config, Gov: gov, Ops: ops, Logger: logger,
		IssueGov: IssueGovernance{AI: ai, Model: model, Config: config, Gov: gov, Ops: ops, Logger: logger},
	}
}

func (p PRGovernance) Govern(ctx context.Context, owner, repo string, pr PullRequest, classification string, shared *SharedContext) (GovernanceResult, error) {
	p.Logger.LogTemplate("governance_pr_start", p.Config, map[string]string{"number": itoa(pr.Number)})
	if p.Gov.DryRun {
		p.Logger.LogTemplate("governance_dry_run", p.Config, nil)
	}
	asIssue := Issue{Number: pr.Number, Title: pr.Title, Body: pr.Body, User: pr.User}
	points, err := p.IssueGov.ExtractKeyPoints(ctx, asIssue)
	if err != nil {
		return GovernanceResult{}, err
	}
	canonical, err := p.IssueGov.ObtainCanonicalCorpus(ctx, owner, repo, shared, asIssue)
	if err != nil {
		return GovernanceResult{}, err
	}
	match := GovernanceResult{Decision: GovernanceNewTopic}
	if len(canonical) > 0 {
		match, err = p.IssueGov.MatchCanonical(ctx, asIssue, canonical)
		if err != nil {
			return GovernanceResult{}, err
		}
	}
	label := match.Decision
	if match.CanonicalNumber != 0 {
		label += fmt.Sprintf("(#%d)", match.CanonicalNumber)
	}
	p.Logger.LogTemplate("governance_pr_match_result", p.Config, map[string]string{"result": label})
	switch match.Decision {
	case GovernanceDuplicate:
		return p.RouteLinkExisting(ctx, owner, repo, pr, points, match.CanonicalNumber, classification)
	case GovernanceUncertain:
		return p.RouteUncertain(ctx, owner, repo, pr, points.Summary)
	default:
		return p.RouteNewTopic(ctx, owner, repo, pr, points, classification)
	}
}

func (p PRGovernance) RouteUncertain(ctx context.Context, owner, repo string, pr PullRequest, summary string) (GovernanceResult, error) {
	if p.Gov.DryRun {
		p.postDryRun(ctx, owner, repo, pr, summary, 0, nil)
		return GovernanceResult{Decision: GovernanceUncertain, DryRun: true}, nil
	}
	comment := p.render("governance_pr_uncertain_comment", map[string]string{"summary": summary})
	p.safeComment(ctx, owner, repo, pr.Number, comment)
	p.Logger.LogTemplate("governance_pr_uncertain_log", p.Config, map[string]string{"number": itoa(pr.Number)})
	return GovernanceResult{Decision: GovernanceUncertain, Passed: true}, nil
}

func (p PRGovernance) RouteLinkExisting(ctx context.Context, owner, repo string, pr PullRequest, points KeyPoints, canonicalNumber int, classification string) (GovernanceResult, error) {
	linkLine := p.render("governance_pr_match_link_line", map[string]string{"canonical": itoa(canonicalNumber)})
	comment := p.render("governance_pr_link_comment", map[string]string{
		"summary": points.Summary, "link_line": linkLine,
	})
	if p.Gov.DryRun {
		p.postDryRun(ctx, owner, repo, pr, points.Summary, canonicalNumber, nil)
		return GovernanceResult{Decision: GovernanceDuplicate, CanonicalNumber: canonicalNumber, DryRun: true}, nil
	}
	p.safeComment(ctx, owner, repo, pr.Number, comment)
	title, err := p.ResolveTitle(ctx, pr, points)
	if err != nil {
		return GovernanceResult{}, err
	}
	body := p.AppendLinkBlock(pr.Number, pr.Body, points.Summary, p.renderRelation(canonicalNumber, false))
	p.ApplyPRUpdates(ctx, owner, repo, pr, title, body)
	p.safeRecordLink(ctx, owner, repo, canonicalNumber, pr, points.Summary)
	p.Logger.LogTemplate("governance_pr_linked", p.Config, map[string]string{
		"number": itoa(pr.Number), "canonical": itoa(canonicalNumber),
	})
	return GovernanceResult{Decision: GovernanceDuplicate, CanonicalNumber: canonicalNumber, Linked: true}, nil
}

func (p PRGovernance) RouteNewTopic(ctx context.Context, owner, repo string, pr PullRequest, points KeyPoints, classification string) (GovernanceResult, error) {
	labels := []string{p.Gov.CanonicalLabel}
	if classification != "" {
		labels = append(labels, classification)
	}
	if p.Gov.DryRun {
		p.postDryRun(ctx, owner, repo, pr, points.Summary, 0, labels)
		return GovernanceResult{Decision: GovernanceNewTopic, DryRun: true}, nil
	}
	asIssue := Issue{Number: pr.Number, Title: pr.Title, Body: pr.Body, User: pr.User}
	title, body, err := p.IssueGov.DraftCanonical(ctx, asIssue, points, classification)
	if err != nil {
		return GovernanceResult{}, err
	}
	created, err := p.Ops.CreateIssue(ctx, owner, repo, title, body, labels)
	if err != nil {
		p.Logger.LogTemplate("governance_create_canonical_failed", p.Config, map[string]string{"error": err.Error()})
		return GovernanceResult{}, err
	}
	if created.Number == 0 {
		return GovernanceResult{}, fmt.Errorf("创建 canonical 后未拿到编号，中止以便重试")
	}
	p.Logger.LogTemplate("governance_canonical_created", p.Config, map[string]string{"number": itoa(created.Number)})
	linkLine := p.render("governance_pr_new_link_line", map[string]string{"canonical": itoa(created.Number)})
	comment := p.render("governance_pr_link_comment", map[string]string{
		"summary": points.Summary, "link_line": linkLine,
	})
	p.safeComment(ctx, owner, repo, pr.Number, comment)
	newTitle, err := p.ResolveTitle(ctx, pr, points)
	if err != nil {
		return GovernanceResult{}, err
	}
	newBody := p.AppendLinkBlock(pr.Number, pr.Body, points.Summary, p.renderRelation(created.Number, true))
	p.ApplyPRUpdates(ctx, owner, repo, pr, newTitle, newBody)
	p.safeRecordLink(ctx, owner, repo, created.Number, pr, points.Summary)
	p.Logger.LogTemplate("governance_pr_new_topic", p.Config, map[string]string{"number": itoa(pr.Number)})
	return GovernanceResult{Decision: GovernanceNewTopic, CanonicalNumber: created.Number, Created: true}, nil
}

func (p PRGovernance) ResolveTitle(ctx context.Context, pr PullRequest, points KeyPoints) (string, error) {
	original := strings.TrimSpace(pr.Title)
	if IsValidCommitTitle(original) {
		p.Logger.LogTemplate("governance_pr_title_ok", p.Config, map[string]string{"number": itoa(pr.Number)})
		return original, nil
	}
	input, _ := json.Marshal(map[string]any{
		"title": original, "body": strings.TrimSpace(pr.Body), "key_points": points,
	})
	newTitle, err := p.AI.Call(ctx, p.Model, AIRequest{
		Instructions: p.Config.Prompts["governance_pr_title"],
		Input:        string(input),
	}, p.Config, AICallOptions{Purpose: "PR 标题规范化"})
	if err != nil {
		p.Logger.LogTemplate("governance_pr_rewrite_failed", p.Config, map[string]string{
			"number": itoa(pr.Number), "error": err.Error(),
		})
		return original, nil
	}
	newTitle = strings.TrimSpace(newTitle)
	if newTitle != "" && newTitle != original {
		p.Logger.LogTemplate("governance_pr_title_rewrite", p.Config, map[string]string{
			"number": itoa(pr.Number), "title": newTitle,
		})
		return newTitle, nil
	}
	return original, nil
}

func (p PRGovernance) renderRelation(canonical int, closes bool) string {
	keyword := "Related to"
	if closes {
		keyword = "Closes"
	}
	return fmt.Sprintf("### 关联 canonical issue\n\n%s #%d\n", keyword, canonical)
}

func (p PRGovernance) AppendLinkBlock(prNumber int, body, summary, relation string) string {
	existing := body
	if strings.Contains(existing, PRLinkAnchor) {
		p.Logger.LogTemplate("governance_pr_body_linked_skip", p.Config, map[string]string{"number": itoa(prNumber)})
		return ""
	}
	keyText := "(未提炼出明确要点)"
	if summary != "" {
		keyText = strings.ReplaceAll(summary, "\n", "\n> ")
	}
	block := strings.Join([]string{
		PRLinkAnchor,
		"",
		"> 本段由 Claude Code 自动维护，请勿手工删除上面这行锚点注释。",
		"",
		"- **要点**：",
		"> " + keyText,
		"- **关联时间**：" + time.Now().UTC().Format(time.RFC3339),
		"",
		strings.TrimSpace(relation),
	}, "\n")
	result := block + "\n\n---\n\n" + existing
	return excessNewlines.ReplaceAllString(result, "\n\n")
}

func (p PRGovernance) ApplyPRUpdates(ctx context.Context, owner, repo string, pr PullRequest, title, body string) {
	if p.Gov.DryRun {
		return
	}
	if title != "" && title != strings.TrimSpace(pr.Title) {
		if err := p.Ops.UpdatePullRequest(ctx, owner, repo, pr.Number, map[string]any{"title": title}); err != nil {
			p.Logger.LogTemplate("governance_pr_update_failed", p.Config, map[string]string{
				"number": itoa(pr.Number), "error": err.Error(),
			})
		}
	}
	if body != "" && body != pr.Body {
		if err := p.Ops.UpdatePullRequest(ctx, owner, repo, pr.Number, map[string]any{"body": body}); err != nil {
			p.Logger.LogTemplate("governance_pr_update_failed", p.Config, map[string]string{
				"number": itoa(pr.Number), "error": err.Error(),
			})
		}
	}
}

func (p PRGovernance) safeRecordLink(ctx context.Context, owner, repo string, canonical int, pr PullRequest, summary string) {
	if p.Gov.DryRun {
		return
	}
	record := p.render("governance_pr_canonical_record", map[string]string{
		"source": itoa(pr.Number), "author": pr.User.Login, "summary": summary,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	if err := p.Ops.AddComment(ctx, owner, repo, canonical, record); err != nil {
		p.Logger.LogTemplate("governance_record_failed", p.Config, map[string]string{
			"number": itoa(canonical), "error": err.Error(),
		})
	}
}

func (p PRGovernance) render(key string, values map[string]string) string {
	return Render(p.Config.Responses[key], values)
}

func (p PRGovernance) safeComment(ctx context.Context, owner, repo string, number int, body string) {
	if err := p.Ops.AddComment(ctx, owner, repo, number, body); err != nil {
		p.Logger.LogTemplate("governance_comment_failed", p.Config, map[string]string{"error": err.Error()})
	}
}

func (p PRGovernance) postDryRun(ctx context.Context, owner, repo string, pr PullRequest, summary string, canonical int, labels []string) {
	action := ""
	if canonical != 0 {
		action = fmt.Sprintf("关联 canonical #%d；规范化标题；正文顶部追加关联块", canonical)
	} else {
		action = fmt.Sprintf("新建 canonical issue（并打标签 %s）；规范化标题；正文顶部追加关联块", strings.Join(labels, ","))
	}
	body := p.Config.Responses["governance_dry_run"] + "\n\n**本应执行**：" + action
	p.safeComment(ctx, owner, repo, pr.Number, body)
}
