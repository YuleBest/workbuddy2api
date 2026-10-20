package aigovernance

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestConfigAndLocaleKeysStayAligned(t *testing.T) {
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	baseKeys := sortedKeys(config.Responses)
	for _, language := range []string{"en", "zh-CN"} {
		keys, err := AssetResponseKeys(language)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(keys, "\x00") != strings.Join(baseKeys, "\x00") {
			t.Fatalf("%s response keys drifted:\nconfig=%v\nlocale=%v", language, baseKeys, keys)
		}
	}
}

func TestActionInputsAndDefaultsStayAligned(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "actions", "ai-governance", "action.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	expected := map[string]string{
		"github-token":               "${{ github.token }}",
		"governance-token":           "",
		"ai-model":                   "openai/gpt-4o",
		"ai-base-url":                "",
		"ai-api-key":                 "",
		"ai-api-type":                "chat-completions",
		"labels":                     "bug,enhancement,question,documentation",
		"language":                   "zh-CN",
		"analyze-file-changes":       "true",
		"max-analysis-depth":         "normal",
		"blacklist":                  "",
		"canonical-label":            "canonical",
		"duplicate-label":            "duplicate",
		"skip-users":                 "",
		"dry-run":                    "true",
		"maintainer-exempt":          "true",
		"enable-pr-governance":       "false",
		"max-canonical-index":        "50",
		"canonical-body-truncate":    "1500",
		"pr-review-close":            "false",
		"max-related-issues":         "3",
		"related-comments-per-issue": "10",
		"related-body-truncate":      "1500",
		"max-history-index":          "100",
		"enable-two-stage":           "false",
		"max-screened-candidates":    "5",
		"screening-model":            "",
	}
	for name, defaultValue := range expected {
		blockPattern := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `:\n(?:    .*\n)*?    default: '(.*)'$`)
		match := blockPattern.FindStringSubmatch(text)
		if len(match) != 2 {
			t.Fatalf("action input %s is missing or malformed", name)
		}
		if match[1] != defaultValue {
			t.Fatalf("action input %s default = %q, want %q", name, match[1], defaultValue)
		}
	}
	for _, spec := range governanceInputSpecs {
		if !regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(spec.Input) + `:`).MatchString(text) {
			t.Fatalf("governance input %s missing from action.yml", spec.Input)
		}
		if env := InputEnvName(spec.Input); !strings.Contains(text, env+":") {
			t.Fatalf("action.yml missing env %s", env)
		}
	}
	for _, forbidden := range []string{"setup-node", "npm ci", "node src/index.js", "jq -r"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("action.yml still contains %q", forbidden)
		}
	}
	if !strings.Contains(text, "go build -o \"$RUNNER_TEMP/ai-governance\" ./cmd/ai-governance") {
		t.Fatal("action.yml does not build the Go binary")
	}
}

func TestParseInputsDefaultsAndInvalidNumbers(t *testing.T) {
	config := testConfig(t, "en")
	env := map[string]string{
		"INPUT_GITHUB_TOKEN":            "token",
		"INPUT_MAX_RELATED_ISSUES":      "not-a-number",
		"INPUT_MAX_CANONICAL_INDEX":     "x50",
		"INPUT_CANONICAL_BODY_TRUNCATE": "1500px",
		"INPUT_ENABLE_TWO_STAGE":        "true",
		"INPUT_SCREENING_MODEL":         "cheap-model",
		"INPUT_SKIP_USERS":              "GitHub-Actions[bot], extra",
	}
	getenv := func(key string) string { return env[key] }
	inputs, err := ParseInputs(&config, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if inputs.Governance.MaxRelatedIssues != config.Defaults.MaxRelatedIssues {
		t.Fatalf("max related = %d", inputs.Governance.MaxRelatedIssues)
	}
	if inputs.Governance.MaxCanonicalIndex != config.Defaults.MaxCanonicalIndex {
		t.Fatalf("max canonical = %d", inputs.Governance.MaxCanonicalIndex)
	}
	if inputs.Governance.CanonicalBodyTruncate != config.Defaults.CanonicalBodyTruncate {
		t.Fatalf("body truncate = %d", inputs.Governance.CanonicalBodyTruncate)
	}
	if !inputs.Governance.EnableTwoStage || inputs.Governance.ScreeningModel != "cheap-model" {
		t.Fatalf("two-stage inputs not parsed: %+v", inputs.Governance)
	}
	if !containsFold(inputs.Governance.SkipUsers, "github-actions[bot]") || !containsFold(inputs.Governance.SkipUsers, "extra") {
		t.Fatalf("skip users not merged: %v", inputs.Governance.SkipUsers)
	}
}

func TestUnknownLanguageFallsBackToEnglish(t *testing.T) {
	config := testConfig(t, "fr")
	if config.Language != "en" {
		t.Fatalf("language = %q", config.Language)
	}
	if !strings.Contains(config.Responses["issue_spam"], "This issue") {
		t.Fatalf("unexpected English response: %s", config.Responses["issue_spam"])
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
