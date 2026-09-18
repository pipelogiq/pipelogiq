package store

import (
	"encoding/json"
	"errors"
	"strings"

	"pipelogiq/internal/types"
)

// ErrScheduleRedactedDefinition rejects an unrecoverable public redaction placeholder.
var ErrScheduleRedactedDefinition = errors.New("redacted schedule values must refer to unchanged existing sensitive fields; provide an explicit replacement to edit them")

// prepareScheduleDefinition restores unchanged public placeholders while holding the schedule row lock.
// It never persists [REDACTED] in place of a secret and never fetches another application's definition.
func prepareScheduleDefinition(incoming, previous string) (string, error) {
	var next, old types.PipelineCreateRequest
	if err := json.Unmarshal([]byte(incoming), &next); err != nil {
		return "", err
	}
	if previous != "" {
		if err := json.Unmarshal([]byte(previous), &old); err != nil {
			return "", err
		}
	}
	next = types.SanitizeScheduleDefinition(next)
	old = types.SanitizeScheduleDefinition(old)
	var publicOld types.PipelineCreateRequest
	if previous != "" {
		if err := json.Unmarshal(types.RedactScheduleDefinition(json.RawMessage(previous)), &publicOld); err != nil {
			return "", err
		}
	}
	for i, item := range next.PipelineContext {
		if !strings.Contains(item.Value, types.RedactedContextValue) {
			continue
		}
		if item.Value != types.RedactedContextValue {
			return "", ErrScheduleRedactedDefinition
		}
		matches := func(j int) bool {
			return item.IsSensitive && old.PipelineContext[j].IsSensitive && strings.EqualFold(item.Key, old.PipelineContext[j].Key)
		}
		matched := -1
		if i < len(old.PipelineContext) && matches(i) {
			matched = i
		} else {
			for j := range old.PipelineContext {
				if matches(j) {
					if matched >= 0 {
						return "", ErrScheduleRedactedDefinition
					}
					matched = j
				}
			}
		}
		if matched < 0 {
			return "", ErrScheduleRedactedDefinition
		}
		next.PipelineContext[i] = old.PipelineContext[matched]
	}
	for i, stage := range next.Stages {
		if !strings.Contains(stage.Input, types.RedactedContextValue) {
			continue
		}
		matches := func(j int) bool {
			original := old.Stages[j]
			return stage.Name == original.Name && stage.StageHandler == original.StageHandler && j < len(publicOld.Stages) && stage.Input == publicOld.Stages[j].Input
		}
		matched := -1
		if i < len(old.Stages) && matches(i) {
			matched = i
		} else {
			for j := range old.Stages {
				if matches(j) {
					if matched >= 0 {
						return "", ErrScheduleRedactedDefinition
					}
					matched = j
				}
			}
		}
		if matched < 0 {
			return "", ErrScheduleRedactedDefinition
		}
		next.Stages[i].Input = old.Stages[matched].Input
	}
	encoded, err := types.MarshalScheduleDefinition(next)
	return string(encoded), err
}
