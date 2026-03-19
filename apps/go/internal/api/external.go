package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"pipelogiq/internal/config"
	"pipelogiq/internal/constants"
	"pipelogiq/internal/mq"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
	"pipelogiq/internal/version"
)

// ExternalServer serves the public API for SDK clients and workers.
// Routes are authenticated via API key (not JWT).
type ExternalServer struct {
	cfg    config.APIConfig
	store  *store.Store
	mq     *mq.Client
	logger *slog.Logger
	server *http.Server

	pendingMu sync.Mutex
	pending   map[string]pendingAck

	metrics externalMetrics
}

type pendingAck struct {
	ack     func() error
	nack    func(bool) error
	queue   string
	expires time.Time
}

type externalMetrics struct {
	pipelinesCreated prometheus.Counter
	stageJobsPulled  prometheus.Counter
	stageJobsAcked   prometheus.Counter
	stageJobsNacked  prometheus.Counter
	appendStages     *prometheus.CounterVec
	resumeStage      *prometheus.CounterVec
	appendDurationMs prometheus.Histogram
	resumeDurationMs prometheus.Histogram
}

func NewExternalServer(cfg config.APIConfig, st *store.Store, mqClient *mq.Client, logger *slog.Logger) *ExternalServer {
	metrics := externalMetrics{
		pipelinesCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ext_pipeline_created_total",
			Help: "Number of pipelines created via external API",
		}),
		stageJobsPulled: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ext_stage_jobs_pulled_total",
			Help: "Number of stage jobs pulled via external gateway",
		}),
		stageJobsAcked: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ext_stage_jobs_acked_total",
			Help: "Number of stage jobs acked via external gateway",
		}),
		stageJobsNacked: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ext_stage_jobs_nacked_total",
			Help: "Number of stage jobs nacked/requeued via external gateway",
		}),
		appendStages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "api_append_stages_total",
			Help: "Number of append stages endpoint requests",
		}, []string{"status_code"}),
		resumeStage: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "api_stage_resume_total",
			Help: "Number of stage resume endpoint requests",
		}, []string{"approved", "status_code"}),
		appendDurationMs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "api_append_stages_duration_ms",
			Help:    "Append stages endpoint latency in milliseconds",
			Buckets: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		}),
		resumeDurationMs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "api_stage_resume_duration_ms",
			Help:    "Stage resume endpoint latency in milliseconds",
			Buckets: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000},
		}),
	}
	registerCounter(&metrics.pipelinesCreated)
	registerCounter(&metrics.stageJobsPulled)
	registerCounter(&metrics.stageJobsAcked)
	registerCounter(&metrics.stageJobsNacked)
	registerCounterVec(&metrics.appendStages)
	registerCounterVec(&metrics.resumeStage)
	registerHistogram(&metrics.appendDurationMs)
	registerHistogram(&metrics.resumeDurationMs)

	return &ExternalServer{
		cfg:     cfg,
		store:   st,
		mq:      mqClient,
		logger:  logger,
		pending: make(map[string]pendingAck),
		metrics: metrics,
	}
}

func (s *ExternalServer) Run(ctx context.Context) error {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(60 * time.Second))
	router.Use(otelhttp.NewMiddleware("pipelogiq-api-external"))
	router.Use(newCORSMiddleware(allowedOriginsMap(s.cfg.AllowedOrigins)))

	// Health and version
	router.Get(s.cfg.HealthLivenessEndpoint, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	router.Get(s.cfg.HealthReadyEndpoint, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	router.Get("/version", version.HandleVersion)

	// External routes — no JWT, API key validated in handler
	router.Post("/pipelines", s.handleCreatePipeline)
	router.Post("/pipelines/{pipelineId}/stages", s.handleAppendPipelineStages)
	router.Post("/stages/{stageId}/resume", s.handleResumeStage)
	router.Post("/jobs/pull", s.handlePullJob)
	router.Post("/jobs/ack", s.handleAckJob)
	router.Post("/logs", s.handleSaveLog)
	router.Post("/workers/bootstrap", s.handleWorkerBootstrap)
	router.Post("/workers/heartbeat", s.handleWorkerHeartbeat)
	router.Post("/workers/events", s.handleWorkerEvents)
	router.Post("/workers/shutdown", s.handleWorkerShutdown)
	router.Get("/rabbitmq/connection", s.handleGetRabbitConnection)

	s.server = &http.Server{
		Addr:    s.cfg.ExternalHTTPAddr,
		Handler: router,
	}

	go s.cleanupExpired(ctx)

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("external api listening", "addr", s.cfg.ExternalHTTPAddr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// --- Handlers ---

func (s *ExternalServer) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var req types.PipelineCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if req.Name == "" || len(req.Stages) == 0 {
		http.Error(w, "name and stages are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	appID, err := s.store.ValidateAPIKey(ctx, req.ApiKey)
	if err != nil {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}

	pipeline, err := s.store.CreatePipeline(ctx, req, appID)
	if err != nil {
		s.logger.Error("create pipeline failed", "err", err)
		http.Error(w, "failed to create pipeline", http.StatusInternalServerError)
		return
	}

	s.metrics.pipelinesCreated.Inc()

	// Auto-fire event pipelines (single stage marked as event)
	if pipeline.IsEvent != nil && *pipeline.IsEvent && len(pipeline.Stages) == 1 {
		stage := pipeline.Stages[0]
		msg := types.StageNextMessage{
			AppID:            appID,
			PipelineID:       &pipeline.ID,
			StageID:          stage.ID,
			TraceID:          pipeline.TraceID,
			SpanID:           stage.SpanID,
			StageHandlerName: stage.StageHandlerName,
			Input:            deref(stage.Input),
			ContextItems:     pipeline.PipelineContext,
		}
		body, _ := json.Marshal(msg)
		opts := mq.QueueOptions{
			Durable:     true,
			DLQEnabled:  s.cfg.QueueDLQEnabled,
			DLQTTL:      s.cfg.QueueDLQMessageTTL,
			ContentType: "application/json",
		}
		queue := extStageQueueName(s.cfg.AppID, stage.StageHandlerName)
		if err := s.mq.PublishWithRetry(ctx, queue, body, opts, nil); err != nil {
			s.logger.Error("failed to publish event stage", "err", err, "queue", queue)
		}
	}

	writeJSON(w, pipeline, http.StatusOK)
}

func (s *ExternalServer) handleAppendPipelineStages(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	statusCode := http.StatusInternalServerError
	defer func() {
		s.metrics.appendStages.WithLabelValues(strconv.Itoa(statusCode)).Inc()
		s.metrics.appendDurationMs.Observe(float64(time.Since(started).Milliseconds()))
	}()

	pipelineID, err := parseRouteID(chi.URLParam(r, "pipelineId"))
	if err != nil {
		statusCode = http.StatusBadRequest
		writeProblemJSON(
			w,
			r,
			http.StatusBadRequest,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"pipelineId must be a positive integer",
			map[string][]string{"pipelineId": {"pipelineId must be greater than 0"}},
		)
		return
	}

	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attribute.Int("pipeline.id", pipelineID))

	var sdkReq appendStagesSDKRequest
	if err = decodeJSONStrict(r, &sdkReq); err != nil {
		statusCode = http.StatusBadRequest
		writeProblemJSON(
			w,
			r,
			http.StatusBadRequest,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"Invalid request payload",
			map[string][]string{"body": {err.Error()}},
		)
		return
	}

	req, err := mapAppendStagesSDKRequest(sdkReq)
	if err != nil {
		statusCode = http.StatusBadRequest
		writeProblemJSON(
			w,
			r,
			http.StatusBadRequest,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"Invalid append stages payload",
			map[string][]string{"body": {err.Error()}},
		)
		return
	}

	validationErrors := validateAppendStagesRequest(req)
	if len(validationErrors) > 0 {
		statusCode = http.StatusBadRequest
		writeProblemJSON(
			w,
			r,
			http.StatusBadRequest,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"Append stages request contains invalid fields",
			validationErrors,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	appID, err := s.validateExternalAPIKey(ctx, r)
	if err != nil {
		statusCode = http.StatusUnauthorized
		writeProblemJSON(
			w,
			r,
			http.StatusUnauthorized,
			"https://api.pipelogiq.dev/errors/unauthorized",
			"Unauthorized",
			"Invalid API key",
			nil,
		)
		return
	}

	result, err := s.store.AppendStages(ctx, pipelineID, req, fmt.Sprintf("application:%d", appID))
	if err != nil {
		switch {
		case store.IsPipelineNotFoundError(err):
			statusCode = http.StatusNotFound
			writeProblemJSON(
				w,
				r,
				http.StatusNotFound,
				"https://api.pipelogiq.dev/errors/not-found",
				"Not found",
				"Pipeline not found",
				nil,
			)
			return
		case store.IsPipelineAppendNotAllowedError(err):
			statusCode = http.StatusConflict
			writeProblemJSON(
				w,
				r,
				http.StatusConflict,
				"https://api.pipelogiq.dev/errors/conflict",
				"Conflict",
				"Pipeline is in terminal state and append is not allowed",
				nil,
			)
			return
		default:
			s.logger.Error("append stages failed", "pipelineId", pipelineID, "err", err)
			statusCode = http.StatusInternalServerError
			writeProblemJSON(
				w,
				r,
				http.StatusInternalServerError,
				"https://api.pipelogiq.dev/errors/internal",
				"Internal server error",
				"Failed to append stages",
				nil,
			)
			return
		}
	}

	s.logger.Info("append stages succeeded", "pipelineId", pipelineID, "count", len(req.Stages), "actor", fmt.Sprintf("application:%d", appID))
	statusCode = http.StatusOK
	writeJSON(w, result, http.StatusOK)
}

func (s *ExternalServer) handleResumeStage(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	statusCode := http.StatusInternalServerError
	approvedLabel := "unknown"
	defer func() {
		s.metrics.resumeStage.WithLabelValues(approvedLabel, strconv.Itoa(statusCode)).Inc()
		s.metrics.resumeDurationMs.Observe(float64(time.Since(started).Milliseconds()))
	}()

	stageID, err := parseRouteID(chi.URLParam(r, "stageId"))
	if err != nil {
		statusCode = http.StatusBadRequest
		writeProblemJSON(
			w,
			r,
			http.StatusBadRequest,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"stageId must be a positive integer",
			map[string][]string{"stageId": {"stageId must be greater than 0"}},
		)
		return
	}

	span := trace.SpanFromContext(r.Context())
	span.SetAttributes(attribute.Int("stage.id", stageID))

	var req types.ResumeStageRequest
	if err = decodeJSONStrict(r, &req); err != nil {
		statusCode = http.StatusBadRequest
		writeProblemJSON(
			w,
			r,
			http.StatusBadRequest,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"Invalid request payload",
			map[string][]string{"body": {err.Error()}},
		)
		return
	}
	approvedLabel = strconv.FormatBool(req.Approved)

	validationErrors := validateResumeStageRequest(req)
	if len(validationErrors) > 0 {
		statusCode = http.StatusBadRequest
		writeProblemJSON(
			w,
			r,
			http.StatusBadRequest,
			"https://api.pipelogiq.dev/errors/validation",
			"Validation failed",
			"Resume stage request contains invalid fields",
			validationErrors,
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	appID, err := s.validateExternalAPIKey(ctx, r)
	if err != nil {
		statusCode = http.StatusUnauthorized
		writeProblemJSON(
			w,
			r,
			http.StatusUnauthorized,
			"https://api.pipelogiq.dev/errors/unauthorized",
			"Unauthorized",
			"Invalid API key",
			nil,
		)
		return
	}

	if err = s.store.ResumeStageApproval(ctx, stageID, req, fmt.Sprintf("application:%d", appID)); err != nil {
		switch {
		case store.IsStageNotFoundError(err):
			statusCode = http.StatusNotFound
			writeProblemJSON(
				w,
				r,
				http.StatusNotFound,
				"https://api.pipelogiq.dev/errors/not-found",
				"Not found",
				"Stage not found",
				nil,
			)
			return
		case store.IsStageNotWaitingApprovalError(err), store.IsStageResumeConflictError(err):
			statusCode = http.StatusConflict
			writeProblemJSON(
				w,
				r,
				http.StatusConflict,
				"https://api.pipelogiq.dev/errors/conflict",
				"Conflict",
				"Stage is not in waiting-for-approval state or decision conflicts with previous resume",
				nil,
			)
			return
		default:
			s.logger.Error("resume stage failed", "stageId", stageID, "approved", req.Approved, "err", err)
			statusCode = http.StatusInternalServerError
			writeProblemJSON(
				w,
				r,
				http.StatusInternalServerError,
				"https://api.pipelogiq.dev/errors/internal",
				"Internal server error",
				"Failed to resume stage",
				nil,
			)
			return
		}
	}

	s.logger.Info("resume stage succeeded", "stageId", stageID, "approved", req.Approved, "actor", fmt.Sprintf("application:%d", appID))
	statusCode = http.StatusNoContent
	w.WriteHeader(http.StatusNoContent)
}

type pullRequest struct {
	Queue string `json:"queue"`
}

type pullResponse struct {
	Token     string          `json:"token"`
	Queue     string          `json:"queue"`
	MessageID string          `json:"messageId,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	Headers   amqp.Table      `json:"headers,omitempty"`
}

func (s *ExternalServer) handlePullJob(w http.ResponseWriter, r *http.Request) {
	var req pullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Queue) == "" {
		http.Error(w, "queue is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	opts := mq.QueueOptions{
		Durable:    true,
		DLQEnabled: s.cfg.QueueDLQEnabled,
		DLQTTL:     s.cfg.QueueDLQMessageTTL,
		Prefetch:   1,
	}

	msg, err := s.mq.Get(ctx, req.Queue, opts)
	if err != nil {
		s.logger.Error("pull job failed", "err", err, "queue", req.Queue)
		http.Error(w, "failed to pull", http.StatusInternalServerError)
		return
	}
	if msg == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	token := uuid.NewString()
	s.pendingMu.Lock()
	if len(s.pending) >= s.cfg.GatewayMaxInFlight {
		s.pendingMu.Unlock()
		_ = msg.Nack(true)
		http.Error(w, "too many in-flight messages, try again", http.StatusTooManyRequests)
		return
	}
	s.pending[token] = pendingAck{
		ack:     msg.Ack,
		nack:    msg.Nack,
		queue:   req.Queue,
		expires: time.Now().Add(s.cfg.GatewayVisibilityTTL),
	}
	s.pendingMu.Unlock()

	s.metrics.stageJobsPulled.Inc()
	writeJSON(w, pullResponse{
		Token:     token,
		Queue:     req.Queue,
		MessageID: msg.MessageID,
		Payload:   json.RawMessage(msg.Body),
		Headers:   msg.Headers,
	}, http.StatusOK)
}

type ackRequest struct {
	Token   string `json:"token"`
	Requeue bool   `json:"requeue"`
}

func (s *ExternalServer) handleAckJob(w http.ResponseWriter, r *http.Request) {
	var req ackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	s.pendingMu.Lock()
	msg, ok := s.pending[req.Token]
	if ok {
		delete(s.pending, req.Token)
	}
	s.pendingMu.Unlock()

	if !ok {
		http.Error(w, "token not found", http.StatusNotFound)
		return
	}

	var err error
	if req.Requeue {
		err = msg.nack(true)
		s.metrics.stageJobsNacked.Inc()
	} else {
		err = msg.ack()
		s.metrics.stageJobsAcked.Inc()
	}

	if err != nil {
		http.Error(w, "ack failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"}, http.StatusOK)
}

func (s *ExternalServer) handleSaveLog(w http.ResponseWriter, r *http.Request) {
	var req types.LogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	log, err := s.store.SaveLog(ctx, req)
	if err != nil {
		s.logger.Error("save log failed", "err", err)
		http.Error(w, "failed to save log", http.StatusInternalServerError)
		return
	}

	writeJSON(w, log, http.StatusOK)
}

func (s *ExternalServer) handleGetRabbitConnection(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	apiKey := extractAPIKey(r)
	if strings.TrimSpace(apiKey) == "" {
		http.Error(w, "api key is required", http.StatusUnauthorized)
		return
	}

	if _, err := s.store.ValidateAPIKey(ctx, apiKey); err != nil {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}

	if strings.TrimSpace(s.cfg.RabbitURL) == "" {
		http.Error(w, "rabbit connection is not configured", http.StatusServiceUnavailable)
		return
	}

	writeJSON(w, types.RabbitConnectionResponse{
		ConnectionString: s.cfg.RabbitURL,
	}, http.StatusOK)
}

func (s *ExternalServer) handleWorkerBootstrap(w http.ResponseWriter, r *http.Request) {
	var req types.WorkerBootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.WorkerName) == "" {
		http.Error(w, "workerName is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	apiKey := extractAPIKey(r)
	if strings.TrimSpace(apiKey) == "" {
		http.Error(w, "api key is required", http.StatusUnauthorized)
		return
	}

	appID, err := s.store.ValidateAPIKey(ctx, apiKey)
	if err != nil {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
		return
	}

	if strings.TrimSpace(s.cfg.RabbitURL) == "" {
		http.Error(w, "rabbit connection is not configured", http.StatusServiceUnavailable)
		return
	}

	appName, err := s.store.GetApplicationNameByID(ctx, appID)
	if err != nil {
		s.logger.Error("load application for bootstrap failed", "err", err, "applicationId", appID)
		http.Error(w, "failed to resolve application", http.StatusInternalServerError)
		return
	}

	sessionToken := uuid.NewString() + "." + uuid.NewString()
	sessionExpiresAt := time.Now().UTC().Add(s.cfg.WorkerSessionTTL)
	workerID, err := s.store.RegisterWorkerSession(
		ctx,
		appID,
		s.cfg.AppID,
		"rabbitmq",
		req,
		sessionToken,
		sessionExpiresAt,
	)
	if err != nil {
		s.logger.Error("register worker session failed", "err", err, "applicationId", appID)
		http.Error(w, "failed to register worker", http.StatusInternalServerError)
		return
	}

	traceTemplate := ""
	logsTemplate := ""
	if trace, logs, err := s.store.GetObservabilityLinkTemplates(ctx); err == nil {
		traceTemplate = trace
		logsTemplate = logs
	} else {
		s.logger.Warn("load observability templates failed for bootstrap", "err", err)
	}

	response := types.WorkerBootstrapResponse{
		WorkerID:           workerID,
		WorkerSessionToken: sessionToken,
		ConfigVersion:      time.Now().UTC().Format(time.RFC3339),
		Application: types.WorkerApplicationInfo{
			ApplicationID:   appID,
			ApplicationName: appName,
			AppID:           s.cfg.AppID,
		},
		MessageBroker: types.WorkerBrokerInfo{
			Type:              "rabbitmq",
			ConnectionString:  s.cfg.RabbitURL,
			Prefetch:          s.cfg.QueuePrefetch,
			TopologyOwnership: s.cfg.QueueTopologyOwnership,
			DLQEnabled:        s.cfg.QueueDLQEnabled,
			DLQTTLSec:         int64(s.cfg.QueueDLQMessageTTL.Seconds()),
		},
		Queues: types.WorkerQueueTopology{
			StageResult:        constants.StageResult,
			StageSetStatus:     constants.StageSetStatus,
			StageUpdatedFanout: constants.StageUpdated + ".fanout",
			StageNextPattern:   "{appId}_{handler}_" + constants.StageNext,
		},
		Heartbeat: types.WorkerHeartbeatContract{
			IntervalSec:     int64(s.cfg.WorkerHeartbeatInterval.Seconds()),
			OfflineAfterSec: int64(s.cfg.WorkerOfflineAfter.Seconds()),
		},
		Observability: types.WorkerObservabilityInfo{
			TraceLinkTemplate: traceTemplate,
			LogsLinkTemplate:  logsTemplate,
		},
	}

	writeJSON(w, response, http.StatusOK)
}

func (s *ExternalServer) handleWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req types.WorkerHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.WorkerID) == "" {
		http.Error(w, "workerId is required", http.StatusBadRequest)
		return
	}

	sessionToken := extractWorkerSessionToken(r)
	if strings.TrimSpace(sessionToken) == "" {
		http.Error(w, "worker session token is required", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := s.store.UpdateWorkerHeartbeat(ctx, sessionToken, req); err != nil {
		if store.IsInvalidWorkerSessionError(err) {
			http.Error(w, "invalid worker session", http.StatusUnauthorized)
			return
		}
		s.logger.Error("worker heartbeat failed", "err", err, "workerId", req.WorkerID)
		http.Error(w, "failed to persist heartbeat", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"status":   "ok",
		"workerId": req.WorkerID,
	}, http.StatusOK)
}

func (s *ExternalServer) handleWorkerEvents(w http.ResponseWriter, r *http.Request) {
	var req types.WorkerEventsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.WorkerID) == "" {
		http.Error(w, "workerId is required", http.StatusBadRequest)
		return
	}
	if len(req.Events) == 0 {
		http.Error(w, "events are required", http.StatusBadRequest)
		return
	}
	if len(req.Events) > s.cfg.WorkerEventsMaxBatch {
		http.Error(w, "too many events in one batch", http.StatusBadRequest)
		return
	}

	sessionToken := extractWorkerSessionToken(r)
	if strings.TrimSpace(sessionToken) == "" {
		http.Error(w, "worker session token is required", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := s.store.SaveWorkerEvents(ctx, req.WorkerID, sessionToken, req.Events); err != nil {
		if store.IsInvalidWorkerSessionError(err) {
			http.Error(w, "invalid worker session", http.StatusUnauthorized)
			return
		}
		s.logger.Error("save worker events failed", "err", err, "workerId", req.WorkerID, "count", len(req.Events))
		http.Error(w, "failed to save worker events", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"status":        "ok",
		"acceptedCount": len(req.Events),
		"workerId":      req.WorkerID,
	}, http.StatusOK)
}

func (s *ExternalServer) handleWorkerShutdown(w http.ResponseWriter, r *http.Request) {
	var req types.WorkerShutdownRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.WorkerID) == "" {
		http.Error(w, "workerId is required", http.StatusBadRequest)
		return
	}

	sessionToken := extractWorkerSessionToken(r)
	if strings.TrimSpace(sessionToken) == "" {
		http.Error(w, "worker session token is required", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := s.store.StopWorkerSession(ctx, req.WorkerID, sessionToken, req.Reason); err != nil {
		if store.IsInvalidWorkerSessionError(err) {
			http.Error(w, "invalid worker session", http.StatusUnauthorized)
			return
		}
		s.logger.Error("worker shutdown update failed", "err", err, "workerId", req.WorkerID)
		http.Error(w, "failed to stop worker session", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"status":   "ok",
		"workerId": req.WorkerID,
	}, http.StatusOK)
}

func (s *ExternalServer) cleanupExpired(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			s.pendingMu.Lock()
			for token, msg := range s.pending {
				if now.After(msg.expires) {
					_ = msg.nack(true)
					delete(s.pending, token)
				}
			}
			s.pendingMu.Unlock()
		}
	}
}

func extStageQueueName(appID, handler string) string {
	return fmt.Sprintf("%s_%s_%s", appID, handler, constants.StageNext)
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func extractAPIKey(r *http.Request) string {
	if bearer := strings.TrimSpace(r.Header.Get("Authorization")); bearer != "" {
		const prefix = "bearer "
		if len(bearer) > len(prefix) && strings.EqualFold(bearer[:len(prefix)], prefix) {
			return strings.TrimSpace(bearer[len(prefix):])
		}
	}

	if apiKey := strings.TrimSpace(r.Header.Get("X-API-Key")); apiKey != "" {
		return apiKey
	}

	return strings.TrimSpace(r.URL.Query().Get("apiKey"))
}

func extractWorkerSessionToken(r *http.Request) string {
	if token := strings.TrimSpace(r.Header.Get("X-Worker-Session")); token != "" {
		return token
	}
	if token := strings.TrimSpace(r.Header.Get("X-Worker-Token")); token != "" {
		return token
	}
	if bearer := strings.TrimSpace(r.Header.Get("Authorization")); bearer != "" {
		const prefix = "bearer "
		if len(bearer) > len(prefix) && strings.EqualFold(bearer[:len(prefix)], prefix) {
			return strings.TrimSpace(bearer[len(prefix):])
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("workerSessionToken"))
}

type problemDetails struct {
	Type   string              `json:"type"`
	Title  string              `json:"title"`
	Status int                 `json:"status"`
	Detail string              `json:"detail,omitempty"`
	Trace  string              `json:"traceId,omitempty"`
	Errors map[string][]string `json:"errors,omitempty"`
}

type appendStagesSDKRequest struct {
	Stages []appendStageSDK `json:"stages"`
}

type appendStageSDK struct {
	StageID                *int                `json:"stageId,omitempty"`
	PipelineID             *int                `json:"pipelineId,omitempty"`
	StageName              string              `json:"stageName"`
	StageHandlerName       string              `json:"stageHandlerName"`
	Description            string              `json:"description,omitempty"`
	Input                  json.RawMessage     `json:"input,omitempty"`
	Options                *types.StageOptions `json:"options,omitempty"`
	IsEvent                *bool               `json:"isEvent,omitempty"`
	RunNextIfFailed        *bool               `json:"runNextIfFailed,omitempty"`
	RunNextIfCurrentFailed *bool               `json:"runNextIfCurrentFailed,omitempty"`
}

func decodeJSONStrict(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeProblemJSON(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	typeURL string,
	title string,
	detail string,
	fieldErrors map[string][]string,
) {
	traceID := middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problemDetails{
		Type:   typeURL,
		Title:  title,
		Status: status,
		Detail: detail,
		Trace:  traceID,
		Errors: fieldErrors,
	})
}

func parseRouteID(raw string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || id <= 0 {
		return 0, errors.New("invalid route id")
	}
	return id, nil
}

func validateAppendStagesRequest(req types.AppendStagesRequest) map[string][]string {
	result := make(map[string][]string)
	if len(req.Stages) == 0 {
		result["stages"] = []string{"at least one stage is required"}
		return result
	}

	for idx, stage := range req.Stages {
		prefix := fmt.Sprintf("stages[%d]", idx)
		if strings.TrimSpace(stage.Name) == "" {
			result[prefix+".stageName"] = []string{"stageName is required"}
		}
		if strings.TrimSpace(stage.StageHandler) == "" {
			result[prefix+".stageHandlerName"] = []string{"stageHandlerName is required"}
		}
		if stage.Options == nil {
			continue
		}
		if stage.Options.RetryInterval != nil && *stage.Options.RetryInterval < 0 {
			result[prefix+".options.retryInterval"] = []string{"retryInterval must be >= 0"}
		}
		if stage.Options.MaxRetries != nil && *stage.Options.MaxRetries < 0 {
			result[prefix+".options.maxRetries"] = []string{"maxRetries must be >= 0"}
		}
		if stage.Options.TimeOut != nil && *stage.Options.TimeOut <= 0 {
			result[prefix+".options.timeOut"] = []string{"timeOut must be > 0"}
		}
	}

	return result
}

func mapAppendStagesSDKRequest(raw appendStagesSDKRequest) (types.AppendStagesRequest, error) {
	mapped := types.AppendStagesRequest{
		Stages: make([]types.StageCreate, 0, len(raw.Stages)),
	}

	for _, stage := range raw.Stages {
		runNextIfFailed := false
		if stage.RunNextIfFailed != nil {
			runNextIfFailed = *stage.RunNextIfFailed
		}
		if stage.RunNextIfCurrentFailed != nil {
			runNextIfFailed = *stage.RunNextIfCurrentFailed
		}

		isEvent := false
		if stage.IsEvent != nil {
			isEvent = *stage.IsEvent
		}

		input, err := normalizeStageInput(stage.Input)
		if err != nil {
			return types.AppendStagesRequest{}, err
		}

		// StageID/PipelineID are accepted for SDK compatibility but intentionally ignored.
		mapped.Stages = append(mapped.Stages, types.StageCreate{
			Name:            stage.StageName,
			StageHandler:    stage.StageHandlerName,
			Description:     stage.Description,
			Input:           input,
			Options:         stage.Options,
			IsEvent:         isEvent,
			RunNextIfFailed: runNextIfFailed,
		})
	}

	return mapped, nil
}

func normalizeStageInput(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return "", nil
	}

	if strings.HasPrefix(trimmed, "\"") {
		var value string
		if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
			return "", err
		}
		return value, nil
	}

	return trimmed, nil
}

func validateResumeStageRequest(req types.ResumeStageRequest) map[string][]string {
	result := make(map[string][]string)
	if !req.Approved {
		if req.RejectionReason == nil || strings.TrimSpace(*req.RejectionReason) == "" {
			result["rejectionReason"] = []string{"rejectionReason is required when approved=false"}
		}
	}
	return result
}

func (s *ExternalServer) validateExternalAPIKey(ctx context.Context, r *http.Request) (int, error) {
	apiKey := extractAPIKey(r)
	if strings.TrimSpace(apiKey) == "" {
		return 0, errors.New("api key is required")
	}
	return s.store.ValidateAPIKey(ctx, apiKey)
}

func registerCounter(counter *prometheus.Counter) {
	if counter == nil || *counter == nil {
		return
	}
	if err := prometheus.Register(*counter); err != nil {
		if regErr, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, okCast := regErr.ExistingCollector.(prometheus.Counter); okCast {
				*counter = existing
			}
		}
	}
}

func registerCounterVec(counterVec **prometheus.CounterVec) {
	if counterVec == nil || *counterVec == nil {
		return
	}
	if err := prometheus.Register(*counterVec); err != nil {
		if regErr, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, okCast := regErr.ExistingCollector.(*prometheus.CounterVec); okCast {
				*counterVec = existing
			}
		}
	}
}

func registerHistogram(histogram *prometheus.Histogram) {
	if histogram == nil || *histogram == nil {
		return
	}
	if err := prometheus.Register(*histogram); err != nil {
		if regErr, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, okCast := regErr.ExistingCollector.(prometheus.Histogram); okCast {
				*histogram = existing
			}
		}
	}
}
