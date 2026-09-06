package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"pipelogiq/internal/constants"
	"pipelogiq/internal/schedule"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

const (
	defaultSchedulerPollInterval = time.Second
	minSchedulerPollInterval     = 100 * time.Millisecond
	// schedulerBatchSize bounds one pass so a backlog cannot hold the
	// transaction — and therefore the other instances — open indefinitely.
	schedulerBatchSize = 100
	// gaugeReconcileInterval is how often stale schedule_next_run_timestamp_seconds
	// series are dropped. A paused or archived schedule is never claimed again, so
	// its gauge would otherwise sit at a fixed past timestamp and keep the
	// "overdue" alert firing forever.
	gaugeReconcileInterval = time.Minute
)

type schedulerMetrics struct {
	runs            *prometheus.CounterVec
	lagSeconds      *prometheus.HistogramVec
	nextRunTime     *prometheus.GaugeVec
	tickDurationSec prometheus.Histogram
}

func newSchedulerMetrics() schedulerMetrics {
	return schedulerMetrics{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "schedule_runs_total",
			Help: "Schedule runs by schedule and outcome",
		}, []string{"schedule", "outcome"}),
		lagSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "schedule_lag_seconds",
			Help:    "Delay between a schedule's planned tick and the moment it fired",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60, 300, 900, 3600},
		}, []string{"schedule"}),
		nextRunTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "schedule_next_run_timestamp_seconds",
			Help: "Unix timestamp of a schedule's next planned run",
		}, []string{"schedule"}),
		tickDurationSec: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "scheduler_tick_duration_seconds",
			Help:    "Duration of one scheduler pass in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
		}),
	}
}

// runScheduler creates pipelines for due schedules. It creates pipelines and
// nothing else: it never executes a handler, never publishes to a stage queue,
// and never touches stage state. Everything after creation is the existing
// publisher, result consumer, lease, retry, and WebSocket machinery.
func (w *Worker) runScheduler(ctx context.Context) error {
	interval := w.cfg.SchedulerPollInterval
	if interval < minSchedulerPollInterval {
		interval = defaultSchedulerPollInterval
	}
	w.logger.Info("starting scheduler", "pollInterval", interval)

	// Each worker process gets its own jitter source, which is the point: two
	// instances of the same schedule must not pick the same offset.
	rng := newSchedulerRand()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	reconcile := time.NewTicker(gaugeReconcileInterval)
	defer reconcile.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-reconcile.C:
			w.reconcileScheduleGauges(ctx)
		case <-ticker.C:
			started := time.Now()
			if err := w.schedulerPass(ctx, rng); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.logger.Error("scheduler pass failed", "err", err)
			}
			w.metrics.scheduler.tickDurationSec.Observe(time.Since(started).Seconds())
		}
	}
}

// reconcileScheduleGauges drops next-run series for schedules that are no longer
// Active. Without this a paused schedule keeps a gauge pinned to a past
// timestamp, and the overdue alert fires on a schedule nobody expects to run.
func (w *Worker) reconcileScheduleGauges(ctx context.Context) {
	active, err := w.store.ActiveScheduleNames(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("reconcile schedule gauges failed", "err", err)
		}
		return
	}

	w.exportedMu.Lock()
	defer w.exportedMu.Unlock()
	for name := range w.exportedSchedules {
		if _, stillActive := active[name]; stillActive {
			continue
		}
		w.metrics.scheduler.nextRunTime.DeleteLabelValues(name)
		delete(w.exportedSchedules, name)
	}
}

func (w *Worker) schedulerPass(ctx context.Context, rng *rand.Rand) error {
	now := time.Now().UTC()

	claimed, err := w.store.ClaimDueSchedules(ctx, now, schedulerBatchSize,
		func(row store.ScheduleRow) (store.SchedulePlan, error) {
			return planSchedule(row, now, rng)
		})
	if err != nil {
		return err
	}

	for _, item := range claimed {
		w.exportScheduleGauges(item)
		for _, tick := range item.Plan.SkippedTicks {
			w.recordSkippedTick(ctx, item.Schedule, tick, types.ScheduleSkipReasonCatchup)
		}
		for _, tick := range item.Plan.Ticks {
			w.fireScheduleTick(ctx, item.Schedule, tick, triggerForKind(item.Schedule, tick, item.Plan))
		}
	}
	return nil
}

func (w *Worker) exportScheduleGauges(item store.ClaimedSchedule) {
	w.exportedMu.Lock()
	defer w.exportedMu.Unlock()

	if item.Plan.NextRunAt != nil {
		w.metrics.scheduler.nextRunTime.
			WithLabelValues(item.Schedule.Name).
			Set(float64(item.Plan.NextRunAt.Unix()))
		if w.exportedSchedules == nil {
			w.exportedSchedules = make(map[string]struct{})
		}
		w.exportedSchedules[item.Schedule.Name] = struct{}{}
		return
	}

	// A parked or self-archiving schedule has no next run to report.
	w.metrics.scheduler.nextRunTime.DeleteLabelValues(item.Schedule.Name)
	delete(w.exportedSchedules, item.Schedule.Name)
}

// specFor builds the timing spec from a stored row.
func specFor(row store.ScheduleRow) schedule.Spec {
	spec := schedule.Spec{
		Kind:           row.Kind,
		CronExpression: row.CronExpression.String,
		TimeZone:       row.TimeZone,
		JitterSeconds:  row.JitterSeconds,
	}
	if row.IntervalSeconds.Valid {
		seconds := int(row.IntervalSeconds.Int64)
		spec.IntervalSeconds = &seconds
	}
	if row.RunAt.Valid {
		runAt := row.RunAt.Time.UTC()
		spec.RunAt = &runAt
	}
	return spec
}

// planSchedule decides which ticks fire, which are recorded as skipped, and where
// the schedule's cursor moves. It is pure, so the catch-up policies are testable
// without a database.
func planSchedule(row store.ScheduleRow, now time.Time, rng *rand.Rand) (store.SchedulePlan, error) {
	spec := specFor(row)
	if !row.NextRunAt.Valid {
		return store.SchedulePlan{}, nil
	}

	missed, truncated, err := spec.MissedTicks(row.NextRunAt.Time.UTC(), now)
	if err != nil {
		return store.SchedulePlan{}, err
	}
	if len(missed) == 0 {
		return store.SchedulePlan{NextRunAt: timePtr(row.NextRunAt.Time.UTC())}, nil
	}

	plan := store.SchedulePlan{}
	switch row.CatchupPolicy {
	case types.CatchupPolicyAll:
		limit := row.CatchupMax
		if limit < 1 {
			limit = 1
		}
		if len(missed) > limit {
			// The oldest ticks beyond the budget are recorded, not silently dropped.
			plan.SkippedTicks = missed[:len(missed)-limit]
			missed = missed[len(missed)-limit:]
		}
		plan.Ticks = missed

	case types.CatchupPolicyOne:
		plan.SkippedTicks = missed[:len(missed)-1]
		plan.Ticks = missed[len(missed)-1:]

	default: // CatchupPolicyNone
		plan.SkippedTicks = missed[:len(missed)-1]
		plan.Ticks = missed[len(missed)-1:]
	}

	if truncated {
		// The enumeration hit its cap, so the cursor restarts from now rather than
		// from a tick far in the past that would be re-enumerated forever.
		next, ok, nextErr := spec.Next(now)
		if nextErr != nil {
			return store.SchedulePlan{}, nextErr
		}
		if ok {
			jittered := spec.WithJitter(next, rng)
			plan.NextRunAt = &jittered
		}
		return plan, nil
	}

	last := missed[len(missed)-1]
	next, ok, err := spec.Next(last)
	if err != nil {
		return store.SchedulePlan{}, err
	}
	if ok {
		// Jitter moves the next firing, not the recorded tick grid: for cron the
		// following Next() call snaps back onto the expression.
		jittered := spec.WithJitter(next, rng)
		plan.NextRunAt = &jittered
	} else if row.Kind == types.ScheduleKindOnce {
		// A Once schedule has no further tick; archive it instead of leaving an
		// Active row that can never fire again.
		plan.Status = types.ScheduleStatusArchived
	}
	return plan, nil
}

// triggerForKind labels a run. A catch-up tick that is not the latest one carries
// Catchup so the history distinguishes it from an on-time run.
func triggerForKind(row store.ScheduleRow, tick time.Time, plan store.SchedulePlan) string {
	if row.CatchupPolicy == types.CatchupPolicyOne && len(plan.SkippedTicks) > 0 {
		return types.ScheduleTriggerCatchup
	}
	if row.CatchupPolicy == types.CatchupPolicyAll &&
		len(plan.Ticks) > 0 && !tick.Equal(plan.Ticks[len(plan.Ticks)-1]) {
		return types.ScheduleTriggerCatchup
	}
	switch row.Kind {
	case types.ScheduleKindInterval:
		return types.ScheduleTriggerInterval
	case types.ScheduleKindOnce:
		return types.ScheduleTriggerOnce
	default:
		return types.ScheduleTriggerCron
	}
}

func (w *Worker) recordSkippedTick(
	ctx context.Context,
	row store.ScheduleRow,
	tick time.Time,
	reason string,
) {
	_, err := w.store.RecordScheduleRun(ctx, store.ScheduleRunRecord{
		ScheduleID:        row.ID,
		ScheduledFor:      tick,
		Trigger:           types.ScheduleTriggerCatchup,
		Outcome:           types.ScheduleRunOutcomeSkipped,
		SkipReason:        reason,
		DefinitionVersion: row.DefinitionVersion,
	})
	if err != nil && !errors.Is(err, store.ErrScheduleRunAlreadyRecorded) {
		w.logger.Error("record skipped schedule tick failed",
			"scheduleId", row.ID, "scheduledFor", tick, "err", err)
		return
	}
	if err == nil {
		w.metrics.scheduler.runs.WithLabelValues(row.Name, types.ScheduleRunOutcomeSkipped).Inc()
	}
}

// fireScheduleTick turns one tick into a pipeline. Every exit path records a run
// row, so a schedule can never fail silently, and no failure stops the loop.
func (w *Worker) fireScheduleTick(
	ctx context.Context,
	row store.ScheduleRow,
	tick time.Time,
	trigger string,
) {
	ctx, span := workerTracer.Start(ctx, "worker.fire_schedule_tick",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.Int64("pipelogiq.schedule.id", row.ID),
			attribute.String("pipelogiq.schedule.name", row.Name),
			attribute.String("pipelogiq.schedule.trigger", trigger),
		),
	)
	defer span.End()

	if skipped := w.applyOverlapPolicy(ctx, row, tick); skipped {
		span.SetStatus(codes.Ok, "skipped by overlap policy")
		return
	}

	runID, err := w.store.RecordScheduleRun(ctx, store.ScheduleRunRecord{
		ScheduleID:        row.ID,
		ScheduledFor:      tick,
		Trigger:           trigger,
		Outcome:           types.ScheduleRunOutcomeCreated,
		DefinitionVersion: row.DefinitionVersion,
	})
	if err != nil {
		if errors.Is(err, store.ErrScheduleRunAlreadyRecorded) {
			// Another instance, or this one before a restart, already fired this
			// tick. That is the guarantee working, not an error.
			w.logger.Info("schedule tick already fired elsewhere",
				"scheduleId", row.ID, "scheduledFor", tick)
			span.SetStatus(codes.Ok, "already fired")
			return
		}
		w.logger.Error("record schedule run failed", "scheduleId", row.ID, "err", err)
		span.RecordError(err)
		return
	}

	lag := time.Since(tick).Seconds()
	w.metrics.scheduler.lagSeconds.WithLabelValues(row.Name).Observe(lag)

	pipelineID, err := w.createScheduledPipeline(ctx, row, tick, runID)
	if err != nil {
		w.logger.Error("create scheduled pipeline failed",
			"scheduleId", row.ID, "name", row.Name, "scheduledFor", tick, "err", err)
		if failErr := w.store.FailScheduleRun(ctx, runID, err.Error()); failErr != nil {
			w.logger.Error("mark schedule run failed", "runId", runID, "err", failErr)
		}
		w.metrics.scheduler.runs.WithLabelValues(row.Name, types.ScheduleRunOutcomeFailed).Inc()
		span.RecordError(err)
		span.SetStatus(codes.Error, "pipeline creation failed")
		return
	}

	w.metrics.scheduler.runs.WithLabelValues(row.Name, types.ScheduleRunOutcomeCreated).Inc()
	span.SetAttributes(attribute.Int("pipelogiq.pipeline.id", pipelineID))
	span.SetStatus(codes.Ok, "created")
	w.logger.Info("schedule created a pipeline",
		"scheduleId", row.ID,
		"name", row.Name,
		"scheduledFor", tick,
		"runId", runID,
		"pipelineId", pipelineID,
		"lagSeconds", lag)

	// The publisher picks the first stage up on its own; wake it rather than wait
	// out a poll interval.
	w.triggerPublisher()
	w.publishScheduleRunCreated(ctx, row, runID, pipelineID, tick)
}

// applyOverlapPolicy reports whether the tick was consumed by the policy.
func (w *Worker) applyOverlapPolicy(ctx context.Context, row store.ScheduleRow, tick time.Time) bool {
	if row.OverlapPolicy == types.OverlapPolicyQueue {
		// Queue lets the run be created; ordering is handled at creation time.
		return false
	}

	running, err := w.store.UnfinishedSchedulePipeline(ctx, row.ID)
	if err != nil {
		w.logger.Error("overlap check failed", "scheduleId", row.ID, "err", err)
		return false
	}
	if running == nil {
		return false
	}

	if row.OverlapPolicy == types.OverlapPolicyReplace {
		if _, cancelErr := w.store.CancelPipelineForApplication(ctx, *running, row.ApplicationID); cancelErr != nil {
			w.logger.Error("replace overlap: cancel running pipeline failed",
				"scheduleId", row.ID, "pipelineId", *running, "err", cancelErr)
		} else {
			w.logger.Info("replace overlap: cancelled the running pipeline",
				"scheduleId", row.ID, "pipelineId", *running)
		}
		return false
	}

	// Skip: record the tick so the history shows why nothing ran.
	if _, err = w.store.RecordScheduleRun(ctx, store.ScheduleRunRecord{
		ScheduleID:        row.ID,
		ScheduledFor:      tick,
		Trigger:           types.ScheduleTriggerCron,
		Outcome:           types.ScheduleRunOutcomeSkipped,
		SkipReason:        types.ScheduleSkipReasonOverlap,
		PipelineID:        running,
		DefinitionVersion: row.DefinitionVersion,
	}); err != nil && !errors.Is(err, store.ErrScheduleRunAlreadyRecorded) {
		w.logger.Error("record overlap skip failed", "scheduleId", row.ID, "err", err)
	}
	w.metrics.scheduler.runs.WithLabelValues(row.Name, types.ScheduleRunOutcomeSkipped).Inc()
	w.logger.Info("schedule tick skipped because a run is still in flight",
		"scheduleId", row.ID, "scheduledFor", tick, "runningPipelineId", *running)
	return true
}

func (w *Worker) createScheduledPipeline(
	ctx context.Context,
	row store.ScheduleRow,
	tick time.Time,
	runID int64,
) (int, error) {
	definition, err := store.DecodeScheduleDefinition(row.DefinitionJSON)
	if err != nil {
		return 0, err
	}

	previous, err := w.store.LastScheduledPipelineID(ctx, row.ID)
	if err != nil {
		return 0, err
	}

	definition = store.ScheduleSubstitution{
		ScheduledFor:       tick,
		RunID:              runID,
		PreviousPipelineID: previous,
	}.Apply(definition)
	definition = store.WithScheduleKeyword(definition, row.Name)

	if row.OverlapPolicy == types.OverlapPolicyQueue {
		definition, err = w.queueBehindRunningPipeline(ctx, row, definition)
		if err != nil {
			return 0, err
		}
	}

	pipeline, _, err := w.store.CreatePipelineIdempotent(
		ctx, definition, row.ApplicationID, store.ScheduleIdempotencyKey(row.ID, tick),
	)
	if err != nil {
		return 0, err
	}

	if err = w.store.AttachPipelineToSchedule(ctx, pipeline.ID, row.ID, runID); err != nil {
		return 0, err
	}
	if err = w.store.LinkScheduleRunPipeline(ctx, runID, pipeline.ID); err != nil {
		return 0, err
	}
	return pipeline.ID, nil
}

// queueBehindRunningPipeline makes the new run's first stage depend on the last
// stage of the pipeline still in flight.
//
// The limitation is worth stating: the dependency is on that *pipeline*, not on
// the schedule. Three queued runs all wait for the same in-flight pipeline and
// then start together, rather than forming a chain.
func (w *Worker) queueBehindRunningPipeline(
	ctx context.Context,
	row store.ScheduleRow,
	definition types.PipelineCreateRequest,
) (types.PipelineCreateRequest, error) {
	if len(definition.Stages) == 0 {
		return definition, nil
	}

	running, err := w.store.UnfinishedSchedulePipeline(ctx, row.ID)
	if err != nil || running == nil {
		return definition, err
	}

	pipeline, err := w.store.GetPipelineWithStages(ctx, *running)
	if err != nil || pipeline == nil || len(pipeline.Stages) == 0 {
		return definition, err
	}

	last := pipeline.Stages[len(pipeline.Stages)-1]
	options := types.StageOptions{}
	if definition.Stages[0].Options != nil {
		options = *definition.Stages[0].Options
	}
	dependency := fmt.Sprintf("%d", last.ID)
	for _, existing := range options.DependsOn {
		if existing == dependency {
			return definition, nil
		}
	}
	options.DependsOn = append(append([]string(nil), options.DependsOn...), dependency)
	definition.Stages[0].Options = &options
	return definition, nil
}

func timePtr(t time.Time) *time.Time { return &t }

// newSchedulerRand gives each worker process its own jitter source.
func newSchedulerRand() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// ScheduleRunCreatedEvent is broadcast on the StageUpdated fanout so the
// dashboard's Schedules page updates without polling. It carries applicationId
// because the hub scopes every broadcast on it, and an `event` discriminator so
// the existing pipeline-update consumers ignore it.
type ScheduleRunCreatedEvent struct {
	Event         string    `json:"event"`
	ApplicationID int       `json:"applicationId"`
	ScheduleID    int64     `json:"scheduleId"`
	ScheduleName  string    `json:"scheduleName"`
	RunID         int64     `json:"runId"`
	PipelineID    int       `json:"pipelineId"`
	ScheduledFor  time.Time `json:"scheduledFor"`
}

// ScheduleRunCreatedEventName is the discriminator value.
const ScheduleRunCreatedEventName = "ScheduleRunCreated"

func (w *Worker) publishScheduleRunCreated(
	ctx context.Context,
	row store.ScheduleRow,
	runID int64,
	pipelineID int,
	tick time.Time,
) {
	if w.mq == nil {
		return
	}
	payload, err := json.Marshal(ScheduleRunCreatedEvent{
		Event:         ScheduleRunCreatedEventName,
		ApplicationID: row.ApplicationID,
		ScheduleID:    row.ID,
		ScheduleName:  row.Name,
		RunID:         runID,
		PipelineID:    pipelineID,
		ScheduledFor:  tick.UTC(),
	})
	if err != nil {
		w.logger.Error("marshal schedule run event failed", "scheduleId", row.ID, "err", err)
		return
	}
	if err = w.mq.PublishToExchange(ctx, constants.StageUpdated+".fanout", payload); err != nil {
		w.logger.Error("publish schedule run event failed", "scheduleId", row.ID, "err", err)
	}
}
