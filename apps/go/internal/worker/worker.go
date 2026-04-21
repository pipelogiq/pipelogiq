package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"pipelogiq/internal/config"
	"pipelogiq/internal/constants"
	"pipelogiq/internal/mq"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

var workerTracer = otel.Tracer("pipelogiq/worker")

type Worker struct {
	cfg    config.WorkerConfig
	store  *store.Store
	mq     *mq.Client
	logger *slog.Logger

	wake    chan struct{}
	metrics workerMetrics
}

type workerMetrics struct {
	stagePublished                prometheus.Counter
	stageResultProcessed          prometheus.Counter
	stageResultFailed             prometheus.Counter
	stageStatusUpdated            prometheus.Counter
	activeStageTimedOut           prometheus.Counter
	pendingMarkedFailedDeprecated prometheus.Counter
	stageDuration                 *prometheus.HistogramVec
}

const (
	defaultWorkerPollInterval      = 200 * time.Millisecond
	minWorkerPollInterval          = 50 * time.Millisecond
	defaultStageActiveTimeout      = 5 * time.Minute
	minStageTimeoutWatcherInterval = time.Second
	maxPublishBatch                = 10
)

func New(cfg config.WorkerConfig, st *store.Store, mqClient *mq.Client, logger *slog.Logger) *Worker {
	cfg = normalizeWorkerConfig(cfg, logger)

	metrics := workerMetrics{
		stagePublished: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "stage_published_total",
			Help: "Number of stages published to StageNext",
		}),
		stageResultProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "stage_result_processed_total",
			Help: "Number of stage result messages processed successfully",
		}),
		stageResultFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "stage_result_failed_total",
			Help: "Number of failed stage result handling attempts",
		}),
		stageStatusUpdated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "stage_status_updated_total",
			Help: "Number of stage status set messages processed",
		}),
		activeStageTimedOut: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "stage_active_timed_out_total",
			Help: "Number of active stages marked as failed due to timeout",
		}),
		pendingMarkedFailedDeprecated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pending_marked_failed_total",
			Help: "Deprecated alias for stage_active_timed_out_total",
		}),
		stageDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "stage_duration_seconds",
			Help:    "Stage execution duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}, []string{"handler", "status"}),
	}
	prometheus.MustRegister(
		metrics.stagePublished,
		metrics.stageResultProcessed,
		metrics.stageResultFailed,
		metrics.stageStatusUpdated,
		metrics.activeStageTimedOut,
		metrics.pendingMarkedFailedDeprecated,
		metrics.stageDuration,
	)

	return &Worker{
		cfg:     cfg,
		store:   st,
		mq:      mqClient,
		logger:  logger,
		wake:    make(chan struct{}, 1),
		metrics: metrics,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	startWorker := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.withRecover(ctx, name, fn)
		}()
	}

	startWorker("publisher", w.runPublisher)
	startWorker("stage-result-consumer", w.runStageResultConsumer)
	startWorker("stage-status-consumer", w.runStageStatusConsumer)
	startWorker("stage-timeout-watcher", w.runStageTimeoutWatcher)
	startWorker("orphan-recovery", w.runOrphanRecovery)

	if w.cfg.MetricsAddr != "" {
		go w.runMetricsServer(ctx)
	}

	<-ctx.Done()
	w.logger.Info("worker shutting down, waiting for goroutines to drain")
	wg.Wait()
	w.logger.Info("worker shutdown complete")
	return ctx.Err()
}

const workerRestartDelay = 10 * time.Second

// withRecover runs fn in a loop, recovering from panics and non-context errors.
// It only stops when ctx is cancelled.
func (w *Worker) withRecover(ctx context.Context, name string, fn func(context.Context) error) {
	for {
		if ctx.Err() != nil {
			return
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("worker goroutine panicked, will restart",
						"goroutine", name, "panic", r)
				}
			}()
			if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.Error("worker goroutine exited with error, will restart",
					"goroutine", name, "err", err)
			}
		}()

		if ctx.Err() != nil {
			return
		}

		w.logger.Warn("worker goroutine restarting", "goroutine", name, "delay", workerRestartDelay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(workerRestartDelay):
		}
	}
}

func (w *Worker) runMetricsServer(ctx context.Context) {
	srv := &http.Server{
		Addr:    w.cfg.MetricsAddr,
		Handler: promhttp.Handler(),
	}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	w.logger.Info("metrics server listening", "addr", w.cfg.MetricsAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		w.logger.Error("metrics server error", "err", err)
	}
}

func (w *Worker) runOrphanRecovery(ctx context.Context) error {
	recoveryThreshold := w.cfg.StageActiveTimeout / 2
	if recoveryThreshold < 30*time.Second {
		recoveryThreshold = 30 * time.Second
	}

	recover := func() {
		recovered, err := w.store.RecoverOrphanedStages(ctx, recoveryThreshold)
		if err != nil {
			if ctx.Err() == nil {
				w.logger.Error("orphan recovery failed", "err", err)
			}
			return
		}
		if recovered > 0 {
			w.logger.Warn("recovered orphaned stages", "count", recovered, "threshold", recoveryThreshold)
		}
	}

	recover()

	ticker := time.NewTicker(recoveryThreshold)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			recover()
		}
	}
}

func (w *Worker) runPublisher(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		dispatched := 0
		for dispatched < maxPublishBatch {
			stage, err := w.store.GetStageToExecute(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.logger.Error("get stage to execute failed", "err", err)
				break
			}
			if stage == nil {
				break
			}

			_, span := workerTracer.Start(ctx, "worker.dispatch_stage",
				trace.WithSpanKind(trace.SpanKindInternal),
				trace.WithAttributes(
					attribute.Int("pipelogiq.stage.id", stage.StageID),
					attribute.String("pipelogiq.stage.handler", stage.StageHandlerName),
				),
			)
			if stage.PipelineID != nil {
				span.SetAttributes(attribute.Int("pipelogiq.pipeline.id", *stage.PipelineID))
			}

			queue := constants.StageNextQueueName(constants.ApplicationQueueID(stage.AppID), stage.StageHandlerName)
			body, _ := json.Marshal(stage)
			opts := mq.QueueOptions{
				Durable:     true,
				DLQEnabled:  w.cfg.QueueDLQEnabled,
				DLQTTL:      w.cfg.QueueDLQMessageTTL,
				ContentType: "application/json",
			}

			if err := w.mq.PublishWithRetry(ctx, queue, body, opts, nil); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "publish failed")
				span.End()
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.logger.Error("publish stage next failed", "queue", queue, "err", err)
				break
			}

			span.SetStatus(codes.Ok, "dispatched")
			span.End()

			if stage.PipelineID != nil {
				pipeline, err := w.store.GetPipelineWithStages(ctx, *stage.PipelineID)
				if err != nil {
					w.logger.Error("load pipeline snapshot for ws update failed", "pipelineId", *stage.PipelineID, "err", err)
				} else {
					w.publishPipelineUpdate(ctx, pipeline)
				}
			}

			w.metrics.stagePublished.Inc()
			w.logger.Info("published stage", "queue", queue, "stageId", stage.StageID, "pipelineId", stage.PipelineID)
			dispatched++
		}

		if dispatched > 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.wake:
		case <-time.After(w.cfg.PollInterval):
		}
	}
}

func (w *Worker) triggerPublisher() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) runStageResultConsumer(ctx context.Context) error {
	opts := mq.ConsumeOptions{
		QueueOptions: mq.QueueOptions{
			Durable:     true,
			DLQEnabled:  w.cfg.QueueDLQEnabled,
			DLQTTL:      w.cfg.QueueDLQMessageTTL,
			Prefetch:    w.cfg.Prefetch,
			ContentType: "application/json",
		},
		HandlerTimeout:   30 * time.Second,
		DeadLetterOnFail: true,
	}

	handler := func(ctx context.Context, d amqp.Delivery) error {
		var msg types.StageResultMessage
		if err := json.Unmarshal(d.Body, &msg); err != nil {
			return err
		}

		_, span := workerTracer.Start(ctx, "worker.process_stage_result",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(
				attribute.Int("pipelogiq.stage.id", msg.StageID),
				attribute.Bool("pipelogiq.stage.success", msg.IsSuccess),
			),
		)
		if msg.PipelineID != nil {
			span.SetAttributes(attribute.Int("pipelogiq.pipeline.id", *msg.PipelineID))
		}

		pipeline, err := w.store.UpdateStageResult(ctx, msg)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "update result failed")
			span.End()
			w.metrics.stageResultFailed.Inc()
			return err
		}

		if pipeline != nil {
			for _, s := range pipeline.Stages {
				if s.ID == msg.StageID && s.StartedAt != nil && s.FinishedAt != nil {
					dur := s.FinishedAt.Sub(*s.StartedAt).Seconds()
					status := "completed"
					if !msg.IsSuccess {
						status = "failed"
					}
					w.metrics.stageDuration.WithLabelValues(s.StageHandlerName, status).Observe(dur)
					span.SetAttributes(
						attribute.Float64("pipelogiq.stage.duration_s", dur),
						attribute.String("pipelogiq.stage.handler", s.StageHandlerName),
					)
					break
				}
			}
		}

		span.SetStatus(codes.Ok, "processed")
		span.End()

		w.publishPipelineUpdate(ctx, pipeline)
		w.metrics.stageResultProcessed.Inc()
		w.triggerPublisher()
		return nil
	}

	w.logger.Info("starting StageResult consumer")
	return w.mq.Consume(ctx, constants.StageResult, opts, handler)
}

func (w *Worker) runStageStatusConsumer(ctx context.Context) error {
	opts := mq.ConsumeOptions{
		QueueOptions: mq.QueueOptions{
			Durable:     true,
			DLQEnabled:  w.cfg.QueueDLQEnabled,
			DLQTTL:      w.cfg.QueueDLQMessageTTL,
			Prefetch:    w.cfg.Prefetch,
			ContentType: "application/json",
		},
		HandlerTimeout:   15 * time.Second,
		DeadLetterOnFail: true,
	}

	handler := func(ctx context.Context, d amqp.Delivery) error {
		var msg types.SetStageStatusMessage
		if err := json.Unmarshal(d.Body, &msg); err != nil {
			return err
		}

		_, span := workerTracer.Start(ctx, "worker.update_stage_status",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(
				attribute.Int("pipelogiq.stage.id", msg.StageID),
				attribute.String("pipelogiq.stage.status", msg.Status),
			),
		)

		pipeline, err := w.store.UpdateStageStatus(ctx, msg)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "update status failed")
			span.End()
			return err
		}

		span.SetStatus(codes.Ok, "updated")
		span.End()

		w.publishPipelineUpdate(ctx, pipeline)
		w.metrics.stageStatusUpdated.Inc()
		w.triggerPublisher()
		return nil
	}

	w.logger.Info("starting StageSetStatus consumer")
	return w.mq.Consume(ctx, constants.StageSetStatus, opts, handler)
}

func (w *Worker) runStageTimeoutWatcher(ctx context.Context) error {
	ticker := time.NewTicker(stageTimeoutWatchInterval(w.cfg.StageActiveTimeout))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			affected, err := w.store.MarkActiveTooLong(ctx, w.cfg.StageActiveTimeout)
			if err != nil {
				w.logger.Error("mark active stages timed out failed", "err", err)
				continue
			}
			if affected > 0 {
				w.metrics.activeStageTimedOut.Add(float64(affected))
				w.metrics.pendingMarkedFailedDeprecated.Add(float64(affected))
				w.logger.Warn("marked timed-out active stages as failed", "count", affected)
			}
		}
	}
}

func normalizeWorkerConfig(cfg config.WorkerConfig, logger *slog.Logger) config.WorkerConfig {
	if cfg.PollInterval <= 0 {
		if logger != nil {
			logger.Warn("worker poll interval is invalid, using default", "configured", cfg.PollInterval, "effective", defaultWorkerPollInterval)
		}
		cfg.PollInterval = defaultWorkerPollInterval
	} else if cfg.PollInterval < minWorkerPollInterval {
		if logger != nil {
			logger.Warn("worker poll interval is too low, clamping", "configured", cfg.PollInterval, "effective", minWorkerPollInterval)
		}
		cfg.PollInterval = minWorkerPollInterval
	}

	if cfg.StageActiveTimeout <= 0 {
		if logger != nil {
			logger.Warn("stage active timeout is invalid, using default", "configured", cfg.StageActiveTimeout, "effective", defaultStageActiveTimeout)
		}
		cfg.StageActiveTimeout = defaultStageActiveTimeout
	}

	return cfg
}

func stageTimeoutWatchInterval(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		timeout = defaultStageActiveTimeout
	}

	interval := timeout / 2
	if interval < minStageTimeoutWatcherInterval {
		return minStageTimeoutWatcherInterval
	}

	return interval
}

func (w *Worker) publishPipelineUpdate(ctx context.Context, pipeline *types.PipelineResponse) {
	if pipeline == nil {
		return
	}

	payload, err := json.Marshal(pipeline)
	if err != nil {
		w.logger.Error("marshal stage updated payload failed", "pipelineId", pipeline.ID, "err", err)
		return
	}

	// Stage updates are consumed via the fanout exchange by the API/WebSocket
	// broadcaster. Publishing the same payload into the legacy durable queue
	// only accumulates unread backlog when no direct consumer is attached.
	if err := w.mq.PublishToExchange(ctx, constants.StageUpdated+".fanout", payload); err != nil {
		w.logger.Error("publish stage updated to fanout failed", "pipelineId", pipeline.ID, "err", err)
	}
}
