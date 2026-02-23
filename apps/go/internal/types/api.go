package types

import "time"

// Pipeline types

type PipelineCreateRequest struct {
	ApiKey           string            `json:"apiKey"`
	Name             string            `json:"name"`
	TraceID          string            `json:"traceId,omitempty"`
	Stages           []StageCreate     `json:"stages"`
	PipelineKeywords []PipelineKeyword `json:"pipelineKeywords,omitempty"`
	PipelineContext  []ContextItem     `json:"pipelineContextItems,omitempty"`
}

type StageCreate struct {
	Name            string        `json:"stageName"`
	StageHandler    string        `json:"stageHandlerName"`
	Description     string        `json:"description,omitempty"`
	Input           string        `json:"input,omitempty"`
	Options         *StageOptions `json:"options,omitempty"`
	IsEvent         bool          `json:"isEvent,omitempty"`
	RunNextIfFailed bool          `json:"runNextIfFailed,omitempty"`
}

type StageOptions struct {
	RunNextIfFailed   *bool    `json:"runNextIfFailed,omitempty"`
	RetryInterval     *int     `json:"retryInterval,omitempty"` // seconds
	TimeOut           *int     `json:"timeOut,omitempty"`       // seconds
	MaxRetries        *int     `json:"maxRetries,omitempty"`
	DependsOn         []string `json:"dependsOn,omitempty"`
	RunInParallelWith []string `json:"runInParallelWith,omitempty"`
	FailIfOutputEmpty *bool    `json:"failIfOutputEmpty,omitempty"`
	NotifyOnFailure   *bool    `json:"notifyOnFailure,omitempty"`
	RunAsUser         *string  `json:"runAsUser,omitempty"`
}

type PipelineResponse struct {
	ID               int               `json:"id"`
	Name             string            `json:"name"`
	TraceID          string            `json:"traceId,omitempty"`
	Status           string            `json:"status"`
	CreatedAt        time.Time         `json:"createdAt"`
	FinishedAt       *time.Time        `json:"finishedAt,omitempty"`
	ApplicationID    *int              `json:"applicationId,omitempty"`
	StageStatuses    []string          `json:"stageStatuses,omitempty"`
	Stages           []StageResponse   `json:"stages,omitempty"`
	PipelineContext  []ContextItem     `json:"pipelineContextItems,omitempty"`
	PipelineKeywords []PipelineKeyword `json:"pipelineKeywords,omitempty"`
	IsEvent          *bool             `json:"isEvent,omitempty"`
}

type StageResponse struct {
	ID               int           `json:"id" db:"id"`
	PipelineID       int           `json:"pipelineId" db:"pipeline_id"`
	SpanID           string        `json:"spanId,omitempty" db:"span_id"`
	Name             string        `json:"name" db:"name"`
	StageHandlerName string        `json:"stageHandlerName,omitempty" db:"stage_handler_name"`
	Description      string        `json:"description,omitempty" db:"description"`
	Status           string        `json:"status,omitempty" db:"status"`
	CreatedAt        time.Time     `json:"createdAt" db:"created_at"`
	FinishedAt       *time.Time    `json:"finishedAt,omitempty" db:"finished_at"`
	StartedAt        *time.Time    `json:"startedAt,omitempty" db:"started_at"`
	Output           *string       `json:"output,omitempty" db:"output"`
	Input            *string       `json:"input,omitempty" db:"input"`
	IsSkipped        *bool         `json:"isSkipped,omitempty" db:"is_skipped"`
	IsEvent          *bool         `json:"isEvent,omitempty" db:"is_event"`
	NextStageID      *int          `json:"nextStageId,omitempty"`
	Logs             []StageLog    `json:"logs,omitempty"`
	Options          *StageOptions `json:"options,omitempty"`
}

type StageLog struct {
	ID        int       `json:"id,omitempty" db:"id"`
	StageID   int       `json:"stageId,omitempty" db:"stage_id"`
	Message   string    `json:"message" db:"log"`
	LogLevel  string    `json:"logLevel,omitempty" db:"log_level"`
	CreatedAt time.Time `json:"created" db:"created_at"`
}

// Pagination

type GetPipelinesRequest struct {
	PageNumber        *int     `json:"pageNumber"`
	PageSize          *int     `json:"pageSize"`
	ApplicationID     *int     `json:"applicationId"`
	Search            *string  `json:"search"`
	Keywords          []string `json:"keywords"`
	PipelineStartFrom *string  `json:"pipelineStartFrom"`
	PipelineStartTo   *string  `json:"pipelineStartTo"`
	PipelineEndFrom   *string  `json:"pipelineEndFrom"`
	PipelineEndTo     *string  `json:"pipelineEndTo"`
	Statuses          []string `json:"statuses"`
}

type PagedResult[T any] struct {
	Items      []T `json:"items"`
	TotalCount int `json:"totalCount"`
	PageNumber int `json:"pageNumber"`
	PageSize   int `json:"pageSize"`
}

// Stage actions

type RerunStageRequest struct {
	StageID            int  `json:"stageId"`
	RerunAllNextStages bool `json:"rerunAllNextStages"`
}

type SkipStageRequest struct {
	StageID int `json:"stageId"`
}

// Auth types

type UserResponse struct {
	ID        int       `json:"id" db:"id"`
	FirstName string    `json:"firstName" db:"first_name"`
	LastName  *string   `json:"lastName,omitempty" db:"last_name"`
	Email     string    `json:"email" db:"email"`
	Role      string    `json:"role" db:"role"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Application types

type ApplicationResponse struct {
	ID          int              `json:"id" db:"id"`
	Name        string           `json:"name" db:"name"`
	Description *string          `json:"description,omitempty" db:"description"`
	ApiKeys     []ApiKeyResponse `json:"apiKeys,omitempty"`
}

type SaveApplicationRequest struct {
	ID          *int    `json:"id,omitempty"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// ApiKey types

type ApiKeyResponse struct {
	ID            int        `json:"id" db:"id"`
	ApplicationID int        `json:"applicationId" db:"application_id"`
	Name          *string    `json:"name,omitempty" db:"name"`
	Key           *string    `json:"key,omitempty" db:"key"`
	CreatedAt     *time.Time `json:"createdAt,omitempty" db:"created_at"`
	DisabledAt    *time.Time `json:"disabledAt,omitempty" db:"disabled_at"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty" db:"expires_at"`
	LastUsed      *time.Time `json:"lastUsed,omitempty" db:"last_used"`
}

type GenerateApiKeyRequest struct {
	ApiKeyID       *int                  `json:"apiKeyId,omitempty"`
	ApplicationID  *int                  `json:"applicationId,omitempty"`
	NewApplication *ApiKeyNewApplication `json:"newApplication,omitempty"`
	Name           *string               `json:"name,omitempty"`
	ExpiresAt      *time.Time            `json:"expiresAt,omitempty"`
}

type ApiKeyNewApplication struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type DisableApiKeyRequest struct {
	ApiKeyID int `json:"apiKeyId"`
}

type RabbitConnectionResponse struct {
	ConnectionString string `json:"connectionString"`
}

type WorkerBootstrapRequest struct {
	WorkerName        string         `json:"workerName"`
	InstanceID        string         `json:"instanceId,omitempty"`
	WorkerVersion     string         `json:"workerVersion,omitempty"`
	SDKVersion        string         `json:"sdkVersion,omitempty"`
	Environment       string         `json:"environment,omitempty"`
	HostName          string         `json:"hostName,omitempty"`
	PID               *int           `json:"pid,omitempty"`
	SupportedHandlers []string       `json:"supportedHandlers,omitempty"`
	Capabilities      map[string]any `json:"capabilities,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type WorkerBootstrapResponse struct {
	WorkerID           string                  `json:"workerId"`
	WorkerSessionToken string                  `json:"workerSessionToken"`
	ConfigVersion      string                  `json:"configVersion"`
	Application        WorkerApplicationInfo   `json:"application"`
	MessageBroker      WorkerBrokerInfo        `json:"messageBroker"`
	Queues             WorkerQueueTopology     `json:"queues"`
	Heartbeat          WorkerHeartbeatContract `json:"heartbeat"`
	Observability      WorkerObservabilityInfo `json:"observability"`
}

type WorkerApplicationInfo struct {
	ApplicationID   int    `json:"applicationId"`
	ApplicationName string `json:"applicationName"`
	AppID           string `json:"appId"`
}

type WorkerBrokerInfo struct {
	Type             string `json:"type"`
	ConnectionString string `json:"connectionString"`
	Prefetch         int    `json:"prefetch"`
	DLQEnabled       bool   `json:"dlqEnabled"`
	DLQTTLSec        int64  `json:"dlqTtlSec"`
}

type WorkerQueueTopology struct {
	StageResult        string `json:"stageResult"`
	StageSetStatus     string `json:"stageSetStatus"`
	StageUpdatedFanout string `json:"stageUpdatedFanout"`
	StageNextPattern   string `json:"stageNextPattern"`
}

type WorkerHeartbeatContract struct {
	IntervalSec     int64 `json:"intervalSec"`
	OfflineAfterSec int64 `json:"offlineAfterSec"`
}

type WorkerObservabilityInfo struct {
	TraceLinkTemplate string `json:"traceLinkTemplate,omitempty"`
	LogsLinkTemplate  string `json:"logsLinkTemplate,omitempty"`
}

type WorkerHeartbeatRequest struct {
	WorkerID        string         `json:"workerId"`
	State           string         `json:"state"`
	UptimeSec       *int64         `json:"uptimeSec,omitempty"`
	BrokerConnected *bool          `json:"brokerConnected,omitempty"`
	InFlightJobs    *int           `json:"inFlightJobs,omitempty"`
	JobsProcessed   *int64         `json:"jobsProcessed,omitempty"`
	JobsFailed      *int64         `json:"jobsFailed,omitempty"`
	QueueLag        *int           `json:"queueLag,omitempty"`
	CPUPercent      *float64       `json:"cpuPercent,omitempty"`
	MemoryMB        *float64       `json:"memoryMb,omitempty"`
	LastError       *string        `json:"lastError,omitempty"`
	Message         *string        `json:"message,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type WorkerEventsRequest struct {
	WorkerID string             `json:"workerId"`
	Events   []WorkerEventInput `json:"events"`
}

type WorkerEventInput struct {
	TS        *time.Time     `json:"ts,omitempty"`
	Level     string         `json:"level"`
	EventType string         `json:"eventType"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}

type WorkerShutdownRequest struct {
	WorkerID string `json:"workerId"`
	Reason   string `json:"reason,omitempty"`
}

type WorkerStatusResponse struct {
	ID                string         `json:"id" db:"id"`
	ApplicationID     int            `json:"applicationId" db:"application_id"`
	ApplicationName   string         `json:"applicationName" db:"application_name"`
	AppID             string         `json:"appId" db:"app_runtime_id"`
	WorkerName        string         `json:"workerName" db:"worker_name"`
	InstanceID        string         `json:"instanceId" db:"instance_id"`
	WorkerVersion     *string        `json:"workerVersion,omitempty" db:"worker_version"`
	SDKVersion        *string        `json:"sdkVersion,omitempty" db:"sdk_version"`
	Environment       *string        `json:"environment,omitempty" db:"environment"`
	HostName          *string        `json:"hostName,omitempty" db:"host_name"`
	PID               *int           `json:"pid,omitempty" db:"pid"`
	State             string         `json:"state" db:"state"`
	EffectiveState    string         `json:"effectiveState"`
	StatusReason      *string        `json:"statusReason,omitempty" db:"status_reason"`
	BrokerType        *string        `json:"brokerType,omitempty" db:"broker_type"`
	BrokerConnected   bool           `json:"brokerConnected" db:"broker_connected"`
	InFlightJobs      int            `json:"inFlightJobs" db:"in_flight_jobs"`
	JobsProcessed     int64          `json:"jobsProcessed" db:"jobs_processed"`
	JobsFailed        int64          `json:"jobsFailed" db:"jobs_failed"`
	QueueLag          *int           `json:"queueLag,omitempty" db:"queue_lag"`
	CPUPercent        *float64       `json:"cpuPercent,omitempty" db:"cpu_percent"`
	MemoryMB          *float64       `json:"memoryMb,omitempty" db:"memory_mb"`
	LastError         *string        `json:"lastError,omitempty" db:"last_error"`
	StartedAt         string         `json:"startedAt" db:"started_at"`
	LastSeenAt        string         `json:"lastSeenAt" db:"last_seen_at"`
	StoppedAt         *string        `json:"stoppedAt,omitempty" db:"stopped_at"`
	UpdatedAt         string         `json:"updatedAt" db:"updated_at"`
	SupportedHandlers []string       `json:"supportedHandlers,omitempty"`
	Capabilities      map[string]any `json:"capabilities,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type WorkerStatusListResponse struct {
	Items           []WorkerStatusResponse `json:"items"`
	TotalCount      int                    `json:"totalCount"`
	OnlineCount     int                    `json:"onlineCount"`
	OfflineCount    int                    `json:"offlineCount"`
	DegradedCount   int                    `json:"degradedCount"`
	OfflineAfterSec int64                  `json:"offlineAfterSec"`
}

type WorkerEventResponse struct {
	ID              int64          `json:"id" db:"id"`
	WorkerID        string         `json:"workerId" db:"worker_id"`
	WorkerName      string         `json:"workerName" db:"worker_name"`
	ApplicationID   int            `json:"applicationId" db:"application_id"`
	ApplicationName string         `json:"applicationName" db:"application_name"`
	TS              string         `json:"ts" db:"ts"`
	Level           string         `json:"level" db:"level"`
	EventType       string         `json:"eventType" db:"event_type"`
	Message         string         `json:"message" db:"message"`
	Details         map[string]any `json:"details,omitempty"`
}

type WorkerListRequest struct {
	ApplicationID *int
	State         *string
	Search        *string
	Limit         int
}

type WorkerEventListRequest struct {
	WorkerID      *string
	ApplicationID *int
	Limit         int
}

// Log types

type LogRequest struct {
	ID       *int              `json:"id,omitempty"`
	ApiKey   *string           `json:"apiKey,omitempty"`
	Created  *time.Time        `json:"created,omitempty"`
	Message  *string           `json:"message,omitempty"`
	LogLevel *string           `json:"logLevel,omitempty"`
	Keywords []PipelineKeyword `json:"keywords,omitempty"`
}

type LogResponse struct {
	ID            int               `json:"id" db:"id"`
	ApplicationID *int              `json:"applicationId,omitempty" db:"application_id"`
	Message       *string           `json:"message,omitempty" db:"log"`
	LogLevel      *string           `json:"logLevel,omitempty" db:"log_level"`
	CreatedAt     *time.Time        `json:"created,omitempty" db:"created_at"`
	Keywords      []PipelineKeyword `json:"keywords,omitempty"`
}
