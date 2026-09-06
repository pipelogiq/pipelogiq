import type { Schedule, ScheduleRun } from '@/types/schedules';

/** describeSchedule renders a schedule's trigger in words, mirroring
 *  internal/schedule.Describe so the UI and the API agree. */
export function describeSchedule(schedule: Schedule): string {
  if (schedule.kind === 'Interval') {
    return schedule.intervalSeconds
      ? `every ${describeDuration(schedule.intervalSeconds)}`
      : 'interval';
  }
  if (schedule.kind === 'Once') {
    return schedule.runAt ? `once at ${formatAbsolute(schedule.runAt)}` : 'once';
  }

  const zone = schedule.timeZone || 'UTC';
  const expression = (schedule.cronExpression || '').trim();
  switch (expression) {
    case '* * * * *':
      return 'every minute';
    case '@hourly':
    case '0 * * * *':
      return 'every hour';
    case '@daily':
    case '@midnight':
    case '0 0 * * *':
      return `daily 00:00 ${zone}`;
    default:
      break;
  }

  const fields = expression.split(/\s+/);
  if (
    fields.length === 5 &&
    fields.slice(2).every((f) => f === '*') &&
    /^\d+$/.test(fields[0]) &&
    /^\d+$/.test(fields[1])
  ) {
    return `daily ${pad(fields[1])}:${pad(fields[0])} ${zone}`;
  }
  return `${expression} ${zone}`;
}

export function describeDuration(seconds: number): string {
  if (seconds % 86400 === 0 && seconds >= 86400) return plural(seconds / 86400, 'day');
  if (seconds % 3600 === 0 && seconds >= 3600) return plural(seconds / 3600, 'hour');
  if (seconds % 60 === 0 && seconds >= 60) return plural(seconds / 60, 'minute');
  return plural(seconds, 'second');
}

function plural(n: number, unit: string): string {
  return n === 1 ? unit : `${n} ${unit}s`;
}

function pad(value: string): string {
  return value.padStart(2, '0');
}

export function formatAbsolute(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString();
}

/** formatRelative renders a signed distance from now, e.g. "in 42s" or "3m ago". */
export function formatRelative(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';

  const deltaMs = date.getTime() - Date.now();
  const suffix = deltaMs >= 0 ? '' : ' ago';
  const prefix = deltaMs >= 0 ? 'in ' : '';
  return `${prefix}${formatDurationMs(Math.abs(deltaMs))}${suffix}`;
}

export function formatDurationMs(ms: number): string {
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.round(hours / 24)}d`;
}

export function formatLag(seconds: number): string {
  if (!Number.isFinite(seconds)) return '—';
  if (seconds < 1) return '<1s';
  return formatDurationMs(seconds * 1000);
}

export function runDuration(run: ScheduleRun): string {
  if (!run.pipelineFinishedAt) return '—';
  const finished = new Date(run.pipelineFinishedAt).getTime();
  const started = new Date(run.firedAt).getTime();
  if (Number.isNaN(finished) || Number.isNaN(started) || finished < started) return '—';
  return formatDurationMs(finished - started);
}

/**
 * scheduleHealthLabel turns the recent-run summary into the one line an operator
 * needs: recent failures first, then lag, then "healthy".
 */
export function scheduleHealthLabel(schedule: Schedule): {
  label: string;
  tone: 'ok' | 'warn' | 'bad';
} {
  const health = schedule.health;
  if (!health || health.recentRuns === 0) {
    return { label: 'no runs yet', tone: 'ok' };
  }
  if (health.recentFailed > 0) {
    return {
      label: `${health.recentFailed} of last ${health.recentRuns} failed`,
      tone: health.recentFailed >= health.recentRuns / 2 ? 'bad' : 'warn',
    };
  }
  if (typeof health.lagSeconds === 'number' && health.lagSeconds > 60) {
    return { label: `lagging ${formatLag(health.lagSeconds)}`, tone: 'warn' };
  }
  if (health.recentSkipped > 0) {
    return { label: `${health.recentSkipped} of last ${health.recentRuns} skipped`, tone: 'ok' };
  }
  return { label: 'healthy', tone: 'ok' };
}
