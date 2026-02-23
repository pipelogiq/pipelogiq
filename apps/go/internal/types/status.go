package types

const (
	StageStatusNotStarted     = "NotStarted"
	StageStatusRunning        = "Running"
	StageStatusPending        = "Pending"
	StageStatusRetryScheduled = "RetryScheduled"
	StageStatusCompleted      = "Completed"
	StageStatusFailed         = "Failed"
	StageStatusSkipped        = "Skipped"
)

const (
	PipelineStatusNotStarted = "NotStarted"
	PipelineStatusRunning    = "Running"
	PipelineStatusCompleted  = "Completed"
	PipelineStatusFailed     = "Failed"
)

const (
	WorkerStateStarting = "starting"
	WorkerStateReady    = "ready"
	WorkerStateDegraded = "degraded"
	WorkerStateDraining = "draining"
	WorkerStateStopped  = "stopped"
	WorkerStateError    = "error"
	WorkerStateOffline  = "offline"
)
