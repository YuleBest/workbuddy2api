package aigovernance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	DecisionSpam          = "SPAM"
	DecisionReadmeCovered = "README_COVERED"
	DecisionBasic         = "BASIC"
	DecisionUnclear       = "UNCLEAR"
	DecisionKeep          = "KEEP"
	DecisionInvalidCommit = "INVALID_COMMIT"
	DecisionMalicious     = "MALICIOUS"
	DecisionTrivial       = "TRIVIAL"

	GovernanceDuplicate  = "DUPLICATE"
	GovernanceNewTopic   = "NEW_TOPIC"
	GovernanceUncertain  = "UNCERTAIN"
	GovernanceWellFormed = "WELL_FORMED"

	ReviewClose     = "CLOSE"
	ReviewKeep      = "KEEP"
	ReviewUncertain = "UNCERTAIN"

	PRLinkAnchor = "<!-- ai-governance:linked -->"
)

var DefaultSkipUsers = []string{"github-actions[bot]", "github-actions"}

type User struct {
	Login string `json:"login"`
}

type Label struct {
	Name string `json:"name"`
}

type Issue struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	State       string  `json:"state"`
	StateReason *string `json:"state_reason"`
	ClosedAt    *string `json:"closed_at"`
	User        User    `json:"user"`
	Labels      []Label `json:"labels"`
	PullRequest *struct {
		MergedAt *string `json:"merged_at"`
	} `json:"pull_request"`
}

type PullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	User   User   `json:"user"`
	State  string `json:"state"`
}

type Event struct {
	Action      string       `json:"action"`
	Issue       *Issue       `json:"issue"`
	PullRequest *PullRequest `json:"pull_request"`
	Repository  struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type EventContext struct {
	Name        string
	Action      string
	Owner       string
	Repo        string
	Issue       *Issue
	PullRequest *PullRequest
}

func ParseEvent(name string, payload []byte, fallbackRepo string) (EventContext, error) {
	var event Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return EventContext{}, fmt.Errorf("parse GitHub event: %w", err)
	}
	repoFull := strings.TrimSpace(event.Repository.FullName)
	if repoFull == "" {
		repoFull = strings.TrimSpace(fallbackRepo)
	}
	parts := strings.SplitN(repoFull, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return EventContext{}, fmt.Errorf("invalid GITHUB_REPOSITORY %q", repoFull)
	}
	return EventContext{
		Name:        name,
		Action:      event.Action,
		Owner:       parts[0],
		Repo:        parts[1],
		Issue:       event.Issue,
		PullRequest: event.PullRequest,
	}, nil
}

type CanonicalItem struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	State       string  `json:"state"`
	StateReason *string `json:"state_reason"`
	ClosedAt    *string `json:"closed_at"`
}

type HistoryIndexItem struct {
	Number      int      `json:"number"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Labels      []string `json:"labels"`
	State       string   `json:"state"`
	StateReason *string  `json:"state_reason"`
	ClosedAt    *string  `json:"closed_at"`
	Body        string   `json:"body,omitempty"`
}

type HistoryComment struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type TimelineEvent struct {
	Event     string  `json:"event"`
	Actor     string  `json:"actor"`
	CommitID  *string `json:"commit_id"`
	CreatedAt string  `json:"created_at,omitempty"`
}

type PRCommit struct {
	Message string `json:"message"`
	Author  string `json:"author"`
}

type PRFilesSummary struct {
	Summary string `json:"summary"`
	Total   int    `json:"total"`
}

type HistoryDetail struct {
	Number       int              `json:"number"`
	Kind         string           `json:"kind"`
	Title        string           `json:"title"`
	State        string           `json:"state"`
	StateReason  *string          `json:"state_reason"`
	ClosedAt     *string          `json:"closed_at"`
	Body         string           `json:"body"`
	Comments     []HistoryComment `json:"comments"`
	Timeline     []TimelineEvent  `json:"timeline"`
	FilesChanged string           `json:"files_changed,omitempty"`
	Commits      []PRCommit       `json:"commits,omitempty"`
}

type SearchRelatedIssue struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	State       string  `json:"state"`
	StateReason *string `json:"state_reason"`
	ClosedAt    *string `json:"closed_at"`
	IsPR        bool    `json:"is_pr"`
}

type KeyPoints struct {
	Key     string   `json:"要点"`
	Todos   []string `json:"要做的事"`
	Summary string   `json:"-"`
}

type GovernanceOptions struct {
	CanonicalLabel          string
	DuplicateLabel          string
	SkipUsers               []string
	DryRun                  bool
	MaintainerExempt        bool
	EnablePRGovernance      bool
	MaxCanonicalIndex       int
	CanonicalBodyTruncate   int
	PRReviewClose           bool
	MaxRelatedIssues        int
	RelatedCommentsPerIssue int
	RelatedBodyTruncate     int
	MaxHistoryIndex         int
	EnableTwoStage          bool
	MaxScreenedCandidates   int
	ScreeningModel          string
	GovernanceToken         string
	WellFormedMinSections   int
	WellFormedMinTitleLen   int
	WellFormedMinBodyLen    int
	HistoryRejectedLabel    string
}

func DefaultGovernanceOptions() GovernanceOptions {
	return GovernanceOptions{
		CanonicalLabel:          "canonical",
		DuplicateLabel:          "duplicate",
		DryRun:                  true,
		MaintainerExempt:        true,
		MaxCanonicalIndex:       50,
		CanonicalBodyTruncate:   1500,
		MaxRelatedIssues:        3,
		RelatedCommentsPerIssue: 10,
		RelatedBodyTruncate:     1500,
		MaxHistoryIndex:         100,
		MaxScreenedCandidates:   5,
		WellFormedMinSections:   3,
		WellFormedMinTitleLen:   8,
		WellFormedMinBodyLen:    80,
		HistoryRejectedLabel:    "history-rejected",
	}
}

type Inputs struct {
	Token                string
	AIModel              string
	Labels               []string
	BlacklistUsers       []string
	Language             string
	AnalyzeFileChanges   bool
	AnalysisDepth        string
	MaxFilesToAnalyze    int
	MaxPatchLinesPerFile int
	CustomBaseURL        string
	CustomAPIKey         string
	AIAPIType            string
	GovernanceToken      string
	Governance           GovernanceOptions
	Warnings             []string
}

type AIRequest struct {
	Instructions string
	Input        string
}

type AICallOptions struct {
	Purpose   string
	Normalize bool
	MaxTokens int
}

type AI interface {
	Call(ctx context.Context, model string, request AIRequest, config Config, options AICallOptions) (string, error)
	Structured(ctx context.Context, model string, request AIRequest, config Config, options AICallOptions, defaults map[string]any) (map[string]any, error)
}
