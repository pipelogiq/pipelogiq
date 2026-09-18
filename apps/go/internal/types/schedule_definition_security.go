package types

import (
	"encoding/json"
	"strings"
)

// SanitizeScheduleDefinition drops authentication and per-execution metadata.
// The stored definition is business intent; each run gets a fresh trace and idempotency key.
func SanitizeScheduleDefinition(definition PipelineCreateRequest) PipelineCreateRequest {
	definition.ApiKey = ""
	definition.TraceID = ""
	definition.IdempotencyKey = ""
	definition.PipelineContext = append([]ContextItem(nil), definition.PipelineContext...)
	context := definition.PipelineContext[:0]
	for _, item := range definition.PipelineContext {
		if !strings.EqualFold(item.Key, "traceparent") && !strings.EqualFold(item.Key, "tracestate") {
			context = append(context, item)
		}
	}
	definition.PipelineContext = context
	return definition
}

// MarshalScheduleDefinition uses the legacy creation schema without its authentication field.
func MarshalScheduleDefinition(definition PipelineCreateRequest) (json.RawMessage, error) {
	// #nosec G117 -- Sanitization clears ApiKey, and its JSON key is removed below.
	raw, err := json.Marshal(SanitizeScheduleDefinition(definition))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "apiKey")
	return json.Marshal(fields)
}

// RedactScheduleDefinition returns a safe public projection, including definitions stored by older SDKs.
func RedactScheduleDefinition(raw json.RawMessage) json.RawMessage {
	var definition PipelineCreateRequest
	if err := json.Unmarshal(raw, &definition); err != nil {
		return json.RawMessage(`{}`)
	}
	definition = SanitizeScheduleDefinition(definition)
	secrets := sensitiveContextValues(definition.PipelineContext)
	definition.PipelineContext = RedactContextItems(definition.PipelineContext)
	definition.Stages = append([]StageCreate(nil), definition.Stages...)
	for i := range definition.Stages {
		definition.Stages[i].Input = redactKnownValues(definition.Stages[i].Input, secrets)
	}
	encoded, err := MarshalScheduleDefinition(definition)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}
