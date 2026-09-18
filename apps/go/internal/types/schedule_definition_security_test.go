package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScheduleDefinitionSanitizationAndRedaction(t *testing.T) {
	original := PipelineCreateRequest{ApiKey: "private-api-key", TraceID: "old-trace", IdempotencyKey: "old-run", Name: "daily",
		Stages: []StageCreate{{Name: "work", StageHandler: "Handler", Input: `{"token":"private-value"}`}},
		PipelineContext: []ContextItem{
			{Key: "TraceParent", Value: `"old-trace"`}, {Key: "tracestate", Value: `"vendor=1"`},
			{Key: "token", Value: `"private-value"`, ValueType: "System.String", IsSensitive: true},
			{Key: "public", Value: `42`},
		},
	}
	persisted, err := MarshalScheduleDefinition(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-api-key", "old-trace", "old-run", "traceparent", "tracestate", "apiKey"} {
		if strings.Contains(strings.ToLower(string(persisted)), strings.ToLower(forbidden)) {
			t.Fatalf("persisted runtime/auth metadata: %s", persisted)
		}
	}
	if !strings.Contains(string(persisted), "private-value") {
		t.Fatal("storage lost the execution secret")
	}
	public := RedactScheduleDefinition(persisted)
	if strings.Contains(string(public), "private-value") {
		t.Fatalf("public definition leaked a sensitive value: %s", public)
	}
	var decoded PipelineCreateRequest
	if err = json.Unmarshal(public, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PipelineContext[0].Value != RedactedContextValue || !decoded.PipelineContext[0].IsSensitive {
		t.Fatal("redaction flag/marker lost")
	}
	if decoded.PipelineContext[1].Value != "42" {
		t.Fatal("public value changed")
	}
	if original.PipelineContext[0].Key != "TraceParent" || original.Stages[0].Input != `{"token":"private-value"}` {
		t.Fatal("redaction mutated source")
	}
}

func TestScheduleDefinitionRedactionFailsClosedForInvalidLegacyJSON(t *testing.T) {
	if got := string(RedactScheduleDefinition(json.RawMessage(`{"private":"secret"`))); got != `{}` {
		t.Fatalf("invalid definition = %s", got)
	}
}
