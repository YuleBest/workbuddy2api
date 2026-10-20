package aigovernance

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type GitHub interface {
	AddComment(ctx context.Context, owner, repo string, number int, body string) error
	AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error
	UpdateIssueState(ctx context.Context, owner, repo string, number int, state string, reason *string) error
	UpdatePullRequest(ctx context.Context, owner, repo string, number int, patch map[string]any) error
	LockIssue(ctx context.Context, owner, repo string, number int, reason string) error
	CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (Issue, error)
	GetReadme(ctx context.Context, owner, repo string) (string, error)
	PinnedIssuesContent(ctx context.Context, owner, repo string) (string, error)
	ListCanonicalIssues(ctx context.Context, owner, repo, label string, maxResults, bodyTruncate int, includeClosed bool) ([]CanonicalItem, error)
	SearchIssuesAndPRs(ctx context.Context, owner, repo string, maxResults int) ([]HistoryIndexItem, error)
	GetIssueDetail(ctx context.Context, owner, repo string, number int) (HistoryDetail, error)
	ListPRFilesSummary(ctx context.Context, owner, repo string, number, maxFiles, maxPatchLines int) (PRFilesSummary, error)
	SearchRelatedClosedIssues(ctx context.Context, owner, repo string, keywords []string, maxResults int) ([]SearchRelatedIssue, error)
	ListIssueComments(ctx context.Context, owner, repo string, number, perPage int) ([]HistoryComment, error)
	ListIssueTimeline(ctx context.Context, owner, repo string, number, perPage int) ([]TimelineEvent, error)
	ListPRCommits(ctx context.Context, owner, repo string, number, perPage int) ([]PRCommit, error)
	CheckCollaborator(ctx context.Context, owner, repo, login string) (bool, error)
}

type GitHubAPIError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e *GitHubAPIError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("GitHub API %s %s returned %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

type GitHubClient struct {
	Token      string
	BaseURL    string
	GraphQLURL string
	Client     *http.Client
	UserAgent  string
}

func NewGitHubClient(token, baseURL, graphqlURL string, client *http.Client) *GitHubClient {
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.github.com"
	}
	if strings.TrimSpace(graphqlURL) == "" {
		graphqlURL = "https://api.github.com/graphql"
	}
	return &GitHubClient{
		Token:      token,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		GraphQLURL: graphqlURL,
		Client:     client,
		UserAgent:  "workbuddy2api-ai-governance",
	}
}

func (c *GitHubClient) AddComment(ctx context.Context, owner, repo string, number int, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(repo), number)
	return c.do(ctx, http.MethodPost, path, map[string]any{"body": body}, nil)
}

func (c *GitHubClient) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", url.PathEscape(owner), url.PathEscape(repo), number)
	return c.do(ctx, http.MethodPost, path, map[string]any{"labels": labels}, nil)
}

func (c *GitHubClient) UpdateIssueState(ctx context.Context, owner, repo string, number int, state string, reason *string) error {
	payload := map[string]any{"state": state}
	if reason != nil && *reason != "" {
		payload["state_reason"] = *reason
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	return c.do(ctx, http.MethodPatch, path, payload, nil)
}

func (c *GitHubClient) UpdatePullRequest(ctx context.Context, owner, repo string, number int, patch map[string]any) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	return c.do(ctx, http.MethodPatch, path, patch, nil)
}

func (c *GitHubClient) LockIssue(ctx context.Context, owner, repo string, number int, reason string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/lock", url.PathEscape(owner), url.PathEscape(repo), number)
	return c.do(ctx, http.MethodPut, path, map[string]any{"lock_reason": reason}, nil)
}

func (c *GitHubClient) CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues", url.PathEscape(owner), url.PathEscape(repo))
	var created Issue
	err := c.do(ctx, http.MethodPost, path, map[string]any{
		"title":  title,
		"body":   body,
		"labels": labels,
	}, &created)
	return created, err
}

func (c *GitHubClient) GetReadme(ctx context.Context, owner, repo string) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s/contents/README.md", url.PathEscape(owner), url.PathEscape(repo))
	var response struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return "", err
	}
	if response.Content == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(response.Content, "\n", ""))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func (c *GitHubClient) PinnedIssuesContent(ctx context.Context, owner, repo string) (string, error) {
	query := `query($owner: String!, $repo: String!) {
  repository(owner: $owner, name: $repo) {
    pinnedIssues(first: 10) {
      nodes {
        issue { title body number url createdAt state }
        pinnedBy { login }
      }
    }
  }
}`
	var response struct {
		Data struct {
			Repository struct {
				PinnedIssues struct {
					Nodes []struct {
						Issue struct {
							Title  string `json:"title"`
							Body   string `json:"body"`
							Number int    `json:"number"`
							State  string `json:"state"`
						} `json:"issue"`
						PinnedBy User `json:"pinnedBy"`
					} `json:"nodes"`
				} `json:"pinnedIssues"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := c.doURL(ctx, http.MethodPost, c.GraphQLURL, map[string]any{
		"query":     query,
		"variables": map[string]string{"owner": owner, "repo": repo},
	}, &response); err != nil {
		return "", err
	}
	nodes := response.Data.Repository.PinnedIssues.Nodes
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		title := node.Issue.Title
		if title == "" {
			title = "(无标题)"
		}
		body := node.Issue.Body
		if body == "" {
			body = "(无描述)"
		}
		state := "开启"
		if node.Issue.State == "CLOSED" {
			state = "已关闭"
		}
		pinner := node.PinnedBy.Login
		if pinner == "" {
			pinner = "未知"
		}
		parts = append(parts, fmt.Sprintf("## %s (#%d) - %s\n置顶者: %s\n\n%s\n\n---", title, node.Issue.Number, state, pinner, body))
	}
	return strings.Join(parts, "\n\n"), nil
}

func (c *GitHubClient) ListCanonicalIssues(ctx context.Context, owner, repo, label string, maxResults, bodyTruncate int, includeClosed bool) ([]CanonicalItem, error) {
	stateFilter := "is:issue"
	if !includeClosed {
		stateFilter += " is:open"
	}
	query := fmt.Sprintf(`repo:%s/%s %s label:"%s"`, owner, repo, stateFilter, label)
	rawItems, err := c.searchIssues(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	items := make([]CanonicalItem, 0, len(rawItems))
	for _, raw := range rawItems {
		if len(items) >= maxResults {
			break
		}
		items = append(items, CanonicalItem{
			Number:      raw.Number,
			Title:       raw.Title,
			Body:        truncatePlain(raw.Body, bodyTruncate),
			State:       raw.State,
			StateReason: raw.StateReason,
			ClosedAt:    raw.ClosedAt,
		})
	}
	return items, nil
}

func (c *GitHubClient) SearchIssuesAndPRs(ctx context.Context, owner, repo string, maxResults int) ([]HistoryIndexItem, error) {
	query := fmt.Sprintf("repo:%s/%s", owner, repo)
	rawItems, err := c.searchIssues(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	items := make([]HistoryIndexItem, 0, len(rawItems))
	for _, raw := range rawItems {
		if len(items) >= maxResults {
			break
		}
		kind := "issue"
		state := raw.State
		if raw.PullRequest != nil {
			kind = "pr"
			if raw.PullRequest.MergedAt != nil {
				state = "merged"
			}
		}
		items = append(items, HistoryIndexItem{
			Number:      raw.Number,
			Kind:        kind,
			Title:       raw.Title,
			Labels:      raw.labelNames(),
			State:       state,
			StateReason: raw.StateReason,
			ClosedAt:    raw.ClosedAt,
			Body:        raw.Body,
		})
	}
	return items, nil
}

func (c *GitHubClient) GetIssueDetail(ctx context.Context, owner, repo string, number int) (HistoryDetail, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	var raw struct {
		Number      int     `json:"number"`
		Title       string  `json:"title"`
		Body        string  `json:"body"`
		State       string  `json:"state"`
		StateReason *string `json:"state_reason"`
		ClosedAt    *string `json:"closed_at"`
		PullRequest *struct {
			MergedAt *string `json:"merged_at"`
		} `json:"pull_request"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return HistoryDetail{}, err
	}
	state := raw.State
	if raw.PullRequest != nil && raw.PullRequest.MergedAt != nil {
		state = "merged"
	}
	return HistoryDetail{
		Number:      raw.Number,
		Title:       raw.Title,
		Body:        raw.Body,
		State:       state,
		StateReason: raw.StateReason,
		ClosedAt:    raw.ClosedAt,
	}, nil
}

func (c *GitHubClient) ListPRFilesSummary(ctx context.Context, owner, repo string, number, maxFiles, maxPatchLines int) (PRFilesSummary, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files", url.PathEscape(owner), url.PathEscape(repo), number)
	var files []struct {
		Filename  string `json:"filename"`
		Status    string `json:"status"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Patch     string `json:"patch"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &files); err != nil {
		return PRFilesSummary{}, err
	}
	if len(files) == 0 {
		return PRFilesSummary{}, nil
	}
	if maxFiles <= 0 {
		maxFiles = 5
	}
	if maxPatchLines <= 0 {
		maxPatchLines = 5
	}
	if len(files) < maxFiles {
		maxFiles = len(files)
	}
	parts := make([]string, 0, maxFiles)
	for _, file := range files[:maxFiles] {
		part := fmt.Sprintf("%s(%s,+%d/-%d)", file.Filename, file.Status, file.Additions, file.Deletions)
		if file.Patch != "" {
			lines := strings.Split(file.Patch, "\n")
			diff := make([]string, 0, len(lines))
			for _, line := range lines {
				if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
					diff = append(diff, line)
				}
			}
			shown := diff
			if len(shown) > maxPatchLines {
				shown = shown[:maxPatchLines]
			}
			if strings.TrimSpace(strings.Join(shown, "\n")) != "" {
				part += "\n" + strings.Join(shown, "\n")
				if len(diff) > maxPatchLines {
					part += "\n..."
				}
			}
		}
		parts = append(parts, part)
	}
	return PRFilesSummary{Summary: strings.Join(parts, "\n---\n"), Total: len(files)}, nil
}

func (c *GitHubClient) SearchRelatedClosedIssues(ctx context.Context, owner, repo string, keywords []string, maxResults int) ([]SearchRelatedIssue, error) {
	sanitized := make([]string, 0, 4)
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(strings.NewReplacer(`"`, "", `'`, "").Replace(keyword))
		if len([]rune(keyword)) > 1 {
			sanitized = append(sanitized, keyword)
		}
		if len(sanitized) == 4 {
			break
		}
	}
	if len(sanitized) == 0 {
		return nil, nil
	}
	terms := make([]string, 0, len(sanitized))
	for _, keyword := range sanitized {
		terms = append(terms, `"`+keyword+`"`)
	}
	query := fmt.Sprintf("repo:%s/%s is:issue is:closed %s", owner, repo, strings.Join(terms, " "))
	rawItems, err := c.searchIssues(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}
	items := make([]SearchRelatedIssue, 0, len(rawItems))
	for _, raw := range rawItems {
		if len(items) >= maxResults {
			break
		}
		items = append(items, SearchRelatedIssue{
			Number:      raw.Number,
			Title:       raw.Title,
			State:       raw.State,
			StateReason: raw.StateReason,
			ClosedAt:    raw.ClosedAt,
			IsPR:        raw.PullRequest != nil,
		})
	}
	return items, nil
}

func (c *GitHubClient) ListIssueComments(ctx context.Context, owner, repo string, number, perPage int) ([]HistoryComment, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=%d&page=1", url.PathEscape(owner), url.PathEscape(repo), number, perPage)
	var raw []struct {
		User      User   `json:"user"`
		CreatedAt string `json:"created_at"`
		Body      string `json:"body"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	comments := make([]HistoryComment, 0, len(raw))
	for _, item := range raw {
		author := item.User.Login
		if author == "" {
			author = "unknown"
		}
		comments = append(comments, HistoryComment{Author: author, Body: item.Body, CreatedAt: item.CreatedAt})
	}
	return comments, nil
}

func (c *GitHubClient) ListIssueTimeline(ctx context.Context, owner, repo string, number, perPage int) ([]TimelineEvent, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/events?per_page=%d&page=1", url.PathEscape(owner), url.PathEscape(repo), number, perPage)
	var raw []struct {
		Event     string  `json:"event"`
		Actor     User    `json:"actor"`
		CommitID  *string `json:"commit_id"`
		CreatedAt string  `json:"created_at"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	events := make([]TimelineEvent, 0, len(raw))
	for _, item := range raw {
		actor := item.Actor.Login
		if actor == "" {
			actor = "unknown"
		}
		events = append(events, TimelineEvent{
			Event: item.Event, Actor: actor, CommitID: item.CommitID, CreatedAt: item.CreatedAt,
		})
	}
	return events, nil
}

func (c *GitHubClient) ListPRCommits(ctx context.Context, owner, repo string, number, perPage int) ([]PRCommit, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/commits?per_page=%d&page=1", url.PathEscape(owner), url.PathEscape(repo), number, perPage)
	var raw []struct {
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	commits := make([]PRCommit, 0, len(raw))
	for _, item := range raw {
		author := item.Commit.Author.Name
		if author == "" {
			author = "unknown"
		}
		commits = append(commits, PRCommit{Message: item.Commit.Message, Author: author})
	}
	return commits, nil
}

func (c *GitHubClient) CheckCollaborator(ctx context.Context, owner, repo, login string) (bool, error) {
	path := fmt.Sprintf("/repos/%s/%s/collaborators/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(login))
	request, err := c.newRequest(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return false, err
	}
	response, err := c.Client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusOK {
		io.Copy(io.Discard, response.Body)
		return true, nil
	}
	if response.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, response.Body)
		return false, nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 200))
	return false, &GitHubAPIError{Method: http.MethodGet, URL: request.URL.String(), StatusCode: response.StatusCode, Body: string(body)}
}

func (c *GitHubClient) do(ctx context.Context, method, path string, payload any, out any) error {
	return c.doURL(ctx, method, c.BaseURL+path, payload, out)
}

func (c *GitHubClient) doURL(ctx context.Context, method, endpoint string, payload any, out any) error {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := c.newRequest(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	response, err := c.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body := strings.TrimSpace(string(raw))
		if len(body) > 200 {
			body = body[:200]
		}
		return &GitHubAPIError{Method: method, URL: endpoint, StatusCode: response.StatusCode, Body: body}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode GitHub response: %w", err)
		}
	}
	return nil
}

func (c *GitHubClient) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

type rawSearchItem struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	State       string  `json:"state"`
	StateReason *string `json:"state_reason"`
	ClosedAt    *string `json:"closed_at"`
	Labels      []Label `json:"labels"`
	PullRequest *struct {
		MergedAt *string `json:"merged_at"`
	} `json:"pull_request"`
}

func (r rawSearchItem) labelNames() []string {
	names := make([]string, 0, len(r.Labels))
	for _, label := range r.Labels {
		names = append(names, label.Name)
	}
	return names
}

func (c *GitHubClient) searchIssues(ctx context.Context, query string, maxResults int) ([]rawSearchItem, error) {
	if maxResults <= 0 {
		return nil, nil
	}
	perPage := maxResults
	if perPage > 100 {
		perPage = 100
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("per_page", strconv.Itoa(perPage))
	values.Set("sort", "updated")
	values.Set("order", "desc")
	path := "/search/issues?" + values.Encode()
	var response struct {
		Items []rawSearchItem `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}
