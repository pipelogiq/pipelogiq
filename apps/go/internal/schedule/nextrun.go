// Package schedule computes when a schedule fires next and which ticks it missed.
// It owns no state and touches no database: the scheduler goroutine and the API
// validation path both use it, and it is the part worth testing exhaustively.
package schedule

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"pipelogiq/internal/types"
)

// MinIntervalSeconds is the floor for Interval schedules. Sub-10-second precision
// is explicitly not a goal; the scheduler ticks once a second.
const MinIntervalSeconds = 10

// MaxCatchupTicks caps how many missed ticks one pass will enumerate, so a
// schedule that was down for a month cannot allocate an unbounded slice.
const MaxCatchupTicks = 1000

// cronParser accepts the standard 5-field syntax plus the @every/@daily/@hourly
// descriptors. Only the parser is used; robfig's runner is not.
var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

var (
	// ErrInvalidCron reports an unparseable cron expression.
	ErrInvalidCron = errors.New("invalid cron expression")
	// ErrUnknownTimeZone reports an IANA zone the host cannot load.
	ErrUnknownTimeZone = errors.New("unknown time zone")
	// ErrIntervalTooSmall reports an interval below MinIntervalSeconds.
	ErrIntervalTooSmall = errors.New("interval is below the minimum")
	// ErrMissingRunAt reports a Once schedule without runAt.
	ErrMissingRunAt = errors.New("runAt is required for a Once schedule")
	// ErrUnknownKind reports an unrecognised schedule kind.
	ErrUnknownKind = errors.New("unknown schedule kind")
)

// Spec is everything needed to place a schedule's ticks on the timeline.
type Spec struct {
	Kind            string
	CronExpression  string
	TimeZone        string
	IntervalSeconds *int
	RunAt           *time.Time
	JitterSeconds   int
}

// Location resolves the schedule's IANA zone, defaulting to UTC.
func (s Spec) Location() (*time.Location, error) {
	name := strings.TrimSpace(s.TimeZone)
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTimeZone, name)
	}
	return loc, nil
}

// Validate reports every reason the spec cannot be scheduled.
func (s Spec) Validate() error {
	if _, err := s.Location(); err != nil {
		return err
	}

	switch s.Kind {
	case types.ScheduleKindCron:
		if _, err := cronParser.Parse(strings.TrimSpace(s.CronExpression)); err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidCron, err.Error())
		}
	case types.ScheduleKindInterval:
		if s.IntervalSeconds == nil || *s.IntervalSeconds < MinIntervalSeconds {
			return fmt.Errorf("%w: minimum is %d seconds", ErrIntervalTooSmall, MinIntervalSeconds)
		}
	case types.ScheduleKindOnce:
		if s.RunAt == nil {
			return ErrMissingRunAt
		}
	default:
		return fmt.Errorf("%w: %q", ErrUnknownKind, s.Kind)
	}

	if s.JitterSeconds < 0 {
		return errors.New("jitterSeconds must not be negative")
	}
	return nil
}

// Next returns the first tick strictly after from, in UTC.
//
// Cron ticks are computed in the schedule's own zone and stored in UTC, so a
// "daily at 03:00 Europe/Madrid" schedule stays at 03:00 local across a DST
// boundary. The two awkward local times behave as robfig's cron does, and both
// are surprising enough to be worth stating plainly:
//
//   - A local time inside the hour that does not exist on the spring-forward day
//     is SKIPPED that day. "02:30 Europe/Madrid" does not run at all on
//     2026-03-29; the next run is 2026-03-30.
//   - A local time inside the hour that occurs twice on the autumn-back day RUNS
//     TWICE. "02:30 Europe/Madrid" runs at 00:30Z and again at 01:30Z on
//     2026-10-25. They are distinct UTC ticks, so the (schedule, scheduled_for)
//     uniqueness guard does not collapse them.
//
// Pick a local time outside 02:00-03:00 for a daily schedule, or use UTC, if
// neither is acceptable. Interval schedules are unaffected: they add wall-clock
// seconds and never consult the zone.
//
// A Once schedule has no tick after its runAt; Next reports ok=false.
func (s Spec) Next(from time.Time) (time.Time, bool, error) {
	loc, err := s.Location()
	if err != nil {
		return time.Time{}, false, err
	}
	local := from.In(loc)

	switch s.Kind {
	case types.ScheduleKindCron:
		sched, parseErr := cronParser.Parse(strings.TrimSpace(s.CronExpression))
		if parseErr != nil {
			return time.Time{}, false, fmt.Errorf("%w: %s", ErrInvalidCron, parseErr.Error())
		}
		next := sched.Next(local)
		if next.IsZero() {
			return time.Time{}, false, nil
		}
		return next.UTC(), true, nil

	case types.ScheduleKindInterval:
		if s.IntervalSeconds == nil || *s.IntervalSeconds < MinIntervalSeconds {
			return time.Time{}, false, fmt.Errorf("%w: minimum is %d seconds", ErrIntervalTooSmall, MinIntervalSeconds)
		}
		return from.Add(time.Duration(*s.IntervalSeconds) * time.Second).UTC(), true, nil

	case types.ScheduleKindOnce:
		if s.RunAt == nil {
			return time.Time{}, false, ErrMissingRunAt
		}
		runAt := s.RunAt.UTC()
		if runAt.After(from) {
			return runAt, true, nil
		}
		return time.Time{}, false, nil

	default:
		return time.Time{}, false, fmt.Errorf("%w: %q", ErrUnknownKind, s.Kind)
	}
}

// MissedTicks returns every tick in (nextRunAt-1ns, now], oldest first, starting
// with nextRunAt itself. It is capped at MaxCatchupTicks; the boolean reports
// whether the cap truncated the list.
//
// An Interval schedule that has been down for a long time would otherwise produce
// one tick per interval for the whole outage, which is exactly what the cap is for.
func (s Spec) MissedTicks(nextRunAt, now time.Time) ([]time.Time, bool, error) {
	if nextRunAt.IsZero() || nextRunAt.After(now) {
		return nil, false, nil
	}

	ticks := []time.Time{nextRunAt.UTC()}
	cursor := nextRunAt
	for len(ticks) < MaxCatchupTicks {
		next, ok, err := s.Next(cursor)
		if err != nil {
			return nil, false, err
		}
		if !ok || next.After(now) {
			return ticks, false, nil
		}
		ticks = append(ticks, next)
		cursor = next
	}
	return ticks, true, nil
}

// WithJitter offsets a tick by a random 0..JitterSeconds. The tick recorded as
// scheduled_for stays exact; only the moment the pipeline is created moves, which
// is what spreads load across instances.
func (s Spec) WithJitter(tick time.Time, rng *rand.Rand) time.Time {
	if s.JitterSeconds <= 0 {
		return tick
	}
	offset := time.Duration(rng.Intn(s.JitterSeconds+1)) * time.Second
	return tick.Add(offset)
}

// Describe renders a schedule in words for the dashboard list.
func Describe(s Spec) string {
	switch s.Kind {
	case types.ScheduleKindCron:
		return describeCron(strings.TrimSpace(s.CronExpression), s.TimeZone)
	case types.ScheduleKindInterval:
		if s.IntervalSeconds == nil {
			return "interval"
		}
		return "every " + describeDuration(time.Duration(*s.IntervalSeconds)*time.Second)
	case types.ScheduleKindOnce:
		if s.RunAt == nil {
			return "once"
		}
		return "once at " + s.RunAt.UTC().Format(time.RFC3339)
	default:
		return s.Kind
	}
}

func describeCron(expression, zone string) string {
	if zone == "" {
		zone = "UTC"
	}
	switch expression {
	case "* * * * *":
		return "every minute"
	case "@hourly", "0 * * * *":
		return "every hour"
	case "@daily", "@midnight", "0 0 * * *":
		return "daily 00:00 " + zone
	}
	if strings.HasPrefix(expression, "@every ") {
		if d, err := time.ParseDuration(strings.TrimPrefix(expression, "@every ")); err == nil {
			return "every " + describeDuration(d)
		}
	}
	fields := strings.Fields(expression)
	if len(fields) == 5 && fields[2] == "*" && fields[3] == "*" && fields[4] == "*" &&
		!strings.ContainsAny(fields[0]+fields[1], "*/,-") {
		return fmt.Sprintf("daily %02s:%02s %s", fields[1], fields[0], zone)
	}
	return expression + " " + zone
}

func describeDuration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0 && d >= 24*time.Hour:
		return plural(int(d/(24*time.Hour)), "day")
	case d%time.Hour == 0 && d >= time.Hour:
		return plural(int(d/time.Hour), "hour")
	case d%time.Minute == 0 && d >= time.Minute:
		return plural(int(d/time.Minute), "minute")
	default:
		return plural(int(d/time.Second), "second")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
