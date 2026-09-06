package schedule

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"pipelogiq/internal/types"
)

func intPtr(v int) *int { return &v }

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("time zone %s unavailable on this host: %v", name, err)
	}
	return loc
}

func TestSpecValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec Spec
		want error
	}{
		{
			name: "five field cron",
			spec: Spec{Kind: types.ScheduleKindCron, CronExpression: "*/5 * * * *"},
		},
		{
			name: "descriptor cron",
			spec: Spec{Kind: types.ScheduleKindCron, CronExpression: "@daily"},
		},
		{
			name: "every descriptor",
			spec: Spec{Kind: types.ScheduleKindCron, CronExpression: "@every 10s"},
		},
		{
			name: "six field cron is rejected",
			spec: Spec{Kind: types.ScheduleKindCron, CronExpression: "0 */5 * * * *"},
			want: ErrInvalidCron,
		},
		{
			name: "nonsense cron",
			spec: Spec{Kind: types.ScheduleKindCron, CronExpression: "not a cron"},
			want: ErrInvalidCron,
		},
		{
			name: "unknown zone",
			spec: Spec{Kind: types.ScheduleKindCron, CronExpression: "@daily", TimeZone: "Mars/Olympus"},
			want: ErrUnknownTimeZone,
		},
		{
			name: "interval at the floor",
			spec: Spec{Kind: types.ScheduleKindInterval, IntervalSeconds: intPtr(MinIntervalSeconds)},
		},
		{
			name: "interval below the floor",
			spec: Spec{Kind: types.ScheduleKindInterval, IntervalSeconds: intPtr(MinIntervalSeconds - 1)},
			want: ErrIntervalTooSmall,
		},
		{
			name: "interval missing",
			spec: Spec{Kind: types.ScheduleKindInterval},
			want: ErrIntervalTooSmall,
		},
		{
			name: "once without runAt",
			spec: Spec{Kind: types.ScheduleKindOnce},
			want: ErrMissingRunAt,
		},
		{
			name: "unknown kind",
			spec: Spec{Kind: "Whenever"},
			want: ErrUnknownKind,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.spec.Validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSpecNext(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 10, 12, 0, 30, 0, time.UTC)

	cases := []struct {
		name string
		spec Spec
		from time.Time
		want time.Time
		ok   bool
	}{
		{
			name: "every minute rounds up",
			spec: Spec{Kind: types.ScheduleKindCron, CronExpression: "* * * * *"},
			from: base,
			want: time.Date(2026, 3, 10, 12, 1, 0, 0, time.UTC),
			ok:   true,
		},
		{
			name: "daily in UTC",
			spec: Spec{Kind: types.ScheduleKindCron, CronExpression: "0 3 * * *"},
			from: base,
			want: time.Date(2026, 3, 11, 3, 0, 0, 0, time.UTC),
			ok:   true,
		},
		{
			name: "interval adds exactly",
			spec: Spec{Kind: types.ScheduleKindInterval, IntervalSeconds: intPtr(90)},
			from: base,
			want: base.Add(90 * time.Second),
			ok:   true,
		},
		{
			name: "once in the future",
			spec: Spec{Kind: types.ScheduleKindOnce, RunAt: timePtr(base.Add(time.Hour))},
			from: base,
			want: base.Add(time.Hour),
			ok:   true,
		},
		{
			name: "once already past has no next tick",
			spec: Spec{Kind: types.ScheduleKindOnce, RunAt: timePtr(base.Add(-time.Hour))},
			from: base,
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok, err := tc.spec.Next(tc.from)
			if err != nil {
				t.Fatalf("Next() error: %v", err)
			}
			if ok != tc.ok {
				t.Fatalf("Next() ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if !got.Equal(tc.want) {
				t.Fatalf("Next() = %s, want %s", got.Format(time.RFC3339Nano), tc.want.Format(time.RFC3339Nano))
			}
			if got.Location() != time.UTC {
				t.Fatalf("Next() must return UTC, got %s", got.Location())
			}
		})
	}
}

// Europe/Madrid moves to CEST on the last Sunday of March and back on the last
// Sunday of October. These are the two cases where "daily at 03:00 local" is not
// simply "every 24 hours", and both behaviours are contractual: they are what
// docs/schedules.md tells an operator to expect.
func TestSpecNextAcrossDST(t *testing.T) {
	t.Parallel()
	madrid := mustLoad(t, "Europe/Madrid")

	// next collects the following n ticks from the given instant.
	next := func(t *testing.T, spec Spec, from time.Time, n int) []time.Time {
		t.Helper()
		out := make([]time.Time, 0, n)
		cursor := from
		for i := 0; i < n; i++ {
			tick, ok, err := spec.Next(cursor)
			if err != nil || !ok {
				t.Fatalf("Next() ok=%v err=%v", ok, err)
			}
			out = append(out, tick)
			cursor = tick
		}
		return out
	}

	t.Run("a local time that does not exist is skipped that day", func(t *testing.T) {
		t.Parallel()
		// 2026-03-29 02:00 local jumps to 03:00 CEST, so 02:30 never happens.
		spec := Spec{Kind: types.ScheduleKindCron, CronExpression: "30 2 * * *", TimeZone: "Europe/Madrid"}
		ticks := next(t, spec, time.Date(2026, 3, 27, 12, 0, 0, 0, madrid), 2)

		if got := ticks[0].In(madrid).Format("2006-01-02T15:04"); got != "2026-03-28T02:30" {
			t.Fatalf("first tick = %s, want 2026-03-28T02:30 local", got)
		}
		if got := ticks[1].In(madrid).Format("2006-01-02T15:04"); got != "2026-03-30T02:30" {
			t.Fatalf("second tick = %s, want 2026-03-30T02:30 local (the 29th has no 02:30)", got)
		}
	})

	t.Run("a local time that occurs twice runs twice", func(t *testing.T) {
		t.Parallel()
		// 2026-10-25 03:00 CEST falls back to 02:00 CET, so 02:30 local happens twice.
		spec := Spec{Kind: types.ScheduleKindCron, CronExpression: "30 2 * * *", TimeZone: "Europe/Madrid"}
		ticks := next(t, spec, time.Date(2026, 10, 24, 12, 0, 0, 0, madrid), 2)

		if got := ticks[0].Format(time.RFC3339); got != "2026-10-25T00:30:00Z" {
			t.Fatalf("first tick = %s, want 2026-10-25T00:30:00Z", got)
		}
		if got := ticks[1].Format(time.RFC3339); got != "2026-10-25T01:30:00Z" {
			t.Fatalf("second tick = %s, want 2026-10-25T01:30:00Z", got)
		}
		// Distinct UTC instants, so the (schedule, scheduled_for) guard keeps both.
		if ticks[0].Equal(ticks[1]) {
			t.Fatalf("the two passes must be distinct UTC ticks")
		}
		for _, tick := range ticks {
			if got := tick.In(madrid).Format("15:04"); got != "02:30" {
				t.Fatalf("tick %s is %s local, want 02:30", tick, got)
			}
		}
	})

	t.Run("a local time outside the transition hour is stable", func(t *testing.T) {
		t.Parallel()
		spec := Spec{Kind: types.ScheduleKindCron, CronExpression: "0 3 * * *", TimeZone: "Europe/Madrid"}

		for _, from := range []time.Time{
			time.Date(2026, 3, 27, 12, 0, 0, 0, madrid),
			time.Date(2026, 10, 23, 12, 0, 0, 0, madrid),
		} {
			ticks := next(t, spec, from, 4)
			for i, tick := range ticks {
				if got := tick.In(madrid).Format("15:04"); got != "03:00" {
					t.Fatalf("tick %d from %s is %s local, want 03:00", i, from, got)
				}
				if i > 0 && !tick.After(ticks[i-1]) {
					t.Fatalf("ticks must advance: %s then %s", ticks[i-1], tick)
				}
			}
		}
	})

	t.Run("interval ignores local time entirely", func(t *testing.T) {
		t.Parallel()
		spec := Spec{Kind: types.ScheduleKindInterval, IntervalSeconds: intPtr(3600), TimeZone: "Europe/Madrid"}
		from := time.Date(2026, 3, 29, 0, 30, 0, 0, madrid)

		tick, ok, err := spec.Next(from)
		if err != nil || !ok {
			t.Fatalf("Next() = %v %v", ok, err)
		}
		if got := tick.Sub(from); got != time.Hour {
			t.Fatalf("interval advanced %s, want exactly 1h across the DST boundary", got)
		}
	})
}

func TestMissedTicks(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	t.Run("nothing is due yet", func(t *testing.T) {
		t.Parallel()
		spec := Spec{Kind: types.ScheduleKindCron, CronExpression: "* * * * *"}
		ticks, truncated, err := spec.MissedTicks(base.Add(time.Minute), base)
		if err != nil || truncated || len(ticks) != 0 {
			t.Fatalf("ticks=%v truncated=%v err=%v", ticks, truncated, err)
		}
	})

	t.Run("enumerates every missed minute oldest first", func(t *testing.T) {
		t.Parallel()
		spec := Spec{Kind: types.ScheduleKindCron, CronExpression: "* * * * *"}
		ticks, truncated, err := spec.MissedTicks(base, base.Add(4*time.Minute+30*time.Second))
		if err != nil || truncated {
			t.Fatalf("truncated=%v err=%v", truncated, err)
		}
		if len(ticks) != 5 {
			t.Fatalf("len(ticks) = %d, want 5: %v", len(ticks), ticks)
		}
		for i := range ticks {
			want := base.Add(time.Duration(i) * time.Minute)
			if !ticks[i].Equal(want) {
				t.Fatalf("ticks[%d] = %s, want %s", i, ticks[i], want)
			}
		}
	})

	t.Run("a long outage is capped rather than unbounded", func(t *testing.T) {
		t.Parallel()
		spec := Spec{Kind: types.ScheduleKindInterval, IntervalSeconds: intPtr(MinIntervalSeconds)}
		ticks, truncated, err := spec.MissedTicks(base, base.Add(30*24*time.Hour))
		if err != nil {
			t.Fatalf("MissedTicks: %v", err)
		}
		if !truncated {
			t.Fatalf("a 30-day outage at a 10s interval must report truncation")
		}
		if len(ticks) != MaxCatchupTicks {
			t.Fatalf("len(ticks) = %d, want %d", len(ticks), MaxCatchupTicks)
		}
	})

	t.Run("a once schedule has exactly one tick", func(t *testing.T) {
		t.Parallel()
		runAt := base.Add(-time.Hour)
		spec := Spec{Kind: types.ScheduleKindOnce, RunAt: &runAt}
		ticks, _, err := spec.MissedTicks(runAt, base)
		if err != nil {
			t.Fatalf("MissedTicks: %v", err)
		}
		if len(ticks) != 1 || !ticks[0].Equal(runAt) {
			t.Fatalf("ticks = %v, want exactly [%s]", ticks, runAt)
		}
	})
}

func TestWithJitter(t *testing.T) {
	t.Parallel()

	tick := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewSource(1))

	t.Run("zero jitter is exact", func(t *testing.T) {
		spec := Spec{JitterSeconds: 0}
		if got := spec.WithJitter(tick, rng); !got.Equal(tick) {
			t.Fatalf("WithJitter = %s, want %s", got, tick)
		}
	})

	t.Run("stays inside 0..jitterSeconds", func(t *testing.T) {
		spec := Spec{JitterSeconds: 15}
		for i := 0; i < 500; i++ {
			got := spec.WithJitter(tick, rng)
			offset := got.Sub(tick)
			if offset < 0 || offset > 15*time.Second {
				t.Fatalf("offset %s outside [0s, 15s]", offset)
			}
		}
	})
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	runAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		spec Spec
		want string
	}{
		{Spec{Kind: types.ScheduleKindCron, CronExpression: "* * * * *"}, "every minute"},
		{Spec{Kind: types.ScheduleKindCron, CronExpression: "@hourly"}, "every hour"},
		{Spec{Kind: types.ScheduleKindCron, CronExpression: "0 3 * * *", TimeZone: "Europe/Madrid"}, "daily 03:00 Europe/Madrid"},
		{Spec{Kind: types.ScheduleKindCron, CronExpression: "@every 10s"}, "every 10 seconds"},
		{Spec{Kind: types.ScheduleKindCron, CronExpression: "*/5 * * * *", TimeZone: "UTC"}, "*/5 * * * * UTC"},
		{Spec{Kind: types.ScheduleKindInterval, IntervalSeconds: intPtr(60)}, "every minute"},
		{Spec{Kind: types.ScheduleKindInterval, IntervalSeconds: intPtr(7200)}, "every 2 hours"},
		{Spec{Kind: types.ScheduleKindOnce, RunAt: &runAt}, "once at 2026-05-01T10:00:00Z"},
	}

	for _, tc := range cases {
		if got := Describe(tc.spec); got != tc.want {
			t.Fatalf("Describe(%+v) = %q, want %q", tc.spec, got, tc.want)
		}
	}
}

func timePtr(t time.Time) *time.Time { return &t }
