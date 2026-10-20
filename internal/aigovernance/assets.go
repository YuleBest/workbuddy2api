package aigovernance

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed assets/config.default.json assets/locales/*.json
var embeddedAssets embed.FS

type AISettings struct {
	AnalyzeFileChanges   bool    `json:"analyze_file_changes"`
	MaxFilesToAnalyze    int     `json:"max_files_to_analyze"`
	MaxPatchLinesPerFile int     `json:"max_patch_lines_per_file"`
	MaxTokens            int     `json:"max_tokens"`
	Temperature          float64 `json:"temperature"`
	APIType              string  `json:"-"`
}

type AnalysisDepth struct {
	MaxFiles int `json:"max_files"`
	MaxLines int `json:"max_lines"`
}

type ConfigDefaults struct {
	AIModel                 string `json:"ai_model"`
	Labels                  string `json:"labels"`
	AnalysisDepth           string `json:"analysis_depth"`
	Language                string `json:"language"`
	AIAPIType               string `json:"ai_api_type"`
	APIBaseURL              string `json:"api_base_url"`
	LockReason              string `json:"lock_reason"`
	CanonicalLabel          string `json:"canonical_label"`
	DuplicateLabel          string `json:"duplicate_label"`
	MaxCanonicalIndex       int    `json:"max_canonical_index"`
	DryRun                  bool   `json:"dry_run"`
	MaintainerExempt        bool   `json:"maintainer_exempt"`
	EnablePRGovernance      bool   `json:"enable_pr_governance"`
	CanonicalBodyTruncate   int    `json:"canonical_body_truncate"`
	PRReviewClose           bool   `json:"pr_review_close"`
	MaxRelatedIssues        int    `json:"max_related_issues"`
	RelatedCommentsPerIssue int    `json:"related_comments_per_issue"`
	RelatedBodyTruncate     int    `json:"related_body_truncate"`
	MaxHistoryIndex         int    `json:"max_history_index"`
	EnableTwoStage          bool   `json:"enable_two_stage"`
	MaxScreenedCandidates   int    `json:"max_screened_candidates"`
	ScreeningModel          string `json:"screening_model"`
}

type Config struct {
	AISettings     AISettings               `json:"ai_settings"`
	AnalysisDepths map[string]AnalysisDepth `json:"analysis_depths"`
	Defaults       ConfigDefaults           `json:"defaults"`
	Logging        map[string]string        `json:"logging"`
	Prompts        map[string]string        `json:"prompts"`
	Responses      map[string]string        `json:"responses"`
	Locale         Locale                   `json:"-"`
	Language       string                   `json:"-"`
}

type Locale struct {
	AnswerLanguage     string            `json:"answer_language"`
	ReadmeAnswerLength string            `json:"readme_answer_length"`
	SmartAnswerLength  string            `json:"smart_answer_length"`
	Responses          map[string]string `json:"responses"`
}

func LoadConfig() (Config, error) {
	data, err := embeddedAssets.ReadFile("assets/config.default.json")
	if err != nil {
		return Config{}, fmt.Errorf("read embedded config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse embedded config: %w", err)
	}
	if err := ValidateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func ValidateConfig(config Config) error {
	if config.AISettings.MaxTokens <= 0 {
		return fmt.Errorf("配置文件中的 max_tokens 必须是正整数")
	}
	if config.AISettings.Temperature < 0 || config.AISettings.Temperature > 2 {
		return fmt.Errorf("配置文件中的 temperature 必须是 0-2 之间的数字")
	}
	if len(config.Prompts) == 0 || len(config.Responses) == 0 || len(config.Logging) == 0 {
		return fmt.Errorf("配置文件缺少必需的段落")
	}
	for _, key := range []string{"spam_detection", "readme_coverage_check", "content_quality_check", "pr_spam_detection"} {
		if strings.TrimSpace(config.Prompts[key]) == "" {
			return fmt.Errorf("配置文件缺少必需的提示词: %s", key)
		}
	}
	return nil
}

func NormalizeLanguage(language string) string {
	if strings.EqualFold(strings.TrimSpace(language), "zh-cn") {
		return "zh-CN"
	}
	return "en"
}

func ApplyLocale(config *Config, requested string) (string, error) {
	language := NormalizeLanguage(requested)
	data, err := embeddedAssets.ReadFile("assets/locales/" + language + ".json")
	if err != nil {
		return "", fmt.Errorf("read locale %s: %w", language, err)
	}
	var locale Locale
	if err := json.Unmarshal(data, &locale); err != nil {
		return "", fmt.Errorf("parse locale %s: %w", language, err)
	}
	if config.Responses == nil {
		config.Responses = map[string]string{}
	}
	for key, value := range locale.Responses {
		config.Responses[key] = value
	}
	config.Locale = locale
	config.Language = language
	config.Defaults.Language = language
	return language, nil
}

func AssetResponseKeys(language string) ([]string, error) {
	data, err := embeddedAssets.ReadFile("assets/locales/" + language + ".json")
	if err != nil {
		return nil, err
	}
	var locale Locale
	if err := json.Unmarshal(data, &locale); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(locale.Responses))
	for key := range locale.Responses {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}
