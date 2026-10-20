package aigovernance

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var placeholderPattern = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

func Render(template string, values map[string]string) string {
	return placeholderPattern.ReplaceAllStringFunc(template, func(token string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(token, "{"), "}")
		if value, ok := values[key]; ok {
			return value
		}
		return token
	})
}

func truncateMarked(value string, limit int) string {
	if limit < 0 {
		limit = 0
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…(截断)"
}

func truncatePlain(value string, limit int) string {
	if limit < 0 {
		limit = 0
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func stringPointer(value string) *string {
	return &value
}

func containsFold(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}

func canonicalLabels(labels []Label) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		out = append(out, label.Name)
	}
	return out
}

func formatSummary(points KeyPoints) string {
	key := points.Key
	if strings.TrimSpace(key) == "" {
		key = "(未提炼出明确要点)"
	}
	lines := []string{fmt.Sprintf("要点：%s", key)}
	if len(points.Todos) > 0 {
		lines = append(lines, "要做的事：")
		for _, item := range points.Todos {
			lines = append(lines, "- "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func parseStrictInt(value string) (int, bool) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	return parsed, err == nil
}
