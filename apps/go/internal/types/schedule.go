package types

import (
	"encoding/json"
	"strings"
	"time"
)

// Schedule kinds.
const (
	ScheduleKindCron     = "Cron"
	ScheduleKindInterval = "Interval"
	ScheduleKindOnce     = "Once"
)

// Schedule statuses.
const (
	ScheduleStatusActive   = "Active"
	ScheduleStatusPaused   = "Paused"
	ScheduleStatusArchived = "Archived"
)

// Overlap policies decide what happens when a tick fires while a previous run of
// the same schedule is still unfinished.
const (
	OverlapPolicySkip    = "Skip"
	OverlapPolicyQueue   = "Queue"
	OverlapPolicyReplace = "Replace"
	OverlapPolicyAllow   = "Allow"
)

// Catch-up policies decide what happens to ticks missed while the scheduler was
// down or the schedule was paused.
const (
	CatchupPolicyNone = "None"
	CatchupPolicyOne  = "One"
	CatchupPolicyAll  = "All"
)

// Run triggers.
const (
	ScheduleTriggerCron     = "Cron"
	ScheduleTriggerInterval = "Interval"
	ScheduleTriggerOnce     = "Once"
	ScheduleTriggerManual   = "Manual"
	ScheduleTriggerCatchup  = "Catchup"
)

// Run outcomes.
const (
	ScheduleRunOutcomeCreated = "Created"
	ScheduleRunOutcomeSkipped = "Skipped"
	ScheduleRunOutcomeFailed  = "Failed"
)

// Skip reasons.
const (
	ScheduleSkipReasonOverlap = "Overlap"
	ScheduleSkipReasonPaused  = "Paused"
	ScheduleSkipReasonCatchup = "Catchup"
)

// Placeholders substituted into stage inputs and context values. These three and
// nothing else: a schedule is a stored pipeline definition, not a template engine.
const (
	PlaceholderScheduledFor     = "{{scheduledFor}}"
	PlaceholderRunID            = "{{runId}}"
	PlaceholderPreviousPipeline = "{{previousPipelineId}}"
)

// ScheduleUpsertRequest is the body of PUT /schedules/{name}.
type ScheduleUpsertRequest struct {
	Kind            string                `json:"kind"`
	CronExpression  string                `json:"cronExpression,omitempty"`
	TimeZone        string                `json:"timeZone,omitempty"`
	IntervalSeconds *int                  `json:"intervalSeconds,omitempty"`
	RunAt           *time.Time            `json:"runAt,omitempty"`
	OverlapPolicy   string                `json:"overlapPolicy,omitempty"`
	CatchupPolicy   string                `json:"catchupPolicy,omitempty"`
	CatchupMax      *int                  `json:"catchupMax,omitempty"`
	JitterSeconds   *int                  `json:"jitterSeconds,omitempty"`
	Definition      PipelineCreateRequest `json:"definition"`
}

// ScheduleTriggerRequest is the body of POST /schedules/{name}/trigger.
type ScheduleTriggerRequest struct {
	Input *json.RawMessage `json:"input,omitempty"`
}

// ScheduleResponse is the external and internal projection of one schedule.
type ScheduleResponse struct {
	ID                int64           `json:"id"`
	ApplicationID     int             `json:"applicationId"`
	Name              string          `json:"name"`
	Status            string          `json:"status"`
	Kind              string          `json:"kind"`
	CronExpression    string          `json:"cronExpression,omitempty"`
	TimeZone          string          `json:"timeZone"`
	IntervalSeconds   *int            `json:"intervalSeconds,omitempty"`
	RunAt             *time.Time      `json:"runAt,omitempty"`
	Definition        json.RawMessage `json:"definition"`
	DefinitionVersion int             `json:"definitionVersion"`
	OverlapPolicy     string          `json:"overlapPolicy"`
	CatchupPolicy     string          `json:"catchupPolicy"`
	CatchupMax        int             `json:"catchupMax"`
	JitterSeconds     int             `json:"jitterSeconds"`
	NextRunAt         *time.Time      `json:"nextRunAt,omitempty"`
	LastRunAt         *time.Time      `json:"lastRunAt,omitempty"`
	LastRunStatus     string          `json:"lastRunStatus,omitempty"`
	CreatedBy         string          `json:"createdBy,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	Health            *ScheduleHealth `json:"health,omitempty"`
}

// ScheduleHealth summarises the recent run history for the dashboard list.
type ScheduleHealth struct {
	RecentRuns    int `json:"recentRuns"`
	RecentFailed  int `json:"recentFailed"`
	RecentSkipped int `json:"recentSkipped"`
	// Outcomes is newest-first, at most 20 entries, for the outcome strip.
	Outcomes   []string `json:"outcomes"`
	LagSeconds *float64 `json:"lagSeconds,omitempty"`
}

// ScheduleRunResponse is one row of the run history.
type ScheduleRunResponse struct {
	ID                int64      `json:"id"`
	ScheduleID        int64      `json:"scheduleId"`
	ScheduledFor      time.Time  `json:"scheduledFor"`
	FiredAt           time.Time  `json:"firedAt"`
	Trigger           string     `json:"trigger"`
	Outcome           string     `json:"outcome"`
	SkipReason        string     `json:"skipReason,omitempty"`
	PipelineID        *int       `json:"pipelineId,omitempty"`
	DefinitionVersion int        `json:"definitionVersion"`
	Error             string     `json:"error,omitempty"`
	LagSeconds        float64    `json:"lagSeconds"`
	PipelineStatus    string     `json:"pipelineStatus,omitempty"`
	PipelineFinished  *time.Time `json:"pipelineFinishedAt,omitempty"`
}

// ScheduleListResponse is a cursor page of schedules.
type ScheduleListResponse struct {
	Schedules  []ScheduleResponse `json:"schedules"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

// ScheduleRunListResponse is a cursor page of runs.
type ScheduleRunListResponse struct {
	Runs       []ScheduleRunResponse `json:"runs"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

// NormalizeScheduleKind maps user input onto a canonical kind, or "".
func NormalizeScheduleKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "cron":
		return ScheduleKindCron
	case "interval":
		return ScheduleKindInterval
	case "once":
		return ScheduleKindOnce
	default:
		return ""
	}
}

// NormalizeOverlapPolicy maps user input onto a canonical policy, or "".
func NormalizeOverlapPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "skip":
		return OverlapPolicySkip
	case "queue":
		return OverlapPolicyQueue
	case "replace":
		return OverlapPolicyReplace
	case "allow", "parallel", "concurrent":
		return OverlapPolicyAllow
	default:
		return ""
	}
}

// NormalizeCatchupPolicy maps user input onto a canonical policy, or "".
func NormalizeCatchupPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "none":
		return CatchupPolicyNone
	case "one":
		return CatchupPolicyOne
	case "all":
		return CatchupPolicyAll
	default:
		return ""
	}
}

// NormalizeScheduleStatus maps user input onto a canonical status, or "".
func NormalizeScheduleStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return ScheduleStatusActive
	case "paused":
		return ScheduleStatusPaused
	case "archived":
		return ScheduleStatusArchived
	default:
		return ""
	}
}
