package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"pipelogiq/internal/constants"
	"pipelogiq/internal/mq"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

func (s *ExternalServer) handleAcquireStageLease(w http.ResponseWriter, r *http.Request) {
	s.handleStageLease(w, r, false)
}

func (s *ExternalServer) handleRenewStageLease(w http.ResponseWriter, r *http.Request) {
	s.handleStageLease(w, r, true)
}

func (s *ExternalServer) handleStageLease(w http.ResponseWriter, r *http.Request, renew bool) {
	stageID, err := parseRouteID(chi.URLParam(r, "stageId"))
	if err != nil {
		http.Error(w, "stageId must be a positive integer", http.StatusBadRequest)
		return
	}

	var req types.StageLeaseRequest
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil ||
		strings.TrimSpace(req.ExecutionID) == "" ||
		strings.TrimSpace(req.WorkerID) == "" {
		http.Error(w, "executionId and workerId are required", http.StatusBadRequest)
		return
	}
	sessionToken := extractWorkerSessionToken(r)
	if strings.TrimSpace(sessionToken) == "" {
		http.Error(w, "worker session token is required", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var result *types.StageLeaseResponse
	if renew {
		result, err = s.store.RenewStageLease(
			ctx, stageID, req, sessionToken, store.DefaultStageLeaseDuration,
		)
	} else {
		result, err = s.store.AcquireStageLease(
			ctx, stageID, req, sessionToken, store.DefaultStageLeaseDuration,
		)
	}
	if err != nil {
		switch {
		case store.IsInvalidWorkerSessionError(err):
			http.Error(w, "invalid worker session", http.StatusUnauthorized)
		default:
			s.logger.Error("stage lease operation failed",
				"stageId", stageID,
				"renew", renew,
				"err", err)
			http.Error(w, "stage lease operation failed", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, result, http.StatusOK)
}

func (s *ExternalServer) handleCancelPipelineExternal(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := parseRouteID(chi.URLParam(r, "pipelineId"))
	if err != nil {
		http.Error(w, "pipelineId must be a positive integer", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	appID, err := s.validateExternalAPIKey(ctx, r)
	if err != nil {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}

	pipeline, err := s.store.CancelPipelineForApplication(ctx, pipelineID, appID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPipelineNotCancellable):
			http.Error(w, "pipeline is already terminal and cannot be cancelled", http.StatusConflict)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			http.Error(w, "cancellation timed out", http.StatusGatewayTimeout)
		default:
			http.Error(w, "pipeline not found", http.StatusNotFound)
		}
		return
	}
	writeJSON(w, types.RedactPipelineResponse(pipeline), http.StatusOK)
}

// errBrokerUnavailable marks a stage result that could not reach the StageResult
// queue. The caller must retry, so it maps to 503 rather than 500.
var errBrokerUnavailable = errors.New("message broker is unavailable")

const (
	// maxStageResultBytes mirrors maxStageInputBytes: both land in stage_io.
	maxStageResultBytes = maxStageInputBytes
	// stageResultPublishTimeout bounds the confirm-aware publish so an
	// unreachable broker answers 503 instead of hanging on the router timeout.
	stageResultPublishTimeout = 5 * time.Second
)

const problemTypeBrokerUnavailable = "https://api.pipelogiq.dev/errors/broker-unavailable"

// handlePostStageResult accepts a stage result from an HTTP worker and forwards it
// to the StageResult queue. The worker process owns the state machine, so the API
// never calls Store.UpdateStageResult itself: fencing, retry policy evaluation,
// WebSocket updates and publisher wake-ups stay in one place for AMQP and HTTP
// workers alike.
func (s *ExternalServer) handlePostStageResult(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	statusCode := http.StatusInternalServerError
	defer func() {
		s.metrics.stageResults.WithLabelValues(strconv.Itoa(statusCode)).Inc()
		s.metrics.stageResultMs.Observe(float64(time.Since(started).Milliseconds()))
	}()

	stageID, err := parseRouteID(chi.URLParam(r, "stageId"))
	if err != nil {
		statusCode = http.StatusBadRequest
		writeProblemJSON(w, r, statusCode,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"stageId must be a positive integer",
			map[string][]string{"stageId": {"stageId must be greater than 0"}})
		return
	}

	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attribute.Int("stage.id", stageID))

	sessionToken := extractWorkerSessionToken(r)
	if strings.TrimSpace(sessionToken) == "" {
		statusCode = http.StatusUnauthorized
		writeProblemJSON(w, r, statusCode,
			"https://api.pipelogiq.dev/errors/unauthorized",
			"Unauthorized",
			"Worker session token is required",
			nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	appID, err := s.validateExternalAPIKey(ctx, r)
	if err != nil {
		statusCode = http.StatusUnauthorized
		writeProblemJSON(w, r, statusCode,
			"https://api.pipelogiq.dev/errors/unauthorized",
			"Unauthorized",
			"Invalid API key",
			nil)
		return
	}

	if err := s.store.ValidateWorkerSessionForApplication(ctx, appID, sessionToken); err != nil {
		if store.IsInvalidWorkerSessionError(err) {
			statusCode = http.StatusUnauthorized
			writeProblemJSON(w, r, statusCode,
				"https://api.pipelogiq.dev/errors/unauthorized", "Unauthorized", "Invalid worker session", nil)
		} else {
			statusCode = http.StatusInternalServerError
			s.logger.Error("validate stage result worker session failed", "err", err)
			writeProblemJSON(w, r, statusCode,
				"https://api.pipelogiq.dev/errors/internal", "Internal server error", "Failed to validate worker session", nil)
		}
		return
	}

	// Deliberately NOT a strict decode. This is a worker-protocol endpoint, and
	// the SDK's StageResultDto carries fields this message does not declare —
	// tracing metadata and the idempotency key among them. Rejecting unknown
	// fields would couple every SDK version to a server version, and would make
	// adding a field to a worker a breaking change for the control plane.
	var msg types.StageResultMessage
	if err = decodeJSONWithLimit(w, r, &msg, maxStageResultBytes+(64<<10)); err != nil {
		statusCode = http.StatusBadRequest
		writeProblemJSON(w, r, statusCode,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"Invalid request payload",
			map[string][]string{"body": {err.Error()}})
		return
	}

	if fieldErrors := validateStageResultSubmission(stageID, msg); len(fieldErrors) > 0 {
		statusCode = http.StatusBadRequest
		writeProblemJSON(w, r, statusCode,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"Stage result contains invalid fields",
			fieldErrors)
		return
	}

	stageAppID, err := s.store.StageApplicationID(ctx, stageID)
	if err != nil {
		if errors.Is(err, store.ErrStageNotFound) {
			statusCode = http.StatusNotFound
			writeProblemJSON(w, r, statusCode,
				"https://api.pipelogiq.dev/errors/not-found",
				"Not found",
				"Stage not found",
				nil)
			return
		}
		s.logger.Error("resolve stage application failed", "stageId", stageID, "err", err)
		statusCode = http.StatusInternalServerError
		writeProblemJSON(w, r, statusCode,
			"https://api.pipelogiq.dev/errors/internal",
			"Internal server error",
			"Failed to resolve stage",
			nil)
		return
	}
	if stageAppID != appID {
		statusCode = http.StatusForbidden
		writeProblemJSON(w, r, statusCode,
			"https://api.pipelogiq.dev/errors/forbidden",
			"Forbidden",
			"Stage belongs to another application",
			nil)
		return
	}

	body, err := json.Marshal(msg)
	if err != nil {
		s.logger.Error("marshal stage result failed", "stageId", stageID, "err", err)
		statusCode = http.StatusInternalServerError
		writeProblemJSON(w, r, statusCode,
			"https://api.pipelogiq.dev/errors/internal",
			"Internal server error",
			"Failed to encode stage result",
			nil)
		return
	}

	publishCtx, publishCancel := context.WithTimeout(ctx, stageResultPublishTimeout)
	defer publishCancel()
	if err = s.publishStageResult(publishCtx, body); err != nil {
		s.logger.Error("publish stage result failed",
			"stageId", stageID,
			"pipelineId", msg.PipelineID,
			"attempt", msg.Attempt,
			"err", err)
		statusCode = http.StatusServiceUnavailable
		writeProblemJSON(w, r, statusCode,
			problemTypeBrokerUnavailable,
			"Broker unavailable",
			"Stage result could not be queued; retry the request",
			nil)
		return
	}

	s.logger.Info("stage result accepted over http",
		"stageId", stageID,
		"pipelineId", msg.PipelineID,
		"attempt", msg.Attempt,
		"isSuccess", msg.IsSuccess,
		"applicationId", appID)

	statusCode = http.StatusAccepted
	writeJSON(w, types.StageResultAcceptedResponse{
		Accepted:    true,
		StageID:     stageID,
		ExecutionID: msg.ExecutionID,
	}, http.StatusAccepted)
}

// publishStageResultToQueue publishes onto the durable StageResult queue with
// persistent delivery and publisher confirms, the same path AMQP workers use.
func (s *ExternalServer) publishStageResultToQueue(ctx context.Context, body []byte) error {
	if s.mq == nil {
		return errBrokerUnavailable
	}
	return s.mq.PublishWithRetry(ctx, constants.StageResult, body, mq.QueueOptions{
		Durable:     true,
		DLQEnabled:  s.cfg.QueueDLQEnabled,
		DLQTTL:      s.cfg.QueueDLQMessageTTL,
		ContentType: "application/json",
	}, nil)
}

func validateStageResultSubmission(routeStageID int, msg types.StageResultMessage) map[string][]string {
	result := make(map[string][]string)

	if msg.StageID != routeStageID {
		addValidationError(result, "stageId", "stageId in the body must match the route")
	}
	if strings.TrimSpace(msg.ExecutionID) == "" {
		addValidationError(result, "executionId", "executionId is required")
	}
	if msg.Attempt <= 0 {
		addValidationError(result, "attempt", "attempt must be greater than 0")
	}
	if len(msg.Result) > maxStageResultBytes {
		addValidationError(result, "result", fmt.Sprintf("result must not exceed %d bytes", maxStageResultBytes))
	}
	if containsControlCharacters(msg.ErrorCode) {
		addValidationError(result, "errorCode", "errorCode must not contain control characters")
	}

	return result
}

func containsControlCharacters(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
