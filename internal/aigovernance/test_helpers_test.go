package aigovernance

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

type fakeAI struct {
	CallFunc       func(context.Context, string, AIRequest, Config, AICallOptions) (string, error)
	StructuredFunc func(context.Context, string, AIRequest, Config, AICallOptions, map[string]any) (map[string]any, error)
	Calls          []AIRequest
	Models         []string
	Options        []AICallOptions
}

func (f *fakeAI) Call(ctx context.Context, model string, request AIRequest, config Config, options AICallOptions) (string, error) {
	f.Calls = append(f.Calls, request)
	f.Models = append(f.Models, model)
	f.Options = append(f.Options, options)
	if f.CallFunc != nil {
		return f.CallFunc(ctx, model, request, config, options)
	}
	return "", fmt.Errorf("unexpected AI call: %s", options.Purpose)
}

func (f *fakeAI) Structured(ctx context.Context, model string, request AIRequest, config Config, options AICallOptions, defaults map[string]any) (map[string]any, error) {
	f.Calls = append(f.Calls, request)
	f.Models = append(f.Models, model)
	f.Options = append(f.Options, options)
	if f.StructuredFunc != nil {
		return f.StructuredFunc(ctx, model, request, config, options, defaults)
	}
	return nil, fmt.Errorf("unexpected structured AI call: %s", options.Purpose)
}

type fakeGitHub struct {
	AddCommentFunc                func(context.Context, string, string, int, string) error
	AddLabelsFunc                 func(context.Context, string, string, int, []string) error
	UpdateIssueStateFunc          func(context.Context, string, string, int, string, *string) error
	UpdatePullRequestFunc         func(context.Context, string, string, int, map[string]any) error
	LockIssueFunc                 func(context.Context, string, string, int, string) error
	CreateIssueFunc               func(context.Context, string, string, string, string, []string) (Issue, error)
	GetReadmeFunc                 func(context.Context, string, string) (string, error)
	PinnedIssuesContentFunc       func(context.Context, string, string) (string, error)
	ListCanonicalIssuesFunc       func(context.Context, string, string, string, int, int, bool) ([]CanonicalItem, error)
	SearchIssuesAndPRsFunc        func(context.Context, string, string, int) ([]HistoryIndexItem, error)
	GetIssueDetailFunc            func(context.Context, string, string, int) (HistoryDetail, error)
	ListPRFilesSummaryFunc        func(context.Context, string, string, int, int, int) (PRFilesSummary, error)
	SearchRelatedClosedIssuesFunc func(context.Context, string, string, []string, int) ([]SearchRelatedIssue, error)
	ListIssueCommentsFunc         func(context.Context, string, string, int, int) ([]HistoryComment, error)
	ListIssueTimelineFunc         func(context.Context, string, string, int, int) ([]TimelineEvent, error)
	ListPRCommitsFunc             func(context.Context, string, string, int, int) ([]PRCommit, error)
	CheckCollaboratorFunc         func(context.Context, string, string, string) (bool, error)
	Comments                      []string
	Labels                        [][]string
	IssueStates                   []string
	PRPatches                     []map[string]any
	Locks                         []string
	Created                       []Issue
	Events                        []string
}

func (f *fakeGitHub) AddComment(ctx context.Context, owner, repo string, number int, body string) error {
	f.Events = append(f.Events, fmt.Sprintf("comment:%d", number))
	f.Comments = append(f.Comments, body)
	if f.AddCommentFunc != nil {
		return f.AddCommentFunc(ctx, owner, repo, number, body)
	}
	return nil
}

func (f *fakeGitHub) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	f.Events = append(f.Events, fmt.Sprintf("labels:%d", number))
	f.Labels = append(f.Labels, append([]string(nil), labels...))
	if f.AddLabelsFunc != nil {
		return f.AddLabelsFunc(ctx, owner, repo, number, labels)
	}
	return nil
}

func (f *fakeGitHub) UpdateIssueState(ctx context.Context, owner, repo string, number int, state string, reason *string) error {
	f.Events = append(f.Events, fmt.Sprintf("issue-state:%d:%s", number, state))
	f.IssueStates = append(f.IssueStates, state)
	if f.UpdateIssueStateFunc != nil {
		return f.UpdateIssueStateFunc(ctx, owner, repo, number, state, reason)
	}
	return nil
}

func (f *fakeGitHub) UpdatePullRequest(ctx context.Context, owner, repo string, number int, patch map[string]any) error {
	f.Events = append(f.Events, fmt.Sprintf("pr-update:%d", number))
	f.PRPatches = append(f.PRPatches, patch)
	if f.UpdatePullRequestFunc != nil {
		return f.UpdatePullRequestFunc(ctx, owner, repo, number, patch)
	}
	return nil
}

func (f *fakeGitHub) LockIssue(ctx context.Context, owner, repo string, number int, reason string) error {
	f.Events = append(f.Events, fmt.Sprintf("lock:%d", number))
	f.Locks = append(f.Locks, reason)
	if f.LockIssueFunc != nil {
		return f.LockIssueFunc(ctx, owner, repo, number, reason)
	}
	return nil
}

func (f *fakeGitHub) CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (Issue, error) {
	f.Events = append(f.Events, "create-issue")
	created := Issue{Number: 99, Title: title, Body: body}
	f.Created = append(f.Created, created)
	if f.CreateIssueFunc != nil {
		return f.CreateIssueFunc(ctx, owner, repo, title, body, labels)
	}
	return created, nil
}

func (f *fakeGitHub) GetReadme(ctx context.Context, owner, repo string) (string, error) {
	if f.GetReadmeFunc != nil {
		return f.GetReadmeFunc(ctx, owner, repo)
	}
	return "", nil
}

func (f *fakeGitHub) PinnedIssuesContent(ctx context.Context, owner, repo string) (string, error) {
	if f.PinnedIssuesContentFunc != nil {
		return f.PinnedIssuesContentFunc(ctx, owner, repo)
	}
	return "", nil
}

func (f *fakeGitHub) ListCanonicalIssues(ctx context.Context, owner, repo, label string, maxResults, bodyTruncate int, includeClosed bool) ([]CanonicalItem, error) {
	if f.ListCanonicalIssuesFunc != nil {
		return f.ListCanonicalIssuesFunc(ctx, owner, repo, label, maxResults, bodyTruncate, includeClosed)
	}
	return nil, nil
}

func (f *fakeGitHub) SearchIssuesAndPRs(ctx context.Context, owner, repo string, maxResults int) ([]HistoryIndexItem, error) {
	if f.SearchIssuesAndPRsFunc != nil {
		return f.SearchIssuesAndPRsFunc(ctx, owner, repo, maxResults)
	}
	return nil, nil
}

func (f *fakeGitHub) GetIssueDetail(ctx context.Context, owner, repo string, number int) (HistoryDetail, error) {
	if f.GetIssueDetailFunc != nil {
		return f.GetIssueDetailFunc(ctx, owner, repo, number)
	}
	return HistoryDetail{Number: number, Kind: "issue", Title: fmt.Sprintf("issue %d", number), Body: "body", State: "closed"}, nil
}

func (f *fakeGitHub) ListPRFilesSummary(ctx context.Context, owner, repo string, number, maxFiles, maxPatchLines int) (PRFilesSummary, error) {
	if f.ListPRFilesSummaryFunc != nil {
		return f.ListPRFilesSummaryFunc(ctx, owner, repo, number, maxFiles, maxPatchLines)
	}
	return PRFilesSummary{}, nil
}

func (f *fakeGitHub) SearchRelatedClosedIssues(ctx context.Context, owner, repo string, keywords []string, maxResults int) ([]SearchRelatedIssue, error) {
	if f.SearchRelatedClosedIssuesFunc != nil {
		return f.SearchRelatedClosedIssuesFunc(ctx, owner, repo, keywords, maxResults)
	}
	return nil, nil
}

func (f *fakeGitHub) ListIssueComments(ctx context.Context, owner, repo string, number, perPage int) ([]HistoryComment, error) {
	if f.ListIssueCommentsFunc != nil {
		return f.ListIssueCommentsFunc(ctx, owner, repo, number, perPage)
	}
	return nil, nil
}

func (f *fakeGitHub) ListIssueTimeline(ctx context.Context, owner, repo string, number, perPage int) ([]TimelineEvent, error) {
	if f.ListIssueTimelineFunc != nil {
		return f.ListIssueTimelineFunc(ctx, owner, repo, number, perPage)
	}
	return nil, nil
}

func (f *fakeGitHub) ListPRCommits(ctx context.Context, owner, repo string, number, perPage int) ([]PRCommit, error) {
	if f.ListPRCommitsFunc != nil {
		return f.ListPRCommitsFunc(ctx, owner, repo, number, perPage)
	}
	return nil, nil
}

func (f *fakeGitHub) CheckCollaborator(ctx context.Context, owner, repo, login string) (bool, error) {
	if f.CheckCollaboratorFunc != nil {
		return f.CheckCollaboratorFunc(ctx, owner, repo, login)
	}
	return false, nil
}

func testConfig(t *testing.T, language string) Config {
	t.Helper()
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyLocale(&config, language); err != nil {
		t.Fatal(err)
	}
	config.AISettings.APIType = "chat-completions"
	return config
}

func testLogger() (Logger, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return Logger{Out: stdout, Err: stderr}, stdout, stderr
}
