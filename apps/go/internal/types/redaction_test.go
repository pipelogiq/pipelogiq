package types

import (
	"strings"
	"testing"
	"time"
)

func TestRedactPipelineResponse_RedactsSerializedStringSecretFromContextLogsAndStatusPayloads(t *testing.T) {
	secret := `"access-token-value"`
	input := `{"token":"access-token-value"}`
	output := "gateway echoed access-token-value"
	source := &PipelineResponse{
		PipelineContext: []ContextItem{{
			Key:         "credential",
			Value:       secret,
			ValueType:   "System.String",
			IsSensitive: true,
		}},
		Stages: []StageResponse{{
			Input:  &input,
			Output: &output,
			Logs: []StageLog{{
				Message:   "request used access-token-value",
				CreatedAt: time.Now().UTC(),
			}},
		}},
	}

	public := RedactPipelineResponse(source)
	if public.PipelineContext[0].Value != RedactedContextValue {
		t.Fatalf("context value = %q", public.PipelineContext[0].Value)
	}
	for name, value := range map[string]string{
		"input":  *public.Stages[0].Input,
		"output": *public.Stages[0].Output,
		"log":    public.Stages[0].Logs[0].Message,
	} {
		if strings.Contains(value, "access-token-value") {
			t.Fatalf("%s still contains sensitive value: %q", name, value)
		}
	}

	// Redaction must not mutate the raw execution representation.
	if source.PipelineContext[0].Value != secret || !strings.Contains(*source.Stages[0].Input, "access-token-value") {
		t.Fatal("redaction mutated source pipeline")
	}
}

func TestRedactStageLogs_RedactsSensitiveContextWithoutMutatingSource(t *testing.T) {
	logs := []StageLog{{
		Message:   "external request used access-token-value",
		CreatedAt: time.Now().UTC(),
	}}
	contextItems := []ContextItem{{
		Key:         "credential",
		Value:       `"access-token-value"`,
		ValueType:   "System.String",
		IsSensitive: true,
	}}

	public := RedactStageLogs(logs, contextItems)
	if strings.Contains(public[0].Message, "access-token-value") {
		t.Fatalf("log still contains sensitive value: %q", public[0].Message)
	}
	if !strings.Contains(logs[0].Message, "access-token-value") {
		t.Fatal("redaction mutated source log")
	}
}
