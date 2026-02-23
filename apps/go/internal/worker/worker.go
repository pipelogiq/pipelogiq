package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	amqp "github.com/rabbitmq/amqp091-go"

	"pipelogiq/internal/config"
	"pipelogiq/internal/constants"
	"pipelogiq/internal/mq"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

type Worker struct {
	cfg    config.WorkerConfig
	store  *store.Store
	mq     *mq.Client
	logger *slog.Logger

	metrics workerMetrics
}

type workerMetrics struct {
	stagePublished       prometheus.Counter
	stageResultProcessed prometheus.Counter
	stageResultFailed    prometheus.Counter
	stageStatusUpdated   prometheus.Counter
	pendingMarkedFailed  prometheus.Counter
}

func New(cfg config.WorkerConfig, st *store.Store, mqClient *mq.Client, logger *slog.Logger) *Worker {
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
		pendingMarkedFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pending_marked_failed_total",
			Help: "Number of pending stages marked as failed due to timeout",
		}),
	}
	prometheus.MustRegister(
		metrics.stagePublished,
		metrics.stageResultProcessed,
		metrics.stageResultFailed,
		metrics.stageStatusUpdated,
		metrics.pendingMarkedFailed,
	)

	return &Worker{
		cfg:     cfg,
		store:   st,
		mq:      mqClient,
		logger:  logger,
		metrics: metrics,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	errCh := make(chan error, 3)

	go func() { errCh <- w.runPublisher(ctx) }()
	go func() { errCh <- w.runStageResultConsumer(ctx) }()
	go func() { errCh <- w.runStageStatusConsumer(ctx) }()
	go func() { errCh <- w.runPendingWatcher(ctx) }()

	if w.cfg.MetricsAddr != "" {
		go w.runMetricsServer(ctx)
	}

	select {
	case <-ctx.Done():
		w.logger.Info("worker shutting down")
		return ctx.Err()
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
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

func (w *Worker) runPublisher(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stage, err := w.store.GetStageToExecute(ctx)
			if err != nil {
				w.logger.Error("get stage to execute failed", "err", err)
				continue
			}
			if stage == nil {
				continue
			}

			queue := stageQueueName(w.cfg.AppID, stage.StageHandlerName)
			body, _ := json.Marshal(stage)
			opts := mq.QueueOptions{
				Durable:     true,
				DLQEnabled:  w.cfg.QueueDLQEnabled,
				DLQTTL:      w.cfg.QueueDLQMessageTTL,
				ContentType: "application/json",
			}

			if err := w.mq.PublishWithRetry(ctx, queue, body, opts, nil); err != nil {
				w.logger.Error("publish stage next failed", "queue", queue, "err", err)
				continue
			}

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
		}
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
		pipeline, err := w.store.UpdateStageResult(ctx, msg)
		if err != nil {
			w.metrics.stageResultFailed.Inc()
			return err
		}

		w.publishPipelineUpdate(ctx, pipeline)
		w.metrics.stageResultProcessed.Inc()
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
		pipeline, err := w.store.UpdateStageStatus(ctx, msg)
		if err != nil {
			return err
		}
		w.publishPipelineUpdate(ctx, pipeline)
		w.metrics.stageStatusUpdated.Inc()
		return nil
	}

	w.logger.Info("starting StageSetStatus consumer")
	return w.mq.Consume(ctx, constants.StageSetStatus, opts, handler)
}

func (w *Worker) runPendingWatcher(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.StagePendingTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			affected, err := w.store.MarkPendingTooLong(ctx, w.cfg.StagePendingTimeout)
			if err != nil {
				w.logger.Error("mark pending too long failed", "err", err)
				continue
			}
			if affected > 0 {
				w.metrics.pendingMarkedFailed.Add(float64(affected))
				w.logger.Warn("marked pending stages as failed", "count", affected)
			}
		}
	}
}

func stageQueueName(appID string, handler string) string {
	return appID + "_" + handler + "_" + constants.StageNext
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

	pubOpts := mq.QueueOptions{
		Durable:     true,
		DLQEnabled:  w.cfg.QueueDLQEnabled,
		DLQTTL:      w.cfg.QueueDLQMessageTTL,
		ContentType: "application/json",
	}

	if err := w.mq.PublishWithRetry(ctx, constants.StageUpdated, payload, pubOpts, nil); err != nil {
		w.logger.Error("publish stage updated failed", "pipelineId", pipeline.ID, "err", err)
	}
	if err := w.mq.PublishToExchange(ctx, constants.StageUpdated+".fanout", payload); err != nil {
		w.logger.Error("publish stage updated to fanout failed", "pipelineId", pipeline.ID, "err", err)
	}
}
