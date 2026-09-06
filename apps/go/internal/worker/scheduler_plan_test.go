package worker

import (
	"database/sql"
	"math/rand"
	"testing"
	"time"

	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

func schedulerTestRand() *rand.Rand { return rand.New(rand.NewSource(7)) }

func cronRow(catchup string, catchupMax int, nextRunAt time.Time) store.ScheduleRow {
	return store.ScheduleRow{
		ID:                1,
		Name:              "every-minute",
		Kind:              types.ScheduleKindCron,
		CronExpression:    sql.NullString{String: "* * * * *", Valid: true},
		TimeZone:          "UTC",
		CatchupPolicy:     catchup,
		CatchupMax:        catchupMax,
		OverlapPolicy:     types.OverlapPolicySkip,
		DefinitionVersion: 1,
		NextRunAt:         sql.NullTime{Time: nextRunAt, Valid: true},
	}
}

func TestPlanScheduleCatchupPolicies(t *testing.T) {
	t.Parallel()

	// The schedule was due at 10:00 and the scheduler only looks at 10:04:30, so
	// five ticks (10:00 through 10:04) are outstanding.
	due := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	now := due.Add(4*time.Minute + 30*time.Second)

	cases := []struct {
		name        string
		catchup     string
		catchupMax  int
		wantFired   []time.Time
		wantSkipped int
	}{
		{
			name:        "None fires only the latest missed tick",
			catchup:     types.CatchupPolicyNone,
			wantFired:   []time.Time{due.Add(4 * time.Minute)},
			wantSkipped: 4,
		},
		{
			name:        "One fires only the latest, labelled as catch-up",
			catchup:     types.CatchupPolicyOne,
			wantFired:   []time.Time{due.Add(4 * time.Minute)},
			wantSkipped: 4,
		},
		{
			name:       "All fires every missed tick within the budget",
			catchup:    types.CatchupPolicyAll,
			catchupMax: 10,
			wantFired: []time.Time{
				due,
				due.Add(time.Minute),
				due.Add(2 * time.Minute),
				due.Add(3 * time.Minute),
				due.Add(4 * time.Minute),
			},
			wantSkipped: 0,
		},
		{
			name:       "All records the ticks beyond catchupMax as skipped",
			catchup:    types.CatchupPolicyAll,
			catchupMax: 2,
			wantFired: []time.Time{
				due.Add(3 * time.Minute),
				due.Add(4 * time.Minute),
			},
			wantSkipped: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := planSchedule(cronRow(tc.catchup, tc.catchupMax, due), now, schedulerTestRand())
			if err != nil {
				t.Fatalf("planSchedule: %v", err)
			}
			if len(plan.Ticks) != len(tc.wantFired) {
				t.Fatalf("fired %d ticks (%v), want %d", len(plan.Ticks), plan.Ticks, len(tc.wantFired))
			}
			for i, tick := range plan.Ticks {
				if !tick.Equal(tc.wantFired[i]) {
					t.Fatalf("tick[%d] = %s, want %s", i, tick, tc.wantFired[i])
				}
			}
			if len(plan.SkippedTicks) != tc.wantSkipped {
				t.Fatalf("skipped %d ticks, want %d", len(plan.SkippedTicks), tc.wantSkipped)
			}
			// Every outstanding tick is accounted for, either fired or recorded.
			if len(plan.Ticks)+len(plan.SkippedTicks) != 5 {
				t.Fatalf("ticks are unaccounted for: %d fired + %d skipped",
					len(plan.Ticks), len(plan.SkippedTicks))
			}
			if plan.NextRunAt == nil {
				t.Fatalf("a cron schedule must always have a next run")
			}
			if !plan.NextRunAt.After(now.Add(-time.Minute)) {
				t.Fatalf("nextRunAt = %s did not advance past the backlog", plan.NextRunAt)
			}
		})
	}
}

func TestPlanScheduleNothingDue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	plan, err := planSchedule(cronRow(types.CatchupPolicyNone, 10, now.Add(time.Minute)), now, schedulerTestRand())
	if err != nil {
		t.Fatalf("planSchedule: %v", err)
	}
	if len(plan.Ticks) != 0 || len(plan.SkippedTicks) != 0 {
		t.Fatalf("a schedule that is not due must fire nothing: %+v", plan)
	}
	if plan.NextRunAt == nil || !plan.NextRunAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("nextRunAt must stay put, got %v", plan.NextRunAt)
	}
}

func TestPlanScheduleOnceArchivesItself(t *testing.T) {
	t.Parallel()

	runAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	row := store.ScheduleRow{
		ID:            2,
		Name:          "one-shot",
		Kind:          types.ScheduleKindOnce,
		TimeZone:      "UTC",
		CatchupPolicy: types.CatchupPolicyNone,
		OverlapPolicy: types.OverlapPolicySkip,
		RunAt:         sql.NullTime{Time: runAt, Valid: true},
		NextRunAt:     sql.NullTime{Time: runAt, Valid: true},
	}

	plan, err := planSchedule(row, runAt.Add(time.Second), schedulerTestRand())
	if err != nil {
		t.Fatalf("planSchedule: %v", err)
	}
	if len(plan.Ticks) != 1 || !plan.Ticks[0].Equal(runAt) {
		t.Fatalf("ticks = %v, want exactly [%s]", plan.Ticks, runAt)
	}
	if plan.NextRunAt != nil {
		t.Fatalf("a Once schedule must not plan another run, got %s", plan.NextRunAt)
	}
	if plan.Status != types.ScheduleStatusArchived {
		t.Fatalf("status = %q, want Archived", plan.Status)
	}
}

func TestPlanScheduleJitterStaysInBounds(t *testing.T) {
	t.Parallel()

	due := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	row := cronRow(types.CatchupPolicyNone, 10, due)
	row.JitterSeconds = 15
	rng := schedulerTestRand()

	for i := 0; i < 200; i++ {
		plan, err := planSchedule(row, due.Add(time.Second), rng)
		if err != nil {
			t.Fatalf("planSchedule: %v", err)
		}
		if plan.NextRunAt == nil {
			t.Fatalf("nextRunAt must be set")
		}
		// The unjittered next tick after 10:00 is 10:01.
		offset := plan.NextRunAt.Sub(due.Add(time.Minute))
		if offset < 0 || offset > 15*time.Second {
			t.Fatalf("jitter offset %s outside [0s, 15s]", offset)
		}
	}
}

func TestPlanScheduleLongOutageDoesNotStall(t *testing.T) {
	t.Parallel()

	// A 10-second interval schedule that was down for a month produces far more
	// ticks than the enumeration cap. The cursor must still land in the future.
	due := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	now := due.Add(30 * 24 * time.Hour)
	interval := 10
	row := store.ScheduleRow{
		ID:              3,
		Name:            "chatty",
		Kind:            types.ScheduleKindInterval,
		TimeZone:        "UTC",
		IntervalSeconds: sql.NullInt64{Int64: int64(interval), Valid: true},
		CatchupPolicy:   types.CatchupPolicyNone,
		OverlapPolicy:   types.OverlapPolicySkip,
		NextRunAt:       sql.NullTime{Time: due, Valid: true},
	}

	plan, err := planSchedule(row, now, schedulerTestRand())
	if err != nil {
		t.Fatalf("planSchedule: %v", err)
	}
	if plan.NextRunAt == nil {
		t.Fatalf("nextRunAt must be set")
	}
	if !plan.NextRunAt.After(now) {
		t.Fatalf("nextRunAt = %s is not after now = %s; the scheduler would spin",
			plan.NextRunAt, now)
	}
}

func TestTriggerForKind(t *testing.T) {
	t.Parallel()

	due := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	t.Run("an on-time cron tick is Cron", func(t *testing.T) {
		row := cronRow(types.CatchupPolicyNone, 10, due)
		plan := store.SchedulePlan{Ticks: []time.Time{due}}
		if got := triggerForKind(row, due, plan); got != types.ScheduleTriggerCron {
			t.Fatalf("trigger = %q, want Cron", got)
		}
	})

	t.Run("catch-up One labels the fired tick Catchup", func(t *testing.T) {
		row := cronRow(types.CatchupPolicyOne, 10, due)
		plan := store.SchedulePlan{
			Ticks:        []time.Time{due.Add(4 * time.Minute)},
			SkippedTicks: []time.Time{due},
		}
		if got := triggerForKind(row, plan.Ticks[0], plan); got != types.ScheduleTriggerCatchup {
			t.Fatalf("trigger = %q, want Catchup", got)
		}
	})

	t.Run("catch-up All labels all but the newest tick Catchup", func(t *testing.T) {
		row := cronRow(types.CatchupPolicyAll, 10, due)
		plan := store.SchedulePlan{Ticks: []time.Time{due, due.Add(time.Minute)}}
		if got := triggerForKind(row, plan.Ticks[0], plan); got != types.ScheduleTriggerCatchup {
			t.Fatalf("older tick trigger = %q, want Catchup", got)
		}
		if got := triggerForKind(row, plan.Ticks[1], plan); got != types.ScheduleTriggerCron {
			t.Fatalf("newest tick trigger = %q, want Cron", got)
		}
	})

	t.Run("interval and once carry their own trigger", func(t *testing.T) {
		interval := cronRow(types.CatchupPolicyNone, 10, due)
		interval.Kind = types.ScheduleKindInterval
		plan := store.SchedulePlan{Ticks: []time.Time{due}}
		if got := triggerForKind(interval, due, plan); got != types.ScheduleTriggerInterval {
			t.Fatalf("trigger = %q, want Interval", got)
		}

		once := interval
		once.Kind = types.ScheduleKindOnce
		if got := triggerForKind(once, due, plan); got != types.ScheduleTriggerOnce {
			t.Fatalf("trigger = %q, want Once", got)
		}
	})
}
