package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"

	"pipelogiq/internal/types"
)

const scheduleTestSchema = `
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
`

func setupScheduleStore(t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()

	db, err := sqlx.Open("sqlite", testDSN("schedule_test"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err = db.Exec(scheduleTestSchema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema: %v", err)
	}
	if _, err = db.Exec(`INSERT INTO application (id, name) VALUES (1, 'tiqet')`); err != nil {
		_ = db.Close()
		t.Fatalf("seed application: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return New(db, slog.New(slog.NewTextHandler(io.Discard, nil))), db
}

func sampleDefinition(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(types.PipelineCreateRequest{
		Name: "dispatch-due-emails",
		Stages: []types.StageCreate{
			{Name: "dispatch", StageHandler: "DispatchDueEmailsHandler", Input: `{"batchSize":200}`},
		},
	})
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}
	return string(raw)
}

func baseUpsert(t *testing.T, name string) ScheduleUpsert {
	t.Helper()
	next := time.Now().UTC().Add(time.Minute)
	return ScheduleUpsert{
		Name:           name,
		Kind:           types.ScheduleKindCron,
		CronExpression: "* * * * *",
		TimeZone:       "UTC",
		OverlapPolicy:  types.OverlapPolicySkip,
		CatchupPolicy:  types.CatchupPolicyNone,
		CatchupMax:     10,
		DefinitionJSON: sampleDefinition(t),
		NextRunAt:      &next,
		CreatedBy:      "application:1",
	}
}

func TestUpsertSchedule(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("creates and then updates in place", func(t *testing.T) {
		t.Parallel()
		st, _ := setupScheduleStore(t)

		created, wasCreated, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
		if err != nil || !wasCreated {
			t.Fatalf("created=%v err=%v", wasCreated, err)
		}
		if created.DefinitionVersion != 1 || created.Status != types.ScheduleStatusActive {
			t.Fatalf("unexpected schedule %+v", created)
		}

		second, wasCreated, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
		if err != nil || wasCreated {
			t.Fatalf("created=%v err=%v, want an update", wasCreated, err)
		}
		if second.ID != created.ID {
			t.Fatalf("upsert created a second row: %d then %d", created.ID, second.ID)
		}
	})

	t.Run("an unchanged definition keeps the version and the cursor", func(t *testing.T) {
		t.Parallel()
		st, _ := setupScheduleStore(t)

		first, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// A client that re-applies the same schedule on every deploy must not
		// churn the version or push the next tick out.
		later := baseUpsert(t, "emails")
		later.NextRunAt = timePointer(time.Now().UTC().Add(time.Hour))

		second, _, err := st.UpsertSchedule(ctx, 1, later)
		if err != nil {
			t.Fatalf("re-apply: %v", err)
		}
		if second.DefinitionVersion != first.DefinitionVersion {
			t.Fatalf("version moved from %d to %d on an unchanged definition",
				first.DefinitionVersion, second.DefinitionVersion)
		}
		if first.NextRunAt == nil || second.NextRunAt == nil || !second.NextRunAt.Equal(*first.NextRunAt) {
			t.Fatalf("nextRunAt moved from %v to %v on an unchanged definition",
				first.NextRunAt, second.NextRunAt)
		}
	})

	t.Run("a changed definition bumps the version and re-plans", func(t *testing.T) {
		t.Parallel()
		st, _ := setupScheduleStore(t)

		first, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		changed := baseUpsert(t, "emails")
		changed.CronExpression = "*/5 * * * *"
		changed.NextRunAt = timePointer(time.Now().UTC().Add(5 * time.Minute))

		second, _, err := st.UpsertSchedule(ctx, 1, changed)
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if second.DefinitionVersion != first.DefinitionVersion+1 {
			t.Fatalf("version = %d, want %d", second.DefinitionVersion, first.DefinitionVersion+1)
		}
		if second.NextRunAt == nil || !second.NextRunAt.After(*first.NextRunAt) {
			t.Fatalf("a changed definition must re-plan the cursor")
		}
	})

	t.Run("matches the name case-insensitively", func(t *testing.T) {
		t.Parallel()
		st, _ := setupScheduleStore(t)

		first, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "Emails"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		second, wasCreated, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if wasCreated || second.ID != first.ID {
			t.Fatalf("case-different names must be the same schedule")
		}
	})

	t.Run("re-applying an archived schedule reactivates it", func(t *testing.T) {
		t.Parallel()
		st, _ := setupScheduleStore(t)

		if _, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails")); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := st.SetScheduleStatus(ctx, 1, "emails", types.ScheduleStatusArchived, nil); err != nil {
			t.Fatalf("archive: %v", err)
		}

		revived, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
		if err != nil {
			t.Fatalf("re-apply: %v", err)
		}
		if revived.Status != types.ScheduleStatusActive {
			t.Fatalf("status = %q, want Active", revived.Status)
		}
		if revived.NextRunAt == nil {
			t.Fatalf("a revived schedule must be re-planned")
		}
	})
}

// TestUpsertScheduleIgnoresJSONKeyOrder covers what SQLite cannot reproduce:
// definition_json is jsonb on PostgreSQL, so the value read back has been
// normalised and its keys reordered. Comparing raw strings would bump
// definition_version on every re-apply.
func TestUpsertScheduleIgnoresJSONKeyOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, db := setupScheduleStore(t)

	first, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate what jsonb hands back: same document, different key order and
	// whitespace.
	reordered := `{"stages":[{"stageHandlerName":"DispatchDueEmailsHandler","input":"{\"batchSize\":200}","stageName":"dispatch"}],  "name":"dispatch-due-emails","apiKey":""}`
	if _, err = db.Exec(`UPDATE schedule SET definition_json = $1 WHERE id = $2`, reordered, first.ID); err != nil {
		t.Fatalf("rewrite definition: %v", err)
	}

	second, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if second.DefinitionVersion != first.DefinitionVersion {
		t.Fatalf("version moved from %d to %d because the stored JSON was reordered",
			first.DefinitionVersion, second.DefinitionVersion)
	}
}

func TestCanonicalJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a    string
		b    string
		same bool
	}{
		{
			name: "key order does not matter",
			a:    `{"a":1,"b":2}`,
			b:    `{"b":2,"a":1}`,
			same: true,
		},
		{
			name: "whitespace does not matter",
			a:    `{"a": 1}`,
			b:    `{"a":1}`,
			same: true,
		},
		{
			name: "array order does matter",
			a:    `[1,2]`,
			b:    `[2,1]`,
			same: false,
		},
		{
			name: "different values differ",
			a:    `{"a":1}`,
			b:    `{"a":2}`,
			same: false,
		},
		{
			name: "unparseable input falls back to a byte comparison",
			a:    `not json`,
			b:    `not json`,
			same: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := canonicalJSON(tc.a) == canonicalJSON(tc.b)
			if got != tc.same {
				t.Fatalf("canonicalJSON(%q) == canonicalJSON(%q) is %v, want %v", tc.a, tc.b, got, tc.same)
			}
		})
	}
}

func TestScheduleStatusTransitions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, _ := setupScheduleStore(t)

	if _, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails")); err != nil {
		t.Fatalf("create: %v", err)
	}

	paused, err := st.SetScheduleStatus(ctx, 1, "emails", types.ScheduleStatusPaused, nil)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if paused.Status != types.ScheduleStatusPaused {
		t.Fatalf("status = %q, want Paused", paused.Status)
	}

	// Resuming must move the cursor forward, not replay the pause window.
	resumeAt := time.Now().UTC().Add(30 * time.Minute)
	resumed, err := st.SetScheduleStatus(ctx, 1, "emails", types.ScheduleStatusActive, &resumeAt)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Status != types.ScheduleStatusActive {
		t.Fatalf("status = %q, want Active", resumed.Status)
	}
	if resumed.NextRunAt == nil || !resumed.NextRunAt.Equal(resumeAt) {
		t.Fatalf("nextRunAt = %v, want %s", resumed.NextRunAt, resumeAt)
	}

	if _, err = st.SetScheduleStatus(ctx, 1, "missing", types.ScheduleStatusPaused, nil); !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("err = %v, want ErrScheduleNotFound", err)
	}
}

// TestRecordScheduleRunIsIdempotentPerTick is the guarantee the whole design rests
// on: two scheduler instances racing on one database cannot fire the same
// (schedule, scheduled_for) twice.
func TestRecordScheduleRunIsIdempotentPerTick(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, db := setupScheduleStore(t)

	schedule, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	tick := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	record := ScheduleRunRecord{
		ScheduleID:        schedule.ID,
		ScheduledFor:      tick,
		Trigger:           types.ScheduleTriggerCron,
		Outcome:           types.ScheduleRunOutcomeCreated,
		DefinitionVersion: 1,
	}

	const instances = 8
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		duplicate int
		other     []error
	)
	start := make(chan struct{})

	for i := 0; i < instances; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, runErr := st.RecordScheduleRun(ctx, record)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case runErr == nil:
				succeeded++
			case errors.Is(runErr, ErrScheduleRunAlreadyRecorded):
				duplicate++
			default:
				other = append(other, runErr)
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("unexpected errors: %v", other)
	}
	if succeeded != 1 {
		t.Fatalf("%d instances recorded the tick, want exactly 1 (%d saw a duplicate)",
			succeeded, duplicate)
	}

	var rows int
	if err = db.Get(&rows, `SELECT COUNT(*) FROM schedule_run WHERE schedule_id = $1`, schedule.ID); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if rows != 1 {
		t.Fatalf("schedule_run has %d rows for one tick, want 1", rows)
	}
}

// TestClaimDueSchedulesAdvancesTheCursorOnce proves the other half: after one
// instance claims a due schedule, a second pass sees nothing due, so the tick is
// planned exactly once even before the uniqueness guard is consulted.
func TestClaimDueSchedulesAdvancesTheCursorOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, _ := setupScheduleStore(t)

	due := time.Now().UTC().Add(-time.Minute)
	upsert := baseUpsert(t, "emails")
	upsert.NextRunAt = &due
	schedule, _, err := st.UpsertSchedule(ctx, 1, upsert)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	advance := func(row ScheduleRow) (SchedulePlan, error) {
		next := now.Add(time.Minute)
		return SchedulePlan{Ticks: []time.Time{row.NextRunAt.Time.UTC()}, NextRunAt: &next}, nil
	}

	first, err := st.ClaimDueSchedules(ctx, now, 10, advance)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if len(first) != 1 || first[0].Schedule.ID != schedule.ID {
		t.Fatalf("first claim = %+v, want the due schedule", first)
	}

	second, err := st.ClaimDueSchedules(ctx, now, 10, advance)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("the cursor was not advanced: a second pass claimed %d schedules", len(second))
	}
}

func TestClaimDueSchedulesSkipsInactiveAndUnplanned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, db := setupScheduleStore(t)

	due := time.Now().UTC().Add(-time.Minute)

	active := baseUpsert(t, "active")
	active.NextRunAt = &due
	if _, _, err := st.UpsertSchedule(ctx, 1, active); err != nil {
		t.Fatalf("create active: %v", err)
	}

	paused := baseUpsert(t, "paused")
	paused.NextRunAt = &due
	if _, _, err := st.UpsertSchedule(ctx, 1, paused); err != nil {
		t.Fatalf("create paused: %v", err)
	}
	if _, err := st.SetScheduleStatus(ctx, 1, "paused", types.ScheduleStatusPaused, nil); err != nil {
		t.Fatalf("pause: %v", err)
	}

	unplanned := baseUpsert(t, "unplanned")
	unplanned.NextRunAt = nil
	if _, _, err := st.UpsertSchedule(ctx, 1, unplanned); err != nil {
		t.Fatalf("create unplanned: %v", err)
	}
	if _, err := db.Exec(`UPDATE schedule SET next_run_at = NULL WHERE name = 'unplanned'`); err != nil {
		t.Fatalf("clear cursor: %v", err)
	}

	claimed, err := st.ClaimDueSchedules(ctx, time.Now().UTC(), 10,
		func(row ScheduleRow) (SchedulePlan, error) {
			next := time.Now().UTC().Add(time.Hour)
			return SchedulePlan{NextRunAt: &next}, nil
		})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Schedule.Name != "active" {
		names := make([]string, 0, len(claimed))
		for _, item := range claimed {
			names = append(names, item.Schedule.Name)
		}
		t.Fatalf("claimed %v, want only [active]", names)
	}
}

func TestClaimDueSchedulesParksAnUnplannableSchedule(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, db := setupScheduleStore(t)

	due := time.Now().UTC().Add(-time.Minute)
	upsert := baseUpsert(t, "broken")
	upsert.NextRunAt = &due
	if _, _, err := st.UpsertSchedule(ctx, 1, upsert); err != nil {
		t.Fatalf("create: %v", err)
	}

	// A schedule whose planner errors must not be re-claimed forever.
	if _, err := st.ClaimDueSchedules(ctx, time.Now().UTC(), 10,
		func(ScheduleRow) (SchedulePlan, error) {
			return SchedulePlan{}, errors.New("expression no longer parses")
		}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	var nextRunAt sql.NullTime
	if err := db.Get(&nextRunAt, `SELECT next_run_at FROM schedule WHERE name = 'broken'`); err != nil {
		t.Fatalf("load cursor: %v", err)
	}
	if nextRunAt.Valid {
		t.Fatalf("an unplannable schedule must be parked, cursor = %v", nextRunAt.Time)
	}

	claimed, err := st.ClaimDueSchedules(ctx, time.Now().UTC(), 10,
		func(ScheduleRow) (SchedulePlan, error) { return SchedulePlan{}, nil })
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("a parked schedule must not be claimed again")
	}
}

func TestListSchedulesAndRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, db := setupScheduleStore(t)

	schedule, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		outcome := types.ScheduleRunOutcomeCreated
		if i == 1 {
			outcome = types.ScheduleRunOutcomeSkipped
		}
		if _, err = st.RecordScheduleRun(ctx, ScheduleRunRecord{
			ScheduleID:        schedule.ID,
			ScheduledFor:      base.Add(time.Duration(i) * time.Minute),
			Trigger:           types.ScheduleTriggerCron,
			Outcome:           outcome,
			DefinitionVersion: 1,
		}); err != nil {
			t.Fatalf("record run %d: %v", i, err)
		}
	}

	runs, err := st.ListScheduleRuns(ctx, schedule.ID, "", 50)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs.Runs) != 3 {
		t.Fatalf("len(runs) = %d, want 3", len(runs.Runs))
	}
	// Newest first.
	if !runs.Runs[0].ScheduledFor.After(runs.Runs[2].ScheduledFor) {
		t.Fatalf("runs are not newest first: %v", runs.Runs)
	}
	for _, run := range runs.Runs {
		if run.LagSeconds < 0 {
			t.Fatalf("lag must not be negative: %f", run.LagSeconds)
		}
	}

	list, err := st.ListSchedules(ctx, 1, "", "", 50)
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(list.Schedules) != 1 {
		t.Fatalf("len(schedules) = %d, want 1", len(list.Schedules))
	}

	if err = st.AttachScheduleHealth(ctx, list.Schedules); err != nil {
		t.Fatalf("attach health: %v", err)
	}
	health := list.Schedules[0].Health
	if health == nil || health.RecentRuns != 3 || health.RecentSkipped != 1 {
		t.Fatalf("health = %+v, want 3 runs with 1 skipped", health)
	}
	if len(health.Outcomes) != 3 {
		t.Fatalf("outcome strip has %d entries, want 3", len(health.Outcomes))
	}

	// A schedule of another application must not appear.
	if _, err = db.Exec(`INSERT INTO application (id, name) VALUES (2, 'other')`); err != nil {
		t.Fatalf("seed application: %v", err)
	}
	if _, _, err = st.UpsertSchedule(ctx, 2, baseUpsert(t, "emails")); err != nil {
		t.Fatalf("create foreign: %v", err)
	}
	scoped, err := st.ListSchedules(ctx, 1, "", "", 50)
	if err != nil {
		t.Fatalf("list scoped: %v", err)
	}
	if len(scoped.Schedules) != 1 || scoped.Schedules[0].ApplicationID != 1 {
		t.Fatalf("listing leaked another application's schedules: %+v", scoped.Schedules)
	}
}

func TestUnfinishedSchedulePipeline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, db := setupScheduleStore(t)

	schedule, _, err := st.UpsertSchedule(ctx, 1, baseUpsert(t, "emails"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	running, err := st.UnfinishedSchedulePipeline(ctx, schedule.ID)
	if err != nil || running != nil {
		t.Fatalf("running=%v err=%v, want nil and nil", running, err)
	}

	if _, err = db.Exec(`
		INSERT INTO pipeline (name, status, created_at, is_completed, application_id, schedule_id)
		VALUES ('run-1', 'Completed', $1, true, 1, $2), ('run-2', 'Running', $1, false, 1, $2)
	`, time.Now().UTC(), schedule.ID); err != nil {
		t.Fatalf("seed pipelines: %v", err)
	}

	running, err = st.UnfinishedSchedulePipeline(ctx, schedule.ID)
	if err != nil {
		t.Fatalf("unfinished: %v", err)
	}
	if running == nil {
		t.Fatalf("the running pipeline must be found")
	}

	last, err := st.LastScheduledPipelineID(ctx, schedule.ID)
	if err != nil || last == nil || *last != *running {
		t.Fatalf("last = %v err = %v, want the newest pipeline %v", last, err, running)
	}
}

func timePointer(t time.Time) *time.Time { return &t }
