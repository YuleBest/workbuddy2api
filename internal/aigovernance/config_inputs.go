package aigovernance

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type govInputSpec struct {
	Input string
	Key   string
	Type  string
}

var governanceInputSpecs = []govInputSpec{
	{Input: "canonical-label", Key: "canonical_label", Type: "string"},
	{Input: "duplicate-label", Key: "duplicate_label", Type: "string"},
	{Input: "dry-run", Key: "dry_run", Type: "bool"},
	{Input: "maintainer-exempt", Key: "maintainer_exempt", Type: "bool"},
	{Input: "enable-pr-governance", Key: "enable_pr_governance", Type: "bool"},
	{Input: "max-canonical-index", Key: "max_canonical_index", Type: "int"},
	{Input: "canonical-body-truncate", Key: "canonical_body_truncate", Type: "int"},
	{Input: "pr-review-close", Key: "pr_review_close", Type: "bool"},
	{Input: "max-related-issues", Key: "max_related_issues", Type: "int"},
	{Input: "related-comments-per-issue", Key: "related_comments_per_issue", Type: "int"},
	{Input: "related-body-truncate", Key: "related_body_truncate", Type: "int"},
	{Input: "max-history-index", Key: "max_history_index", Type: "int"},
	{Input: "enable-two-stage", Key: "enable_two_stage", Type: "bool"},
	{Input: "max-screened-candidates", Key: "max_screened_candidates", Type: "int"},
	{Input: "screening-model", Key: "screening_model", Type: "string"},
}

func InputEnvName(input string) string {
	return "INPUT_" + strings.ToUpper(strings.ReplaceAll(input, "-", "_"))
}

func ParseInputs(config *Config, getenv func(string) string) (Inputs, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	input := func(name string) string {
		return strings.TrimSpace(getenv(InputEnvName(name)))
	}
	first := func(values ...string) string {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
		return ""
	}

	requestedLanguage := first(input("language"), config.Defaults.Language)
	language, err := ApplyLocale(config, requestedLanguage)
	if err != nil {
		return Inputs{}, err
	}
	warnings := []string{}
	if !strings.EqualFold(strings.TrimSpace(requestedLanguage), "en") &&
		!strings.EqualFold(strings.TrimSpace(requestedLanguage), "zh-cn") {
		warnings = append(warnings, fmt.Sprintf("Unsupported language %q; falling back to English.", requestedLanguage))
	}

	apiType := strings.ToLower(first(input("ai-api-type"), config.Defaults.AIAPIType))
	if apiType != "chat-completions" && apiType != "responses" {
		return Inputs{}, fmt.Errorf("Unsupported AI API type: %s", apiType)
	}
	config.AISettings.APIType = apiType

	analysisDepth := strings.ToLower(first(input("max-analysis-depth"), config.Defaults.AnalysisDepth))
	depth, ok := config.AnalysisDepths[analysisDepth]
	if !ok {
		depth = config.AnalysisDepths["normal"]
	}
	config.AISettings.MaxFilesToAnalyze = depth.MaxFiles
	config.AISettings.MaxPatchLinesPerFile = depth.MaxLines

	rawAnalyze := first(input("analyze-file-changes"))
	if rawAnalyze != "" {
		config.AISettings.AnalyzeFileChanges = strings.EqualFold(rawAnalyze, "true")
	}

	gov := DefaultGovernanceOptions()
	for _, spec := range governanceInputSpecs {
		raw := first(input(spec.Input))
		switch spec.Type {
		case "bool":
			value := boolDefault(config.Defaults, spec.Key)
			if raw != "" {
				value = strings.EqualFold(raw, "true")
			}
			setGovBool(&gov, spec.Key, value)
		case "int":
			fallback := intDefault(config.Defaults, spec.Key)
			value := fallback
			if raw != "" {
				if parsed, parseErr := strconv.Atoi(raw); parseErr == nil {
					value = parsed
				}
			}
			setGovInt(&gov, spec.Key, value)
		case "string":
			value := stringDefault(config.Defaults, spec.Key)
			if raw != "" {
				value = raw
			}
			setGovString(&gov, spec.Key, value)
		}
	}
	gov.SkipUsers = mergeSkipUsers(gov.SkipUsers, splitList(first(input("skip-users"))))
	gov.GovernanceToken = first(input("governance-token"))

	labels := splitList(first(input("labels"), config.Defaults.Labels))
	blacklist := lowerList(splitList(first(input("blacklist"))))

	return Inputs{
		Token:                first(input("github-token"), getenv("GITHUB_TOKEN")),
		AIModel:              first(input("ai-model"), config.Defaults.AIModel),
		Labels:               labels,
		BlacklistUsers:       blacklist,
		Language:             language,
		AnalyzeFileChanges:   config.AISettings.AnalyzeFileChanges,
		AnalysisDepth:        analysisDepth,
		MaxFilesToAnalyze:    config.AISettings.MaxFilesToAnalyze,
		MaxPatchLinesPerFile: config.AISettings.MaxPatchLinesPerFile,
		CustomBaseURL:        first(input("ai-base-url")),
		CustomAPIKey:         first(input("ai-api-key")),
		AIAPIType:            apiType,
		GovernanceToken:      first(input("governance-token")),
		Governance:           gov,
		Warnings:             warnings,
	}, nil
}

func boolDefault(defaults ConfigDefaults, key string) bool {
	switch key {
	case "dry_run":
		return defaults.DryRun
	case "maintainer_exempt":
		return defaults.MaintainerExempt
	case "enable_pr_governance":
		return defaults.EnablePRGovernance
	case "pr_review_close":
		return defaults.PRReviewClose
	case "enable_two_stage":
		return defaults.EnableTwoStage
	default:
		return false
	}
}

func intDefault(defaults ConfigDefaults, key string) int {
	switch key {
	case "max_canonical_index":
		return defaults.MaxCanonicalIndex
	case "canonical_body_truncate":
		return defaults.CanonicalBodyTruncate
	case "max_related_issues":
		return defaults.MaxRelatedIssues
	case "related_comments_per_issue":
		return defaults.RelatedCommentsPerIssue
	case "related_body_truncate":
		return defaults.RelatedBodyTruncate
	case "max_history_index":
		return defaults.MaxHistoryIndex
	case "max_screened_candidates":
		return defaults.MaxScreenedCandidates
	default:
		return 0
	}
}

func stringDefault(defaults ConfigDefaults, key string) string {
	switch key {
	case "canonical_label":
		return defaults.CanonicalLabel
	case "duplicate_label":
		return defaults.DuplicateLabel
	case "screening_model":
		return defaults.ScreeningModel
	default:
		return ""
	}
}

func setGovBool(gov *GovernanceOptions, key string, value bool) {
	switch key {
	case "dry_run":
		gov.DryRun = value
	case "maintainer_exempt":
		gov.MaintainerExempt = value
	case "enable_pr_governance":
		gov.EnablePRGovernance = value
	case "pr_review_close":
		gov.PRReviewClose = value
	case "enable_two_stage":
		gov.EnableTwoStage = value
	}
}

func setGovInt(gov *GovernanceOptions, key string, value int) {
	switch key {
	case "max_canonical_index":
		gov.MaxCanonicalIndex = value
	case "canonical_body_truncate":
		gov.CanonicalBodyTruncate = value
	case "max_related_issues":
		gov.MaxRelatedIssues = value
	case "related_comments_per_issue":
		gov.RelatedCommentsPerIssue = value
	case "related_body_truncate":
		gov.RelatedBodyTruncate = value
	case "max_history_index":
		gov.MaxHistoryIndex = value
	case "max_screened_candidates":
		gov.MaxScreenedCandidates = value
	}
}

func setGovString(gov *GovernanceOptions, key, value string) {
	switch key {
	case "canonical_label":
		gov.CanonicalLabel = value
	case "duplicate_label":
		gov.DuplicateLabel = value
	case "screening_model":
		gov.ScreeningModel = value
	}
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func lowerList(value []string) []string {
	out := make([]string, 0, len(value))
	for _, item := range value {
		out = append(out, strings.ToLower(strings.TrimSpace(item)))
	}
	return out
}

func mergeSkipUsers(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, group := range append([][]string{DefaultSkipUsers}, groups...) {
		for _, item := range lowerList(group) {
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}

var conventionalCommit = regexp.MustCompile(`(?i)^(feat|fix|docs|style|refactor|perf|test|chore|ci|build|revert)(\(.+\))?: .+$`)

func IsValidCommitTitle(title string) bool {
	return conventionalCommit.MatchString(strings.TrimSpace(title))
}
