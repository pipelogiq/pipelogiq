package types

import (
	"encoding/json"
	"strings"
)

const RedactedContextValue = "[REDACTED]"

func RedactContextItems(source []ContextItem) []ContextItem {
	result := append([]ContextItem(nil), source...)
	for i := range result {
		if result[i].IsSensitive {
			result[i].Value = RedactedContextValue
		}
	}
	return result
}

// RedactStageLogs returns a detached public representation of stage logs with
// values from sensitive context items removed.
func RedactStageLogs(source []StageLog, contextItems []ContextItem) []StageLog {
	result := append([]StageLog(nil), source...)
	secrets := sensitiveContextValues(contextItems)
	for i := range result {
		result[i].Message = redactKnownValues(result[i].Message, secrets)
	}
	return result
}

// RedactPipelineResponse returns a detached public/status representation.
// Stage execution messages must continue to use the unredacted context loaded
// directly by the store.
func RedactPipelineResponse(source *PipelineResponse) *PipelineResponse {
	if source == nil {
		return nil
	}

	result := *source
	result.StageStatuses = append([]string(nil), source.StageStatuses...)
	result.PipelineKeywords = append([]PipelineKeyword(nil), source.PipelineKeywords...)
	result.PipelineContext = RedactContextItems(source.PipelineContext)
	result.Stages = append([]StageResponse(nil), source.Stages...)

	secrets := sensitiveContextValues(source.PipelineContext)

	for i := range result.Stages {
		stage := &result.Stages[i]
		stage.Logs = append([]StageLog(nil), source.Stages[i].Logs...)
		if stage.Input != nil {
			value := redactKnownValues(*stage.Input, secrets)
			stage.Input = &value
		}
		if stage.Output != nil {
			value := redactKnownValues(*stage.Output, secrets)
			stage.Output = &value
		}
		for j := range stage.Logs {
			stage.Logs[j].Message = redactKnownValues(stage.Logs[j].Message, secrets)
		}
	}

	return &result
}

func sensitiveContextValues(contextItems []ContextItem) []string {
	secrets := make([]string, 0)
	for _, item := range contextItems {
		if item.IsSensitive && item.Value != "" {
			secrets = append(secrets, item.Value)
		}
	}
	return secrets
}

func redactKnownValues(value string, secrets []string) string {
	for _, secret := range secrets {
		for _, candidate := range redactionCandidates(secret) {
			value = strings.ReplaceAll(value, candidate, RedactedContextValue)
		}
	}
	return value
}

func redactionCandidates(secret string) []string {
	if secret == "" {
		return nil
	}
	candidates := []string{secret}
	var decoded string
	if json.Unmarshal([]byte(secret), &decoded) == nil && decoded != "" && decoded != secret {
		candidates = append(candidates, decoded)
	}
	return candidates
}
