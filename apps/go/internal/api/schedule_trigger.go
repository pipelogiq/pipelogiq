package api

import (
	"context"
	"encoding/json"
	"time"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

// triggerScheduleRun records a Manual run and creates its pipeline. It duplicates
// none of the scheduler's policy logic on purpose: a manual run is explicitly
// requested, so overlap and catch-up policies do not apply to it.
func triggerScheduleRun(
	ctx context.Context,
	st *store.Store,
	appID int,
	stored types.ScheduleResponse,
	input *json.RawMessage,
) (int64, int, error) {
	tick := time.Now().UTC()

	runID, err := st.RecordScheduleRun(ctx, store.ScheduleRunRecord{
		ScheduleID:        stored.ID,
		ScheduledFor:      tick,
		Trigger:           types.ScheduleTriggerManual,
		Outcome:           types.ScheduleRunOutcomeCreated,
		DefinitionVersion: stored.DefinitionVersion,
	})
	if err != nil {
		return 0, 0, err
	}

	definition, err := store.DecodeScheduleDefinition(string(stored.Definition))
	if err != nil {
		_ = st.FailScheduleRun(ctx, runID, err.Error())
		return 0, 0, err
	}

	previous, err := st.LastScheduledPipelineID(ctx, stored.ID)
	if err != nil {
		_ = st.FailScheduleRun(ctx, runID, err.Error())
		return 0, 0, err
	}

	definition = store.ScheduleSubstitution{
		ScheduledFor:       tick,
		RunID:              runID,
		PreviousPipelineID: previous,
	}.Apply(definition)
	definition = store.WithScheduleKeyword(definition, stored.Name)

	// An input override replaces the first stage's input, which is the only
	// thing a "Run now" dialog can meaningfully change.
	if input != nil && len(definition.Stages) > 0 {
		definition.Stages[0].Input = string(*input)
	}

	pipeline, _, err := st.CreatePipelineIdempotent(
		ctx, definition, appID, store.ManualScheduleIdempotencyKey(stored.ID, runID),
	)
	if err != nil {
		_ = st.FailScheduleRun(ctx, runID, err.Error())
		return 0, 0, err
	}

	if err = st.AttachPipelineToSchedule(ctx, pipeline.ID, stored.ID, runID); err != nil {
		return 0, 0, err
	}
	if err = st.LinkScheduleRunPipeline(ctx, runID, pipeline.ID); err != nil {
		return 0, 0, err
	}
	return runID, pipeline.ID, nil
}
