package aigovernance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"
)

type Runner struct {
	Getenv        func(string) string
	Stdin         io.Reader
	Logger        Logger
	HTTPClient    *http.Client
	EventPayload  []byte
	ReadGitHub    GitHub
	WriteGitHub   GitHub
	AI            AI
	TokenResolver TokenResolver
}

func (r Runner) Run(ctx context.Context) error {
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	logger := r.Logger
	httpClient := r.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}

	config, err := LoadConfig()
	if err != nil {
		return err
	}
	inputs, err := ParseInputs(&config, getenv)
	if err != nil {
		return err
	}
	for _, warning := range inputs.Warnings {
		logger.Warning("%s", warning)
	}
	if inputs.GovernanceToken == "" {
		inputs.GovernanceToken = r.TokenResolver.Resolve(ctx, "", getenv, logger)
	}
	if inputs.GovernanceToken != "" {
		logger.LogTemplate("governance_identity_enabled", config, nil)
	}

	baseURL := inputs.CustomBaseURL
	if baseURL == "" {
		baseURL = config.Defaults.APIBaseURL
	}
	apiKey := inputs.CustomAPIKey
	if apiKey == "" {
		apiKey = inputs.Token
	}
	aiClient := r.AI
	if aiClient == nil {
		aiClient = NewHTTPAI(baseURL, apiKey, httpClient, logger)
	}
	if inputs.CustomBaseURL != "" {
		logger.LogTemplate("using_custom_api", config, nil)
	} else {
		logger.LogTemplate("using_github_models", config, nil)
	}
	logger.LogTemplate("using_ai_model", config, map[string]string{"model": inputs.AIModel})
	logger.LogTemplate("config_info", config, nil)
	logger.LogTemplate("analysis_depth_info", config, map[string]string{
		"analyze_changes": boolString(inputs.AnalyzeFileChanges),
	})
	logger.LogTemplate("analysis_depth_details", config, map[string]string{
		"depth": inputs.AnalysisDepth, "files": itoa(inputs.MaxFilesToAnalyze), "lines": itoa(inputs.MaxPatchLinesPerFile),
	})

	readGitHub := r.ReadGitHub
	if readGitHub == nil {
		readGitHub = NewGitHubClient(inputs.Token, getenv("GITHUB_API_URL"), getenv("GITHUB_GRAPHQL_URL"), httpClient)
	}
	writeGitHub := r.WriteGitHub
	if writeGitHub == nil {
		writeToken := inputs.GovernanceToken
		if writeToken == "" {
			writeToken = inputs.Token
		}
		writeGitHub = NewGitHubClient(writeToken, getenv("GITHUB_API_URL"), getenv("GITHUB_GRAPHQL_URL"), httpClient)
	}

	payload := r.EventPayload
	if payload == nil {
		eventPath := getenv("GITHUB_EVENT_PATH")
		if eventPath != "" {
			payload, err = os.ReadFile(eventPath)
			if err != nil {
				return err
			}
		} else if r.Stdin != nil {
			payload, err = io.ReadAll(io.LimitReader(r.Stdin, 16<<20))
			if err != nil {
				return err
			}
		}
	}
	if len(payload) == 0 {
		return errors.New("GitHub event payload is empty")
	}
	event, err := ParseEvent(getenv("GITHUB_EVENT_NAME"), payload, getenv("GITHUB_REPOSITORY"))
	if err != nil {
		return err
	}
	orchestrator := Orchestrator{
		Config: config, Inputs: inputs, Read: readGitHub, Write: writeGitHub, AI: aiClient, Logger: logger,
	}
	return orchestrator.Handle(ctx, event)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
