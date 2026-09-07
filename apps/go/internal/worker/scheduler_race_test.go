package worker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/config"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

// schedulerRaceSchema is the subset of the schema a scheduler pass touches:
// claim a due schedule, record the run, create the pipeline through the
// idempotent creation path, and check that a worker exists to run it.
const schedulerRaceSchema = `
CREATE TABLE worker_client (
	id TEXT PRIMARY KEY,
	application_id INTEGER NOT NULL,
	state TEXT NOT NULL,
	last_seen_at TIMESTAMP NULL
);

CREATE TABLE application (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	description TEXT NULL,
	created_at TIMESTAMP NULL
);
CREATE TABLE schedule (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	application_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'Active',
	kind TEXT NOT NULL,
	cron_expression TEXT NULL,
	time_zone TEXT NOT NULL DEFAULT 'UTC',
	interval_seconds INTEGER NULL,
	run_at TIMESTAMP NULL,
	definition_json TEXT NOT NULL DEFAULT '{}',
	definition_version INTEGER NOT NULL DEFAULT 1,
	overlap_policy TEXT NOT NULL DEFAULT 'Skip',
	catchup_policy TEXT NOT NULL DEFAULT 'None',
	catchup_max INTEGER NOT NULL DEFAULT 10,
	jitter_seconds INTEGER NOT NULL DEFAULT 0,
	next_run_at TIMESTAMP NULL,
	last_run_at TIMESTAMP NULL,
	last_run_status TEXT NULL,
	created_by TEXT NULL,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uq_schedule_application_name ON schedule(application_id, lower(name));
CREATE TABLE schedule_run (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	schedule_id INTEGER NOT NULL,
	scheduled_for TIMESTAMP NOT NULL,
	fired_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	trigger TEXT NOT NULL,
	outcome TEXT NOT NULL,
	skip_reason TEXT NULL,
	pipeline_id INTEGER NULL,
	definition_version INTEGER NOT NULL DEFAULT 1,
	error TEXT NULL
);
CREATE UNIQUE INDEX uq_schedule_run_tick ON schedule_run(schedule_id, scheduled_for);
CREATE TABLE pipeline (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	status TEXT,
	created_at TIMESTAMP NOT NULL,
	finished_at TIMESTAMP NULL,
	is_completed BOOLEAN NOT NULL DEFAULT 0,
	application_id INTEGER NULL,
	trace_id TEXT NULL,
	idempotency_key TEXT NULL,
	request_hash TEXT NULL,
	schedule_id INTEGER NULL,
	schedule_run_id INTEGER NULL
);
CREATE UNIQUE INDEX uq_pipeline_application_idempotency_key
	ON pipeline(application_id, idempotency_key);
CREATE TABLE stage (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	stage_handler_name TEXT NULL,
	description TEXT NULL,
	status TEXT NULL,
	pipeline_id INTEGER NOT NULL,
	created_at TIMESTAMP NOT NULL,
	finished_at TIMESTAMP NULL,
	started_at TIMESTAMP NULL,
	is_skipped BOOLEAN NULL,
	is_event BOOLEAN NULL,
	span_id TEXT NULL,
	retry_attempt INTEGER NOT NULL DEFAULT 0,
	next_retry_at TIMESTAMP NULL,
	execution_id TEXT NULL,
	execution_attempt INTEGER NOT NULL DEFAULT 0,
	dispatched_at TIMESTAMP NULL,
	lease_owner TEXT NULL,
	lease_expires_at TIMESTAMP NULL,
	last_error_code TEXT NULL,
	failure_disposition TEXT NULL
);
CREATE TABLE stage_io (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	input TEXT NULL,
	output TEXT NULL,
	stage_id INTEGER NOT NULL
);
CREATE TABLE stage_options (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	stage_id INTEGER NOT NULL,
	run_next_if_failed BOOLEAN NULL,
	retry_interval INTEGER NULL,
	time_out INTEGER NULL,
	max_retries INTEGER NULL,
	retry_on_error_codes TEXT NULL,
	retry_backoff TEXT NULL,
	max_retry_interval INTEGER NULL,
	retry_jitter BOOLEAN NULL,
	depends_on TEXT NULL,
	run_in_parallel_with TEXT NULL,
	fail_if_output_empty BOOLEAN NULL,
	notify_on_failure BOOLEAN NULL,
	run_as_user TEXT NULL
);
CREATE TABLE stage_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	log TEXT NULL,
	log_level TEXT NULL,
	created_at TIMESTAMP NULL,
	stage_id INTEGER NOT NULL
);
CREATE TABLE keyword (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	key TEXT NOT NULL,
	value TEXT NOT NULL
);
CREATE TABLE pipeline_keyword (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pipeline_id INTEGER NOT NULL,
	keyword_id INTEGER NOT NULL
);
CREATE TABLE pipeline_context_item (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pipeline_id INTEGER NOT NULL,
	key TEXT NOT NULL,
	value TEXT NULL,
	value_type TEXT NULL,
	is_sensitive BOOLEAN NOT NULL DEFAULT 0
);
`

func setupSchedulerRaceStore(t *testing.T) (*store.Store, *sqlx.DB) {
	t.Helper()

	db, err := sqlx.Open("sqlite", testDSN("scheduler_race"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err = db.Exec(schedulerRaceSchema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}
	if _, err = db.Exec(`INSERT INTO application (id, name) VALUES (1, 'tiqet')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed application: %v", err)
	}
	// A live worker is the ordinary state of the world. Tests about what the
	// scheduler does with a tick should not accidentally be testing what it does
	// when there is nowhere to send one.
	if _, err = db.Exec(`
		INSERT INTO worker_client (id, application_id, state, last_seen_at)
		VALUES ('worker-1', 1, 'ready', $1)
	`, time.Now().UTC()); err != nil {
		_ = db.Close()
		t.Fatalf("seed worker: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return store.New(db, slog.New(slog.NewTextHandler(io.Discard, nil))), db
}

// newSchedulerTestWorker builds a worker with no broker: the scheduler only
// creates pipelines, so it never needs one.
func newSchedulerTestWorker(st *store.Store) *Worker {
	return &Worker{
		cfg:               config.WorkerConfig{SchedulerPollInterval: time.Second, StageActiveTimeout: time.Minute},
		store:             st,
		mq:                nil,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		wake:              make(chan struct{}, 1),
		metrics:           workerMetrics{scheduler: newSchedulerMetrics()},
		exportedSchedules: make(map[string]struct{}),
	}
}

func seedRaceSchedule(t *testing.T, st *store.Store, dueAt time.Time) int64 {
	t.Helper()

	definition, err := json.Marshal(types.PipelineCreateRequest{
		Name: "dispatch-due-emails",
		Stages: []types.StageCreate{
			{Name: "dispatch", StageHandler: "DispatchDueEmailsHandler", Input: `{"batchSize":200}`},
		},
	})
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}

	schedule, _, err := st.UpsertSchedule(context.Background(), 1, store.ScheduleUpsert{
		Name:           "emails",
		Kind:           types.ScheduleKindCron,
		CronExpression: "* * * * *",
		TimeZone:       "UTC",
		OverlapPolicy:  types.OverlapPolicySkip,
		CatchupPolicy:  types.CatchupPolicyNone,
		CatchupMax:     10,
		DefinitionJSON: string(definition),
		NextRunAt:      &dueAt,
		CreatedBy:      "test",
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	return schedule.ID
}

// TestTwoSchedulerInstancesFireEachTickOnce runs two independent schedulers
// against one database, as two replicas of pipelogiq-worker would. The tick must
// produce exactly one schedule_run row and exactly one pipeline.
func TestTwoSchedulerInstancesFireEachTickOnce(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulerRaceStore(t)
	scheduleID := seedRaceSchedule(t, st, time.Now().UTC().Add(-time.Minute))

	first := newSchedulerTestWorker(st)
	second := newSchedulerTestWorker(st)

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 2)

	for i, w := range []*Worker{first, second} {
		wg.Add(1)
		go func(instance int, worker *Worker) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(instance)))
			<-start
			// Several passes each, so the two instances interleave rather than
			// happening to run one after the other.
			for pass := 0; pass < 5; pass++ {
				if err := worker.schedulerPass(context.Background(), rng); err != nil {
					errs <- err
					return
				}
			}
		}(i, w)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("scheduler pass failed: %v", err)
	}

	// The guarantee: no (schedule, scheduled_for) tick is recorded twice, however
	// the two instances interleaved.
	var distinctTicks, totalRuns int
	if err := db.Get(&distinctTicks, `
		SELECT COUNT(DISTINCT scheduled_for) FROM schedule_run WHERE schedule_id = $1
	`, scheduleID); err != nil {
		t.Fatalf("count distinct ticks: %v", err)
	}
	if err := db.Get(&totalRuns, `
		SELECT COUNT(*) FROM schedule_run WHERE schedule_id = $1
	`, scheduleID); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if distinctTicks != totalRuns {
		t.Fatalf("%d run rows for %d distinct ticks: a tick fired twice", totalRuns, distinctTicks)
	}

	// Exactly one tick actually ran, and it produced exactly one pipeline.
	var created int
	if err := db.Get(&created, `
		SELECT COUNT(*) FROM schedule_run WHERE schedule_id = $1 AND outcome = $2
	`, scheduleID, types.ScheduleRunOutcomeCreated); err != nil {
		t.Fatalf("count created runs: %v", err)
	}
	if created != 1 {
		t.Fatalf("two schedulers produced %d Created runs, want 1", created)
	}

	var pipelines int
	if err := db.Get(&pipelines, `SELECT COUNT(*) FROM pipeline WHERE schedule_id = $1`, scheduleID); err != nil {
		t.Fatalf("count pipelines: %v", err)
	}
	if pipelines != 1 {
		t.Fatalf("two schedulers created %d pipelines for one tick, want 1", pipelines)
	}

	var (
		outcome    string
		pipelineID int
	)
	if err := db.QueryRow(`
		SELECT outcome, COALESCE(pipeline_id, 0) FROM schedule_run
		WHERE schedule_id = $1 AND outcome = $2
	`, scheduleID, types.ScheduleRunOutcomeCreated).Scan(&outcome, &pipelineID); err != nil {
		t.Fatalf("load run: %v", err)
	}
	if pipelineID == 0 {
		t.Fatalf("the Created run must link its pipeline")
	}

	// The created pipeline must be an ordinary one: NotStarted, with its stage
	// waiting for the existing publisher rather than anything the scheduler did.
	var status string
	if err := db.Get(&status, `SELECT status FROM pipeline WHERE id = $1`, pipelineID); err != nil {
		t.Fatalf("load pipeline: %v", err)
	}
	if status != types.PipelineStatusNotStarted {
		t.Fatalf("pipeline status = %q, want NotStarted; the scheduler must not execute anything", status)
	}

	var stages int
	if err := db.Get(&stages, `SELECT COUNT(*) FROM stage WHERE pipeline_id = $1`, pipelineID); err != nil {
		t.Fatalf("count stages: %v", err)
	}
	if stages != 1 {
		t.Fatalf("pipeline has %d stages, want 1", stages)
	}
}

// TestSchedulerSkipsOverlappingRun proves the Skip policy records why nothing ran.
func TestSchedulerSkipsOverlappingRun(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulerRaceStore(t)
	scheduleID := seedRaceSchedule(t, st, time.Now().UTC().Add(-time.Minute))
	worker := newSchedulerTestWorker(st)

	if _, err := db.Exec(`
		INSERT INTO pipeline (name, status, created_at, is_completed, application_id, schedule_id)
		VALUES ('in-flight', 'Running', $1, false, 1, $2)
	`, time.Now().UTC(), scheduleID); err != nil {
		t.Fatalf("seed running pipeline: %v", err)
	}

	if err := worker.schedulerPass(context.Background(), rand.New(rand.NewSource(1))); err != nil {
		t.Fatalf("scheduler pass: %v", err)
	}

	// A catch-up tick may also be recorded; what matters is that the tick that
	// would have fired was skipped for Overlap, with the in-flight pipeline named.
	var overlapRuns int
	if err := db.Get(&overlapRuns, `
		SELECT COUNT(*) FROM schedule_run
		WHERE schedule_id = $1 AND outcome = $2 AND skip_reason = $3 AND pipeline_id IS NOT NULL
	`, scheduleID, types.ScheduleRunOutcomeSkipped, types.ScheduleSkipReasonOverlap); err != nil {
		t.Fatalf("count overlap skips: %v", err)
	}
	if overlapRuns != 1 {
		t.Fatalf("overlap skip rows = %d, want 1", overlapRuns)
	}

	var createdRuns int
	if err := db.Get(&createdRuns, `
		SELECT COUNT(*) FROM schedule_run WHERE schedule_id = $1 AND outcome = $2
	`, scheduleID, types.ScheduleRunOutcomeCreated); err != nil {
		t.Fatalf("count created runs: %v", err)
	}
	if createdRuns != 0 {
		t.Fatalf("Skip must not create a run, found %d", createdRuns)
	}

	var created int
	if err := db.Get(&created, `
		SELECT COUNT(*) FROM pipeline WHERE schedule_id = $1 AND name = 'dispatch-due-emails'
	`, scheduleID); err != nil {
		t.Fatalf("count pipelines: %v", err)
	}
	if created != 0 {
		t.Fatalf("Skip must not create a pipeline, found %d", created)
	}
}

// TestSchedulerSubstitutesPlaceholders covers the three supported placeholders
// and confirms nothing else is templated.
func TestSchedulerSubstitutesPlaceholders(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulerRaceStore(t)
	tick := time.Now().UTC().Add(-time.Minute).Truncate(time.Minute)

	definition, err := json.Marshal(types.PipelineCreateRequest{
		Name: "templated",
		Stages: []types.StageCreate{{
			Name:         "dispatch",
			StageHandler: "DispatchHandler",
			Input:        `{"at":"{{scheduledFor}}","run":"{{runId}}","prev":"{{previousPipelineId}}","other":"{{notASubstitution}}"}`,
		}},
		PipelineContext: []types.ContextItem{
			{Key: "window", Value: "{{scheduledFor}}", ValueType: "System.String"},
		},
	})
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}

	schedule, _, err := st.UpsertSchedule(context.Background(), 1, store.ScheduleUpsert{
		Name:           "templated",
		Kind:           types.ScheduleKindCron,
		CronExpression: "* * * * *",
		TimeZone:       "UTC",
		OverlapPolicy:  types.OverlapPolicySkip,
		CatchupPolicy:  types.CatchupPolicyNone,
		CatchupMax:     10,
		DefinitionJSON: string(definition),
		NextRunAt:      &tick,
		CreatedBy:      "test",
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	worker := newSchedulerTestWorker(st)
	if err = worker.schedulerPass(context.Background(), rand.New(rand.NewSource(1))); err != nil {
		t.Fatalf("scheduler pass: %v", err)
	}

	// The tick that fired is the latest missed one, which is what the run records.
	var firedFor time.Time
	if err = db.Get(&firedFor, `
		SELECT scheduled_for FROM schedule_run
		WHERE schedule_id = $1 AND outcome = $2
	`, schedule.ID, types.ScheduleRunOutcomeCreated); err != nil {
		t.Fatalf("load fired tick: %v", err)
	}
	firedFor = firedFor.UTC()

	var input string
	if err = db.Get(&input, `
		SELECT COALESCE(io.input, '')
		FROM stage_io io
		JOIN stage s ON s.id = io.stage_id
		JOIN pipeline p ON p.id = s.pipeline_id
		WHERE p.schedule_id = $1
	`, schedule.ID); err != nil {
		t.Fatalf("load stage input: %v", err)
	}

	var parsed map[string]string
	if err = json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("decode substituted input %q: %v", input, err)
	}
	if parsed["at"] != firedFor.Format(time.RFC3339) {
		t.Fatalf("scheduledFor = %q, want %s", parsed["at"], firedFor.Format(time.RFC3339))
	}
	if parsed["run"] == "" || parsed["run"] == "{{runId}}" {
		t.Fatalf("runId was not substituted: %q", parsed["run"])
	}
	if parsed["prev"] != "" {
		t.Fatalf("previousPipelineId on a first run must be empty, got %q", parsed["prev"])
	}
	if parsed["other"] != "{{notASubstitution}}" {
		t.Fatalf("only the three documented placeholders are substituted, got %q", parsed["other"])
	}

	var contextValue string
	if err = db.Get(&contextValue, `
		SELECT value FROM pipeline_context_item
		WHERE pipeline_id = (SELECT id FROM pipeline WHERE schedule_id = $1)
	`, schedule.ID); err != nil {
		t.Fatalf("load context item: %v", err)
	}
	if contextValue != firedFor.Format(time.RFC3339) {
		t.Fatalf("context value = %q, want %s", contextValue, firedFor.Format(time.RFC3339))
	}

	// The keyword the dashboard filters on is added automatically.
	var keywords int
	if err = db.Get(&keywords, `
		SELECT COUNT(*) FROM pipeline_keyword pk
		JOIN keyword k ON k.id = pk.keyword_id
		JOIN pipeline p ON p.id = pk.pipeline_id
		WHERE p.schedule_id = $1 AND k.key = 'schedule' AND k.value = 'templated'
	`, schedule.ID); err != nil {
		t.Fatalf("count keywords: %v", err)
	}
	if keywords != 1 {
		t.Fatalf("schedule keyword count = %d, want 1", keywords)
	}
}

// TestReconcileScheduleGaugesDropsInactiveSeries covers the alerting failure mode:
// a paused schedule keeps a next-run gauge pinned to a past timestamp, and the
// overdue alert then fires forever on a schedule nobody expects to run.
func TestReconcileScheduleGaugesDropsInactiveSeries(t *testing.T) {
	t.Parallel()

	st, _ := setupSchedulerRaceStore(t)
	seedRaceSchedule(t, st, time.Now().UTC().Add(-time.Minute))
	worker := newSchedulerTestWorker(st)
	ctx := context.Background()

	if err := worker.schedulerPass(ctx, rand.New(rand.NewSource(1))); err != nil {
		t.Fatalf("scheduler pass: %v", err)
	}

	worker.exportedMu.Lock()
	exported := len(worker.exportedSchedules)
	worker.exportedMu.Unlock()
	if exported != 1 {
		t.Fatalf("exported %d gauges after a pass, want 1", exported)
	}

	// Reconciling while the schedule is still Active must keep the series.
	worker.reconcileScheduleGauges(ctx)
	worker.exportedMu.Lock()
	exported = len(worker.exportedSchedules)
	worker.exportedMu.Unlock()
	if exported != 1 {
		t.Fatalf("an Active schedule lost its gauge")
	}

	if _, err := st.SetScheduleStatus(ctx, 1, "emails", types.ScheduleStatusPaused, nil); err != nil {
		t.Fatalf("pause: %v", err)
	}

	worker.reconcileScheduleGauges(ctx)
	worker.exportedMu.Lock()
	exported = len(worker.exportedSchedules)
	worker.exportedMu.Unlock()
	if exported != 0 {
		t.Fatalf("a paused schedule kept %d gauges, want 0", exported)
	}
}

// TestSchedulerAllowsOverlappingRun is the counterpart to the Skip test: a
// schedule that opts into concurrency fires its tick even with a run in flight.
func TestSchedulerAllowsOverlappingRun(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulerRaceStore(t)
	scheduleID := seedRaceSchedule(t, st, time.Now().UTC().Add(-time.Minute))
	worker := newSchedulerTestWorker(st)

	if _, err := db.Exec(`UPDATE schedule SET overlap_policy = $2 WHERE id = $1`,
		scheduleID, types.OverlapPolicyAllow); err != nil {
		t.Fatalf("set overlap policy: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO pipeline (name, status, created_at, is_completed, application_id, schedule_id)
		VALUES ('in-flight', 'Running', $1, false, 1, $2)
	`, time.Now().UTC(), scheduleID); err != nil {
		t.Fatalf("seed running pipeline: %v", err)
	}

	if err := worker.schedulerPass(context.Background(), rand.New(rand.NewSource(1))); err != nil {
		t.Fatalf("scheduler pass: %v", err)
	}

	var overlapSkips int
	if err := db.Get(&overlapSkips, `
		SELECT COUNT(*) FROM schedule_run
		WHERE schedule_id = $1 AND skip_reason = $2
	`, scheduleID, types.ScheduleSkipReasonOverlap); err != nil {
		t.Fatalf("count overlap skips: %v", err)
	}
	if overlapSkips != 0 {
		t.Fatalf("Allow recorded %d overlap skips, want none", overlapSkips)
	}

	var created int
	if err := db.Get(&created, `
		SELECT COUNT(*) FROM pipeline WHERE schedule_id = $1 AND name = 'dispatch-due-emails'
	`, scheduleID); err != nil {
		t.Fatalf("count pipelines: %v", err)
	}
	if created != 1 {
		t.Fatalf("Allow created %d pipelines alongside the in-flight one, want 1", created)
	}

	// The point of Allow: the earlier run is untouched, unlike Replace.
	var stillRunning int
	if err := db.Get(&stillRunning, `
		SELECT COUNT(*) FROM pipeline WHERE schedule_id = $1 AND name = 'in-flight' AND status = 'Running'
	`, scheduleID); err != nil {
		t.Fatalf("count in-flight: %v", err)
	}
	if stillRunning != 1 {
		t.Fatalf("Allow disturbed the in-flight pipeline (%d still running, want 1)", stillRunning)
	}
}

// TestSchedulerSkipsWhenNoWorkerIsOnline: a pipeline nobody can pick up is not
// work, it is a backlog. The tick is recorded so the history says why.
func TestSchedulerSkipsWhenNoWorkerIsOnline(t *testing.T) {
	t.Parallel()

	st, db := setupSchedulerRaceStore(t)
	scheduleID := seedRaceSchedule(t, st, time.Now().UTC().Add(-time.Minute))
	worker := newSchedulerTestWorker(st)

	// The worker is still registered — it simply stopped saying anything, which
	// is what a crashed process looks like from here.
	if _, err := db.Exec(`UPDATE worker_client SET last_seen_at = $1`,
		time.Now().UTC().Add(-10*time.Minute)); err != nil {
		t.Fatalf("age the worker heartbeat: %v", err)
	}

	if err := worker.schedulerPass(context.Background(), rand.New(rand.NewSource(1))); err != nil {
		t.Fatalf("scheduler pass: %v", err)
	}

	var created int
	if err := db.Get(&created, `
		SELECT COUNT(*) FROM pipeline WHERE schedule_id = $1
	`, scheduleID); err != nil {
		t.Fatalf("count pipelines: %v", err)
	}
	if created != 0 {
		t.Fatalf("created %d pipelines with no worker online, want 0", created)
	}

	var skips int
	if err := db.Get(&skips, `
		SELECT COUNT(*) FROM schedule_run
		WHERE schedule_id = $1 AND outcome = $2 AND skip_reason = $3
	`, scheduleID, types.ScheduleRunOutcomeSkipped, types.ScheduleSkipReasonNoWorker); err != nil {
		t.Fatalf("count no-worker skips: %v", err)
	}
	if skips == 0 {
		t.Fatalf("nothing recorded the skip, so the history would not explain itself")
	}

	// The ordinary path — a worker is online and the tick fires — is what every
	// other test in this file now exercises, since the fixture seeds a live one.
}
