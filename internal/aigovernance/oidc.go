package aigovernance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultClaudeExchangeURL = "https://api.anthropic.com/api/github/github-app-token-exchange"

type TokenResolver struct {
	Client      *http.Client
	ExchangeURL string
}

func (r TokenResolver) Resolve(ctx context.Context, explicitToken string, getenv func(string) string, logger Logger) string {
	if strings.TrimSpace(explicitToken) != "" {
		return strings.TrimSpace(explicitToken)
	}
	if getenv == nil {
		return ""
	}
	requestURL := strings.TrimSpace(getenv("ACTIONS_ID_TOKEN_REQUEST_URL"))
	requestToken := strings.TrimSpace(getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN"))
	if requestURL == "" || requestToken == "" {
		logger.Warning("OIDC token unavailable — governance falls back to github-actions[bot]")
		return ""
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	exchangeURL := strings.TrimSpace(r.ExchangeURL)
	if exchangeURL == "" {
		exchangeURL = strings.TrimSpace(getenv("CLAUDE_TOKEN_EXCHANGE_URL"))
	}
	if exchangeURL == "" {
		exchangeURL = defaultClaudeExchangeURL
	}

	separator := "?"
	if strings.Contains(requestURL, "?") {
		separator = "&"
	}
	oidcURL := requestURL + separator + "audience=claude-code-github-action"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, oidcURL, nil)
	if err != nil {
		logger.Warning("OIDC token request failed: %v; governance falls back to github-actions[bot]", err)
		return ""
	}
	request.Header.Set("Authorization", "bearer "+requestToken)
	response, err := client.Do(request)
	if err != nil {
		logger.Warning("OIDC token request failed: %v; governance falls back to github-actions[bot]", err)
		return ""
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		logger.Warning("OIDC token request returned %d; governance falls back to github-actions[bot]", response.StatusCode)
		return ""
	}
	var oidc struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &oidc); err != nil || strings.TrimSpace(oidc.Value) == "" || oidc.Value == "null" {
		logger.Warning("OIDC token unavailable — governance falls back to github-actions[bot]")
		return ""
	}

	body, _ := json.Marshal(map[string]any{
		"permissions": map[string]string{
			"contents":      "write",
			"pull_requests": "write",
			"issues":        "write",
		},
	})
	exchangeRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, bytes.NewReader(body))
	if err != nil {
		logger.Warning("Claude App token exchange failed: %v; governance falls back to github-actions[bot]", err)
		return ""
	}
	exchangeRequest.Header.Set("Authorization", "Bearer "+oidc.Value)
	exchangeRequest.Header.Set("Content-Type", "application/json")
	exchangeResponse, err := client.Do(exchangeRequest)
	if err != nil {
		logger.Warning("Claude App token exchange failed: %v; governance falls back to github-actions[bot]", err)
		return ""
	}
	defer exchangeResponse.Body.Close()
	raw, _ = io.ReadAll(io.LimitReader(exchangeResponse.Body, 1<<20))
	if exchangeResponse.StatusCode < 200 || exchangeResponse.StatusCode >= 300 {
		logger.Warning("Claude App token exchange returned %d; governance falls back to github-actions[bot]", exchangeResponse.StatusCode)
		return ""
	}
	var token struct {
		Token    string `json:"token"`
		AppToken string `json:"app_token"`
	}
	if err := json.Unmarshal(raw, &token); err != nil {
		logger.Warning("Claude App token exchange response was invalid: %v; governance falls back to github-actions[bot]", err)
		return ""
	}
	result := strings.TrimSpace(token.Token)
	if result == "" {
		result = strings.TrimSpace(token.AppToken)
	}
	if result == "" {
		logger.Warning("Claude App token exchange returned no token — governance falls back to github-actions[bot]")
		return ""
	}
	logger.Info("Claude App token obtained — governance will act as claude[bot]")
	return result
}

func audienceURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse OIDC request URL: %w", err)
	}
	query := parsed.Query()
	query.Set("audience", "claude-code-github-action")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
