package types

import "time"

type StageNextMessage struct {
	AppID            int           `json:"appId"`
	StageID          int           `json:"stageId"`
	PipelineID       *int          `json:"pipelineId,omitempty"`
	TraceID          string        `json:"traceId,omitempty"`
	SpanID           string        `json:"spanId,omitempty"`
	StageHandlerName string        `json:"stageHandlerName,omitempty"`
	Input            string        `json:"input,omitempty"`
	PrevStageOutput  string        `json:"prevStageOutput,omitempty"`
	ContextItems     []ContextItem `json:"contextItems,omitempty"`
}

type StageResultMessage struct {
	PipelineID             *int              `json:"pipelineId"`
	StageID                int               `json:"stageId"`
	Result                 string            `json:"result"`
	IsSuccess              bool              `json:"isSuccess"`
	NextStageID            *int              `json:"nextStageId,omitempty"`
	RunNextIfCurrentFailed bool              `json:"runNextIfCurrentFailed"`
	Logs                   []StageLogMessage `json:"logs,omitempty"`
	ContextItems           []ContextItem     `json:"contextItems,omitempty"`
}

type StageLogMessage struct {
	Message  string    `json:"message"`
	LogLevel string    `json:"logLevel"`
	Created  time.Time `json:"created"`
}

type SetStageStatusMessage struct {
	StageID int    `json:"stageId"`
	Status  string `json:"status"`
}

type ContextItem struct {
	Key       string `json:"key" db:"key"`
	Value     string `json:"value" db:"value"`
	ValueType string `json:"valueType,omitempty" db:"value_type"`
}

type PipelineKeyword struct {
	Key   string `json:"key" db:"key"`
	Value string `json:"value" db:"value"`
}
