import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertTriangle,
  CalendarClock,
  CheckCircle2,
  Loader2,
  Pause,
  Play,
  Timer,
} from 'lucide-react';

import { AppHeader } from '@/components/layout/AppHeader';
import { RunOutcomeStrip } from '@/components/schedules/RunOutcomeStrip';
import { Badge } from '@/components/ui/badge';
import { KpiCard } from '@/components/ui/kpi-card';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useToast } from '@/hooks/use-toast';
import { useSchedules, useSetScheduleStatus } from '@/hooks/use-schedules';
import {
  describeSchedule,
  formatAbsolute,
  formatRelative,
  scheduleHealthLabel,
} from '@/lib/schedule-format';
import { cn } from '@/lib/utils';
import type { Schedule, ScheduleStatus } from '@/types/schedules';

const statusFilters: { label: string; value: ScheduleStatus | 'all' }[] = [
  { label: 'All', value: 'all' },
  { label: 'Active', value: 'Active' },
  { label: 'Paused', value: 'Paused' },
  { label: 'Archived', value: 'Archived' },
];

const healthToneClass = {
  ok: 'text-muted-foreground',
  warn: 'text-status-warning',
  bad: 'text-destructive',
} as const;

export default function Schedules() {
  const [statusFilter, setStatusFilter] = useState<ScheduleStatus | 'all'>('all');
  const { data, isLoading, isError, error } = useSchedules(
    statusFilter === 'all' ? undefined : statusFilter,
  );
  const setStatus = useSetScheduleStatus();
  const { toast } = useToast();

  const schedules = useMemo(() => data?.schedules ?? [], [data]);

  const counts = useMemo(() => {
    let active = 0;
    let paused = 0;
    let failing = 0;
    for (const schedule of schedules) {
      if (schedule.status === 'Active') active += 1;
      if (schedule.status === 'Paused') paused += 1;
      if ((schedule.health?.recentFailed ?? 0) > 0) failing += 1;
    }
    return { active, paused, failing };
  }, [schedules]);

  const nextRun = useMemo(() => {
    const upcoming = schedules
      .filter((s) => s.status === 'Active' && s.nextRunAt)
      .map((s) => s.nextRunAt as string)
      .sort();
    return upcoming[0];
  }, [schedules]);

  const handleToggle = (schedule: Schedule, shouldRun: boolean) => {
    setStatus.mutate(
      { id: schedule.id, action: shouldRun ? 'resume' : 'pause' },
      {
        onSuccess: () =>
          toast({
            title: shouldRun ? 'Schedule resumed' : 'Schedule paused',
            description: shouldRun
              ? `${schedule.name} will run again from its next tick.`
              : `${schedule.name} will not create new pipelines.`,
          }),
        onError: (mutationError) =>
          toast({
            title: 'Could not update the schedule',
            description: mutationError.message,
            variant: 'destructive',
          }),
      },
    );
  };

  return (
    <div className="flex flex-1 flex-col">
      <AppHeader
        title="Schedules"
        subtitle="Recurring triggers that create ordinary pipelines"
      />

      <div className="flex-1 space-y-6 p-6">
        <div className="grid gap-4 md:grid-cols-4">
          <KpiCard title="Active" value={counts.active} icon={Play} />
          <KpiCard title="Paused" value={counts.paused} icon={Pause} />
          <KpiCard
            title="With recent failures"
            value={counts.failing}
            icon={counts.failing > 0 ? AlertTriangle : CheckCircle2}
            tone="critical"
          />
          <KpiCard
            title="Next run"
            value={nextRun ? formatRelative(nextRun) : '—'}
            subtitle={nextRun ? formatAbsolute(nextRun) : 'nothing scheduled'}
            icon={Timer}
          />
        </div>

        <div className="flex items-center justify-between">
          <Select
            value={statusFilter}
            onValueChange={(value) => setStatusFilter(value as ScheduleStatus | 'all')}
          >
            <SelectTrigger className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {statusFilters.map((filter) => (
                <SelectItem key={filter.value} value={filter.value}>
                  {filter.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="rounded-xl border border-border bg-card">
          {isLoading ? (
            <div className="space-y-3 p-6">
              {[0, 1, 2].map((row) => (
                <Skeleton key={row} className="h-10 w-full" />
              ))}
            </div>
          ) : isError ? (
            <div className="p-6 text-sm text-destructive">
              Could not load schedules: {error?.message}
            </div>
          ) : schedules.length === 0 ? (
            <div className="flex flex-col items-center gap-2 p-12 text-center">
              <CalendarClock className="h-8 w-8 text-muted-foreground" />
              <p className="text-sm font-medium">No schedules yet</p>
              <p className="max-w-md text-sm text-muted-foreground">
                Register one from the SDK with <code>ScheduleBuilder</code>, or over
                HTTP with <code>PUT /schedules/&#123;name&#125;</code>. Every firing
                creates an ordinary pipeline.
              </p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Trigger</TableHead>
                  <TableHead>Next run</TableHead>
                  <TableHead>Last run</TableHead>
                  <TableHead>Recent</TableHead>
                  <TableHead>Health</TableHead>
                  <TableHead className="text-right">Enabled</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {schedules.map((schedule) => {
                  const health = scheduleHealthLabel(schedule);
                  const archived = schedule.status === 'Archived';
                  return (
                    <TableRow key={schedule.id}>
                      <TableCell>
                        <Link
                          to={`/schedules/${schedule.id}`}
                          className="font-medium text-foreground hover:underline"
                        >
                          {schedule.name}
                        </Link>
                        {archived && (
                          <Badge variant="outline" className="ml-2">
                            Archived
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {describeSchedule(schedule)}
                      </TableCell>
                      <TableCell className="text-sm">
                        {schedule.status === 'Active' && schedule.nextRunAt ? (
                          <span title={formatAbsolute(schedule.nextRunAt)}>
                            {formatRelative(schedule.nextRunAt)}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="text-sm">
                        {schedule.lastRunAt ? (
                          <span title={formatAbsolute(schedule.lastRunAt)}>
                            {formatRelative(schedule.lastRunAt)}
                            {schedule.lastRunStatus === 'Failed' && (
                              <Badge variant="destructive" className="ml-2">
                                Failed
                              </Badge>
                            )}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">never</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <RunOutcomeStrip outcomes={schedule.health?.outcomes ?? []} />
                      </TableCell>
                      <TableCell className={cn('text-sm', healthToneClass[health.tone])}>
                        {health.label}
                      </TableCell>
                      <TableCell className="text-right">
                        <Switch
                          checked={schedule.status === 'Active'}
                          disabled={archived || setStatus.isPending}
                          onCheckedChange={(checked) => handleToggle(schedule, checked)}
                          aria-label={`Toggle ${schedule.name}`}
                        />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </div>

        {setStatus.isPending && (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" /> Updating…
          </p>
        )}
      </div>
    </div>
  );
}
