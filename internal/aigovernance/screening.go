package aigovernance

import (
	"context"
	"encoding/json"
	"math"
	"strings"
)

type ScreenedCandidate struct {
	Number    int    `json:"number"`
	Kind      string `json:"kind"`
	Relevance string `json:"relevance"`
}

type ScreeningService struct {
	AI     AI
	Model  string
	Config Config
	Gov    GovernanceOptions
	Logger Logger
}

func (s ScreeningService) Screen(ctx context.Context, subject HistoryIndexItem, index []HistoryIndexItem) []ScreenedCandidate {
	if len(index) == 0 {
		return nil
	}
	compact := make([]map[string]any, 0, len(index))
	for _, item := range index {
		compact = append(compact, map[string]any{
			"number":       item.Number,
			"kind":         item.Kind,
			"title":        item.Title,
			"labels":       item.Labels,
			"state":        item.State,
			"state_reason": item.StateReason,
		})
	}
	payload := map[string]any{
		"subject": map[string]any{
			"kind": subject.Kind, "number": subject.Number, "title": subject.Title,
			"body": truncateMarked(subject.Body, 1000),
		},
		"index": compact,
	}
	input, _ := json.Marshal(payload)
	model := s.Gov.ScreeningModel
	if strings.TrimSpace(model) == "" {
		model = s.Model
	}
	result, err := s.AI.Structured(ctx, model, AIRequest{
		Instructions: s.Config.Prompts["history_screening"],
		Input:        string(input),
	}, s.Config, AICallOptions{Purpose: "历史语境筛选", Normalize: false}, map[string]any{"candidates": []any{}})
	if err != nil {
		s.Logger.LogTemplate("history_screening_failed", s.Config, map[string]string{"error": err.Error()})
		return nil
	}
	if result == nil {
		return nil
	}
	rawCandidates, ok := result["candidates"].([]any)
	if !ok {
		return nil
	}
	return ScreeningGate(rawCandidates, index, s.Gov.MaxScreenedCandidates)
}

func ScreeningGate(candidates []any, index []HistoryIndexItem, limit int) []ScreenedCandidate {
	real := map[string]struct{}{}
	for _, item := range index {
		real[item.Kind+":"+itoa(item.Number)] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := []ScreenedCandidate{}
	for _, raw := range candidates {
		candidate, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		number := parseIntAny(candidate["number"])
		if number <= 0 {
			continue
		}
		kind := "issue"
		if candidate["kind"] == "pr" {
			kind = "pr"
		}
		key := kind + ":" + itoa(number)
		if _, exists := real[key]; !exists {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		relevance, _ := candidate["relevance"].(string)
		if relevance != "direct" && relevance != "context" && relevance != "tangential" {
			relevance = "context"
		}
		out = append(out, ScreenedCandidate{Number: number, Kind: kind, Relevance: relevance})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func parseIntAny(value any) int {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0
		}
		return int(typed)
	case int:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, ok := parseStrictInt(typed)
		if !ok {
			return 0
		}
		return parsed
	default:
		return 0
	}
}
