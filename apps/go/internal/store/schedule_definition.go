package store

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pipelogiq/internal/types"
)

// ScheduleSubstitution carries the three values a run may reference. Nothing else
// is templated: a schedule is a stored pipeline definition plus a trigger, not a
// template engine.
type ScheduleSubstitution struct {
	ScheduledFor       time.Time
	RunID              int64
	PreviousPipelineID *int
}

// Apply substitutes the three placeholders in stage inputs and context values.
// Stage and pipeline names, handler names, and keywords are left alone.
func (s ScheduleSubstitution) Apply(req types.PipelineCreateRequest) types.PipelineCreateRequest {
	previous := ""
	if s.PreviousPipelineID != nil {
		previous = strconv.Itoa(*s.PreviousPipelineID)
	}
	replacer := strings.NewReplacer(
		types.PlaceholderScheduledFor, s.ScheduledFor.UTC().Format(time.RFC3339),
		types.PlaceholderRunID, strconv.FormatInt(s.RunID, 10),
		types.PlaceholderPreviousPipeline, previous,
	)

	out := req
	out.Stages = make([]types.StageCreate, len(req.Stages))
	copy(out.Stages, req.Stages)
	for i := range out.Stages {
		out.Stages[i].Input = replacer.Replace(out.Stages[i].Input)
	}

	out.PipelineContext = make([]types.ContextItem, len(req.PipelineContext))
	copy(out.PipelineContext, req.PipelineContext)
	for i := range out.PipelineContext {
		out.PipelineContext[i].Value = replacer.Replace(out.PipelineContext[i].Value)
	}

	return out
}

// ScheduleIdempotencyKey is the key a scheduled run creates its pipeline under.
// It is derived from the tick, so a retry of the same tick resolves the existing
// pipeline instead of creating a second one.
func ScheduleIdempotencyKey(scheduleID int64, scheduledFor time.Time) string {
	return fmt.Sprintf("schedule:%d:%s", scheduleID, scheduledFor.UTC().Format(time.RFC3339))
}

// ManualScheduleIdempotencyKey is the key a manual run creates its pipeline
// under. It is derived from the run id rather than the clock.
//
// Keying a manual run on its timestamp collides with a scheduled tick that fired
// in the same second, and the two definitions are not identical — {{runId}}
// differs — so the collision surfaces as an idempotency conflict rather than
// resolving. A run id is unique by construction and still makes a retry of the
// same manual run idempotent.
func ManualScheduleIdempotencyKey(scheduleID int64, runID int64) string {
	return fmt.Sprintf("schedule:%d:manual:%d", scheduleID, runID)
}

// DecodeScheduleDefinition parses the stored definition back into a create request.
func DecodeScheduleDefinition(definitionJSON string) (types.PipelineCreateRequest, error) {
	var req types.PipelineCreateRequest
	if err := json.Unmarshal([]byte(definitionJSON), &req); err != nil {
		return types.PipelineCreateRequest{}, fmt.Errorf("decode schedule definition: %w", err)
	}
	return types.SanitizeScheduleDefinition(req), nil
}

// WithScheduleKeyword appends the schedule keyword the dashboard filters on,
// without duplicating one the definition already carries.
func WithScheduleKeyword(req types.PipelineCreateRequest, scheduleName string) types.PipelineCreateRequest {
	for _, keyword := range req.PipelineKeywords {
		if strings.EqualFold(keyword.Key, "schedule") {
			return req
		}
	}
	out := req
	out.PipelineKeywords = append(
		append([]types.PipelineKeyword(nil), req.PipelineKeywords...),
		types.PipelineKeyword{Key: "schedule", Value: scheduleName},
	)
	return out
}
