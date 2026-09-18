package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"pipelogiq/internal/types"
)

func TestPrepareScheduleDefinitionPreservesOnlyExistingRedactedValues(t *testing.T) {
	original := `{"apiKey":"legacy-key","name":"daily","stages":[{"stageName":"work","stageHandlerName":"Handler","input":"{\"token\":\"private-value\"}"}],"pipelineContextItems":[{"key":"token","value":"\"private-value\"","isSensitive":true}]}`
	redacted := string(types.RedactScheduleDefinition(json.RawMessage(original)))
	got, err := prepareScheduleDefinition(redacted, original)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "private-value") || strings.Contains(got, "legacy-key") || strings.Contains(got, types.RedactedContextValue) {
		t.Fatalf("bad stored definition: %s", got)
	}
	if _, err = prepareScheduleDefinition(redacted, ""); !errors.Is(err, ErrScheduleRedactedDefinition) {
		t.Fatalf("new schedule accepted placeholder: %v", err)
	}
	if _, err = prepareScheduleDefinition(strings.Replace(redacted, `"isSensitive":true`, `"isSensitive":false`, 1), original); !errors.Is(err, ErrScheduleRedactedDefinition) {
		t.Fatalf("sensitive flag downgrade accepted: %v", err)
	}
	changedInput := strings.Replace(redacted, `[REDACTED]`, `[REDACTED]-edited`, 1)
	if _, err = prepareScheduleDefinition(changedInput, original); !errors.Is(err, ErrScheduleRedactedDefinition) {
		t.Fatalf("ambiguous redacted edit accepted: %v", err)
	}
}

func TestPrepareScheduleDefinitionPreservesRepeatedFieldOccurrences(t *testing.T) {
	original := types.PipelineCreateRequest{Name: "daily",
		Stages: []types.StageCreate{
			{Name: "work", StageHandler: "Handler", Input: "secret-one"},
			{Name: "work", StageHandler: "Handler", Input: "secret-two"},
		},
		PipelineContext: []types.ContextItem{
			{Key: "token", Value: "secret-one", IsSensitive: true},
			{Key: "token", Value: "secret-two", IsSensitive: true},
		},
	}
	stored, err := types.MarshalScheduleDefinition(original)
	if err != nil {
		t.Fatal(err)
	}
	public := types.RedactScheduleDefinition(stored)
	prepared, err := prepareScheduleDefinition(string(public), string(stored))
	if err != nil {
		t.Fatal(err)
	}
	var restored types.PipelineCreateRequest
	if err := json.Unmarshal([]byte(prepared), &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Stages, restored.Stages) || !reflect.DeepEqual(original.PipelineContext, restored.PipelineContext) {
		t.Fatalf("redacted round trip changed repeated occurrences: %s", prepared)
	}
	// Moving an ambiguous duplicate to a new index cannot safely select a secret.
	var moved types.PipelineCreateRequest
	if err := json.Unmarshal(public, &moved); err != nil {
		t.Fatal(err)
	}
	moved.Stages = append([]types.StageCreate{{Name: "added", StageHandler: "Other"}, {Name: "added-two", StageHandler: "Other"}}, moved.Stages...)
	encoded, err := types.MarshalScheduleDefinition(moved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareScheduleDefinition(string(encoded), string(stored)); !errors.Is(err, ErrScheduleRedactedDefinition) {
		t.Fatalf("ambiguous moved stage accepted: %v", err)
	}
}
