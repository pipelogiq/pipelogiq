package types

import (
	"strings"
	"time"
)

const (
	RetryDispositionRetryable = "retryable"
	RetryDispositionTerminal  = "terminal"
)

const (
	ErrorCodeTimeout              = "TIMEOUT"
	ErrorCodeUpstreamError        = "UPSTREAM_ERROR"
	ErrorCodeRateLimitExceeded    = "RATE_LIMIT_EXCEEDED"
	ErrorCodeTransportUnavailable = "TRANSPORT_UNAVAILABLE"
	ErrorCodeBusinessRejected     = "BUSINESS_REJECTED"
	ErrorCodeValidationError      = "VALIDATION_ERROR"
	ErrorCodeInvalidState         = "INVALID_STATE"
	ErrorCodeMissingRequiredData  = "MISSING_REQUIRED_DATA"
)

type StageLeaseRequest struct {
	ExecutionID string `json:"executionId"`
	WorkerID    string `json:"workerId"`
}

type StageLeaseResponse struct {
	Acquired       bool       `json:"acquired"`
	Attempt        int        `json:"attempt,omitempty"`
	LeaseExpiresAt *time.Time `json:"leaseExpiresAt,omitempty"`
	Reason         string     `json:"reason,omitempty"`
}

type PipelineLookupByIdempotencyKeyRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
}

func IsTerminalStageStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case strings.ToLower(StageStatusCompleted),
		strings.ToLower(StageStatusFailed),
		strings.ToLower(StageStatusSkipped),
		strings.ToLower(StageStatusCancelled):
		return true
	default:
		return false
	}
}

func IsTerminalPipelineStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case strings.ToLower(PipelineStatusCompleted),
		strings.ToLower(PipelineStatusFailed),
		strings.ToLower(PipelineStatusCancelled):
		return true
	default:
		return false
	}
}
