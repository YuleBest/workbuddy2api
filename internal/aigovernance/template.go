package aigovernance

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

type TemplateInfo struct {
	HasTemplate  bool
	TemplateType string
	Confidence   float64
	Indicators   []string
}

type ExtractedSection struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	LineStart int    `json:"lineStart"`
	LineEnd   int    `json:"lineEnd"`
}

type ContentInfo struct {
	UserContent       string             `json:"userContent"`
	IsEmpty           bool               `json:"isEmpty"`
	ExtractedSections []ExtractedSection `json:"extractedSections"`
	TotalSections     int                `json:"totalSections"`
	ValidSections     int                `json:"validSections"`
	TemplateInfo      TemplateInfo       `json:"-"`
}

type QualityInfo struct {
	Score   int      `json:"score"`
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
}

type QualityAnalysis struct {
	TemplateInfo TemplateInfo `json:"templateInfo"`
	ContentInfo  ContentInfo  `json:"contentInfo"`
	Quality      QualityInfo  `json:"quality"`
}

var titlePrefixes = []string{"[BUG]", "[FEATURE]", "[Feature Request]", "[Bug Report]", "[ENHANCEMENT]", "[QUESTION]"}

var templateIndicators = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^### .*`),
	regexp.MustCompile(`(?m)^## .*`),
	regexp.MustCompile(`\*\*.+\*\*`),
	regexp.MustCompile(`<!--.+-->`),
	regexp.MustCompile(`(?m)^\s*-\s*\[[\sx]\]`),
	regexp.MustCompile(`(?m)^>\s*.+`),
	regexp.MustCompile(`\|\s*.+\s*\|`),
}

var commonTemplateFields = []string{
	"description", "expected behavior", "actual behavior", "steps to reproduce",
	"environment", "version", "browser", "additional context", "screenshots",
	"描述", "预期行为", "实际行为", "复现步骤", "环境信息", "版本", "浏览器",
	"附加信息", "截图", "bug描述", "功能描述", "如何实现", "自查", "确认",
}

var emptyContentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^_No response_$`),
	regexp.MustCompile(`(?i)^N/A$`),
	regexp.MustCompile(`(?i)^None$`),
	regexp.MustCompile(`^无$`),
	regexp.MustCompile(`^没有$`),
	regexp.MustCompile(`^暂无$`),
	regexp.MustCompile(`^\s*$`),
	regexp.MustCompile(`^\.+$`),
	regexp.MustCompile(`^-+$`),
	regexp.MustCompile(`^#+$`),
}

func DetectTemplate(issueTitle, issueBody string) TemplateInfo {
	info := TemplateInfo{Indicators: []string{}}
	if strings.TrimSpace(issueBody) == "" {
		return info
	}
	indicatorCount := 0
	totalLines := len(strings.Split(issueBody, "\n"))
	for _, prefix := range titlePrefixes {
		if strings.Contains(issueTitle, prefix) {
			info.Indicators = append(info.Indicators, "Title prefix: "+prefix)
			indicatorCount += 2
			break
		}
	}
	for _, pattern := range templateIndicators {
		matches := pattern.FindAllStringIndex(issueBody, -1)
		if len(matches) > 0 {
			info.Indicators = append(info.Indicators, fmt.Sprintf("Template pattern: %s (%dx)", pattern.String(), len(matches)))
			indicatorCount += len(matches)
		}
	}
	fieldCount := 0
	lowerBody := strings.ToLower(issueBody)
	for _, field := range commonTemplateFields {
		if strings.Contains(lowerBody, strings.ToLower(field)) {
			fieldCount++
		}
	}
	if fieldCount >= 2 {
		info.Indicators = append(info.Indicators, fmt.Sprintf("Found %d template fields", fieldCount))
		indicatorCount += fieldCount
	}
	minLines := totalLines
	if minLines < 3 {
		minLines = 3
	}
	maxExpected := float64(minLines) / 3
	if maxExpected < 2 {
		maxExpected = 2
	}
	density := float64(indicatorCount) / maxExpected
	if density > 1.5 {
		density = 1.5
	}
	lengthFactor := float64(totalLines) / 10
	if lengthFactor > 1 {
		lengthFactor = 1
	}
	info.Confidence = density * lengthFactor * 100
	if info.Confidence > 85 {
		info.Confidence = 85
	}
	info.HasTemplate = info.Confidence > 25
	if info.HasTemplate {
		titleLower := strings.ToLower(issueTitle)
		switch {
		case strings.Contains(titleLower, "bug") || strings.Contains(lowerBody, "bug"):
			info.TemplateType = "bug_report"
		case strings.Contains(titleLower, "feature") || strings.Contains(lowerBody, "feature"):
			info.TemplateType = "feature_request"
		default:
			info.TemplateType = "generic"
		}
	}
	return info
}

func ExtractUserContent(issueBody string, templateInfo TemplateInfo) ContentInfo {
	if strings.TrimSpace(issueBody) == "" || !templateInfo.HasTemplate {
		return ContentInfo{
			UserContent:       issueBody,
			IsEmpty:           strings.TrimSpace(issueBody) == "",
			ExtractedSections: []ExtractedSection{},
		}
	}
	lines := strings.Split(issueBody, "\n")
	sections := []ExtractedSection{}
	currentTitle := ""
	currentLines := []string{}
	currentStart := 0
	flush := func(end int) {
		if currentTitle == "" || len(currentLines) == 0 {
			return
		}
		content := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if content != "" && !IsEmptyContent(content) {
			sections = append(sections, ExtractedSection{
				Title: currentTitle, Content: content, LineStart: currentStart, LineEnd: end,
			})
		}
	}
	for index, line := range lines {
		header := false
		for _, pattern := range templateIndicators {
			if pattern.MatchString(line) {
				header = true
				break
			}
		}
		if header {
			flush(index - 1)
			currentTitle = strings.TrimSpace(strings.NewReplacer("#", "", "*", "", ">", "").Replace(line))
			currentLines = nil
			currentStart = index + 1
			continue
		}
		if currentTitle != "" {
			currentLines = append(currentLines, line)
		}
	}
	flush(len(lines) - 1)

	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		if section.Content != "" && !IsEmptyContent(section.Content) {
			parts = append(parts, section.Title+": "+section.Content)
		}
	}
	return ContentInfo{
		UserContent:       strings.Join(parts, "\n\n"),
		IsEmpty:           len(parts) == 0,
		ExtractedSections: sections,
		TotalSections:     len(sections),
		ValidSections:     len(parts),
	}
}

func IsEmptyContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	for _, pattern := range emptyContentPatterns {
		if pattern.MatchString(trimmed) {
			return true
		}
	}
	clean := strings.NewReplacer("-", "", "_", "", "*", "", "#", "", ">", "", " ", "", "\t", "", "\n", "", "|", "").Replace(trimmed)
	clean = regexp.MustCompile(`\[[\sx]\]`).ReplaceAllString(clean, "")
	return utf8.RuneCountInString(clean) < 3
}

func GenerateAnalysisReport(templateInfo TemplateInfo, contentInfo ContentInfo) string {
	report := []string{
		"Template Analysis:",
		fmt.Sprintf("- Has template: %s", map[bool]string{true: "Yes", false: "No"}[templateInfo.HasTemplate]),
	}
	if templateInfo.HasTemplate {
		report = append(report,
			"- Type: "+templateInfo.TemplateType,
			fmt.Sprintf("- Confidence: %.1f%%", templateInfo.Confidence),
			"- Indicators: "+strings.Join(templateInfo.Indicators, ", "),
			fmt.Sprintf("- Valid sections: %d/%d", contentInfo.ValidSections, contentInfo.TotalSections),
			fmt.Sprintf("- Content: %s", map[bool]string{true: "Empty", false: "Has content"}[contentInfo.IsEmpty]),
		)
	}
	return strings.Join(report, "\n")
}

func AnalyzeIssueQuality(issueTitle, issueBody string) QualityAnalysis {
	templateInfo := DetectTemplate(issueTitle, issueBody)
	contentInfo := ExtractUserContent(issueBody, templateInfo)
	contentInfo.TemplateInfo = templateInfo
	quality := QualityInfo{Score: 50, Level: "unknown"}
	if utf8.RuneCountInString(strings.TrimSpace(issueTitle)) > 10 {
		quality.Score += 15
		quality.Reasons = append(quality.Reasons, "Good title length")
	} else {
		quality.Score -= 10
		quality.Reasons = append(quality.Reasons, "Title too short")
	}
	if templateInfo.HasTemplate {
		if !contentInfo.IsEmpty && contentInfo.ValidSections >= 2 {
			quality.Score += 25
			quality.Reasons = append(quality.Reasons, "Template used and well filled")
		} else if !contentInfo.IsEmpty {
			quality.Score += 10
			quality.Reasons = append(quality.Reasons, "Template used but incomplete")
		} else {
			quality.Score -= 20
			quality.Reasons = append(quality.Reasons, "Template used but empty")
		}
	} else {
		length := utf8.RuneCountInString(strings.TrimSpace(issueBody))
		switch {
		case length > 50:
			quality.Score += 20
			quality.Reasons = append(quality.Reasons, "Good free-form description")
		case length > 10:
			quality.Score += 5
			quality.Reasons = append(quality.Reasons, "Brief free-form description")
		default:
			quality.Score -= 25
			quality.Reasons = append(quality.Reasons, "Too short or empty content")
		}
	}
	if quality.Score < 0 {
		quality.Score = 0
	}
	if quality.Score > 100 {
		quality.Score = 100
	}
	switch {
	case quality.Score >= 70:
		quality.Level = "high"
	case quality.Score >= 40:
		quality.Level = "medium"
	default:
		quality.Level = "low"
	}
	return QualityAnalysis{TemplateInfo: templateInfo, ContentInfo: contentInfo, Quality: quality}
}
