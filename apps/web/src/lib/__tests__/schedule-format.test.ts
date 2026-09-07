import { describe, it, expect } from 'vitest';
import { describeSchedule, describeDuration } from '@/lib/schedule-format';
import type { Schedule } from '@/types/schedules';

const base = { name: 's', version: 1, enabled: true } as unknown as Schedule;

describe('describeDuration', () => {
  it('picks the largest whole unit', () => {
    expect(describeDuration(30)).toBe('30 seconds');
    expect(describeDuration(60)).toBe('minute');
    expect(describeDuration(120)).toBe('2 minutes');
    expect(describeDuration(3600)).toBe('hour');
    expect(describeDuration(86400)).toBe('day');
    expect(describeDuration(172800)).toBe('2 days');
  });
});

describe('describeSchedule', () => {
  it('renders intervals in words', () => {
    expect(describeSchedule({ ...base, kind: 'Interval', intervalSeconds: 300 })).toBe('every 5 minutes');
  });

  it('recognises common cron shorthands and keeps the zone', () => {
    expect(describeSchedule({ ...base, kind: 'Cron', cronExpression: '@hourly', timeZone: 'UTC' })).toBe('every hour');
    expect(describeSchedule({ ...base, kind: 'Cron', cronExpression: '0 0 * * *', timeZone: 'Europe/Madrid' })).toBe('daily 00:00 Europe/Madrid');
    expect(describeSchedule({ ...base, kind: 'Cron', cronExpression: '30 9 * * *', timeZone: 'UTC' })).toBe('daily 09:30 UTC');
  });

  it('falls back to the raw expression it cannot summarise', () => {
    expect(describeSchedule({ ...base, kind: 'Cron', cronExpression: '*/5 9-17 * * 1-5', timeZone: 'UTC' })).toBe('*/5 9-17 * * 1-5 UTC');
  });
});
