package aigovernance

import (
	"context"
	"fmt"
	"strings"
)

type HistoryContext struct {
	GitHub GitHub
	Config Config
	Gov    GovernanceOptions
	Logger Logger
}

func (h HistoryContext) BuildIndex(ctx context.Context, owner, repo string) []HistoryIndexItem {
	candidates := map[string]HistoryIndexItem{}
	order := []string{}
	searchItems, err := h.GitHub.SearchIssuesAndPRs(ctx, owner, repo, h.Gov.MaxHistoryIndex)
	if err != nil {
		h.Logger.LogTemplate("history_index_search_failed", h.Config, map[string]string{"error": err.Error()})
	}
	for _, item := range searchItems {
		item.Body = truncatePlain(item.Body, h.Gov.RelatedBodyTruncate)
		key := item.Kind + ":" + itoa(item.Number)
		if _, exists := candidates[key]; !exists {
			order = append(order, key)
		}
		candidates[key] = item
	}
	canonicalItems, err := h.GitHub.ListCanonicalIssues(
		ctx, owner, repo, h.Gov.CanonicalLabel, h.Gov.MaxHistoryIndex, h.Gov.RelatedBodyTruncate, true,
	)
	if err != nil {
		h.Logger.LogTemplate("history_index_canonical_failed", h.Config, map[string]string{"error": err.Error()})
	}
	for _, item := range canonicalItems {
		state := item.State
		if state == "" {
			state = "closed"
		}
		key := "issue:" + itoa(item.Number)
		if _, exists := candidates[key]; !exists {
			order = append(order, key)
		}
		candidates[key] = HistoryIndexItem{
			Number:      item.Number,
			Kind:        "issue",
			Title:       item.Title,
			Labels:      []string{"canonical"},
			State:       state,
			StateReason: item.StateReason,
			ClosedAt:    item.ClosedAt,
			Body:        truncatePlain(item.Body, h.Gov.RelatedBodyTruncate),
		}
	}
	items := make([]HistoryIndexItem, 0, len(candidates))
	for _, key := range order {
		items = append(items, candidates[key])
		if len(items) >= h.Gov.MaxHistoryIndex {
			break
		}
	}
	return items
}

func (h HistoryContext) Enrich(ctx context.Context, owner, repo string, refs []HistoryIndexItem) []HistoryDetail {
	details := []HistoryDetail{}
	for _, ref := range refs {
		detail, err := h.enrichOne(ctx, owner, repo, ref.Number, ref.Kind)
		if err != nil {
			h.Logger.LogTemplate("history_enrich_failed", h.Config, map[string]string{
				"number": itoa(ref.Number), "error": err.Error(),
			})
			continue
		}
		details = append(details, detail)
	}
	return details
}

func (h HistoryContext) enrichOne(ctx context.Context, owner, repo string, number int, kind string) (HistoryDetail, error) {
	detail, err := h.GitHub.GetIssueDetail(ctx, owner, repo, number)
	if err != nil {
		return HistoryDetail{}, err
	}
	comments, err := h.GitHub.ListIssueComments(ctx, owner, repo, number, h.Gov.RelatedCommentsPerIssue)
	if err != nil {
		return HistoryDetail{}, err
	}
	timeline, err := h.GitHub.ListIssueTimeline(ctx, owner, repo, number, 30)
	if err != nil {
		return HistoryDetail{}, err
	}
	detail.Kind = "issue"
	if kind == "pr" {
		detail.Kind = "pr"
	}
	detail.Body = truncateMarked(detail.Body, h.Gov.RelatedBodyTruncate)
	detail.Comments = make([]HistoryComment, 0, len(comments))
	for _, comment := range comments {
		comment.Body = truncateMarked(comment.Body, 500)
		detail.Comments = append(detail.Comments, comment)
	}
	detail.Timeline = filterTimeline(timeline)

	if detail.Kind == "pr" {
		files, err := h.GitHub.ListPRFilesSummary(
			ctx, owner, repo, number,
			h.Config.AISettings.MaxFilesToAnalyze,
			h.Config.AISettings.MaxPatchLinesPerFile,
		)
		if err != nil {
			return HistoryDetail{}, err
		}
		commits, err := h.GitHub.ListPRCommits(ctx, owner, repo, number, 20)
		if err != nil {
			return HistoryDetail{}, err
		}
		detail.FilesChanged = truncateMarked(files.Summary, h.Gov.RelatedBodyTruncate)
		detail.Commits = make([]PRCommit, 0, len(commits))
		for _, commit := range commits {
			commit.Message = truncateMarked(commit.Message, 200)
			detail.Commits = append(detail.Commits, commit)
		}
	}
	return detail, nil
}

var allowedTimelineEvents = map[string]struct{}{
	"closed": {}, "reopened": {}, "merged": {}, "referenced": {},
	"cross-referenced": {}, "locked": {}, "unlocked": {},
}

func filterTimeline(events []TimelineEvent) []TimelineEvent {
	out := []TimelineEvent{}
	for _, event := range events {
		if _, ok := allowedTimelineEvents[event.Event]; !ok {
			continue
		}
		out = append(out, TimelineEvent{Event: event.Event, Actor: event.Actor, CommitID: event.CommitID})
	}
	return out
}

func historyConclusion(item HistoryIndexItem) string {
	if item.StateReason != nil && *item.StateReason != "" {
		return fmt.Sprintf("%s (%s)", item.State, *item.StateReason)
	}
	return item.State
}

func historyReference(item HistoryIndexItem) string {
	return fmt.Sprintf("#%d %s — %s", item.Number, item.Title, historyConclusion(item))
}

func compactBody(detail HistoryDetail, limit int) string {
	parts := []string{detail.Body}
	for _, comment := range detail.Comments {
		parts = append(parts, strings.TrimSpace(comment.Author+": "+comment.Body))
	}
	return truncatePlain(strings.Join(parts, "\n\n---\n\n"), limit)
}
