package aigovernance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubClientSearchContractsAndWrites(t *testing.T) {
	var sawComment, sawLabel, sawClose bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/issues":
			if r.URL.Query().Get("q") == "" {
				t.Fatal("search q is empty")
			}
			io.WriteString(w, `{"items":[{"number":57,"title":"cache","body":"body","state":"closed","state_reason":"not_planned","closed_at":"2026-01-02T00:00:00Z","labels":[{"name":"canonical"}]}]}`)
		case r.URL.Path == "/repos/o/r/issues/22/comments":
			sawComment = true
			io.WriteString(w, `{}`)
		case r.URL.Path == "/repos/o/r/issues/22/labels":
			sawLabel = true
			io.WriteString(w, `[]`)
		case r.URL.Path == "/repos/o/r/issues/22" && r.Method == http.MethodPatch:
			sawClose = true
			io.WriteString(w, `{}`)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	client := NewGitHubClient("token", server.URL, server.URL+"/graphql", server.Client())
	canonical, err := client.ListCanonicalIssues(context.Background(), "o", "r", "canonical", 50, 1500, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 1 || canonical[0].State != "closed" || canonical[0].StateReason == nil {
		t.Fatalf("unexpected canonical: %+v", canonical)
	}
	hits, err := client.SearchRelatedClosedIssues(context.Background(), "o", "r", []string{"cache"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Number != 57 {
		t.Fatalf("unexpected hits: %+v", hits)
	}
	if err := client.AddComment(context.Background(), "o", "r", 22, "comment"); err != nil {
		t.Fatal(err)
	}
	if err := client.AddLabels(context.Background(), "o", "r", 22, []string{"bug"}); err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateIssueState(context.Background(), "o", "r", 22, "closed", stringPointer("not_planned")); err != nil {
		t.Fatal(err)
	}
	if !sawComment || !sawLabel || !sawClose {
		t.Fatalf("missing write calls: comment=%v label=%v close=%v", sawComment, sawLabel, sawClose)
	}
}

func TestGitHubClientReadmeAndPRFileSummary(t *testing.T) {
	readme := base64.StdEncoding.EncodeToString([]byte("# hello"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/contents/README.md":
			json.NewEncoder(w).Encode(map[string]any{"content": readme})
		case "/repos/o/r/pulls/42/files":
			io.WriteString(w, `[{"filename":"a.go","status":"modified","additions":2,"deletions":1,"patch":"@@ -a\n-old\n+new\n+two"}]`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewGitHubClient("token", server.URL, server.URL+"/graphql", server.Client())
	content, err := client.GetReadme(context.Background(), "o", "r")
	if err != nil {
		t.Fatal(err)
	}
	if content != "# hello" {
		t.Fatalf("readme = %q", content)
	}
	summary, err := client.ListPRFilesSummary(context.Background(), "o", "r", 42, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 1 || summary.Summary == "" {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestParseLocalIssueEventPayload(t *testing.T) {
	payload := []byte(`{
	  "action":"opened",
	  "issue":{"number":22,"title":"feat: request","body":"body","user":{"login":"someone"}},
	  "repository":{"full_name":"owner/repo"}
	}`)
	event, err := ParseEvent("issues", payload, "fallback/repo")
	if err != nil {
		t.Fatal(err)
	}
	if event.Owner != "owner" || event.Repo != "repo" || event.Issue == nil || event.Issue.Number != 22 {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestParseLocalPullRequestEventPayload(t *testing.T) {
	payload := []byte(`{
	  "action":"opened",
	  "pull_request":{"number":42,"title":"feat: cache","body":"body","user":{"login":"someone"}},
	  "repository":{"full_name":"owner/repo"}
	}`)
	event, err := ParseEvent("pull_request_target", payload, "")
	if err != nil {
		t.Fatal(err)
	}
	if event.PullRequest == nil || event.PullRequest.Number != 42 {
		t.Fatalf("unexpected event: %+v", event)
	}
}
