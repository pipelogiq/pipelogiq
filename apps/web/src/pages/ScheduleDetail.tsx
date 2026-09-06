import { useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Loader2, Pause, Play, Archive, PlayCircle } from 'lucide-react';

import { AppHeader } from '@/components/layout/AppHeader';
import { RunOutcomeStrip } from '@/components/schedules/RunOutcomeStrip';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { Textarea } from '@/components/ui/textarea';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useToast } from '@/hooks/use-toast';
import {
  useSchedule,
  useScheduleRuns,
  useSetScheduleStatus,
  useTriggerSchedule,
} from '@/hooks/use-schedules';
import {
  describeSchedule,
  formatAbsolute,
  formatLag,
  formatRelative,
  runDuration,
  scheduleHealthLabel,
} from '@/lib/schedule-format';
import { cn } from '@/lib/utils';
import type { ScheduleRunOutcome } from '@/types/schedules';

const outcomeVariant: Record<ScheduleRunOutcome, 'default' | 'destructive' | 'outline'> = {
  Created: 'default',
  Failed: 'destructive',
  Skipped: 'outline',
};

const healthToneClass = {
  ok: 'text-muted-foreground',
  warn: 'text-amber-600 dark:text-amber-400',
  bad: 'text-destructive',
} as const;

export default function ScheduleDetail() {
  const { id } = useParams<{ id: string }>();
  const scheduleId = Number(id);
  const { data: schedule, isLoading, isError, error } = useSchedule(scheduleId);
  const { data: runs, isLoading: runsLoading } = useScheduleRuns(scheduleId);
  const setStatus = useSetScheduleStatus();
  const trigger = useTriggerSchedule();
  const { toast } = useToast();

  const [runDialogOpen, setRunDialogOpen] = useState(false);
  const [inputOverride, setInputOverride] = useState('');
  const [inputError, setInputError] = useState('');

  const health = useMemo(
    () => (schedule ? scheduleHealthLabel(schedule) : null),
    [schedule],
  );

  if (isLoading) {
    return (
      <div className="flex flex-1 flex-col">
        <AppHeader title="Schedule" />
        <div className="space-y-4 p-6">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-64 w-full" />
        </div>
      </div>
    );
  }

  if (isError || !schedule) {
    return (
      <div className="flex flex-1 flex-col">
        <AppHeader title="Schedule" />
        <div className="p-6 text-sm text-destructive">
          Could not load this schedule: {error?.message ?? 'not found'}
        </div>
      </div>
    );
  }

  const archived = schedule.status === 'Archived';

  const changeStatus = (action: 'pause' | 'resume' | 'archive') => {
    setStatus.mutate(
      { id: schedule.id, action },
      {
        onSuccess: () => toast({ title: `Schedule ${action}d` }),
        onError: (mutationError) =>
          toast({
            title: 'Could not update the schedule',
            description: mutationError.message,
            variant: 'destructive',
          }),
      },
    );
  };

  const runNow = () => {
    let parsedInput: unknown;
    if (inputOverride.trim() !== '') {
      try {
        parsedInput = JSON.parse(inputOverride);
      } catch {
        setInputError('Input must be valid JSON.');
        return;
      }
    }
    setInputError('');

    trigger.mutate(
      { id: schedule.id, input: parsedInput },
      {
        onSuccess: (result) => {
          setRunDialogOpen(false);
          setInputOverride('');
          toast({
            title: 'Run created',
            description: `Pipeline ${result.pipelineId} is queued.`,
          });
        },
        onError: (mutationError) =>
          toast({
            title: 'Could not create the run',
            description: mutationError.message,
            variant: 'destructive',
          }),
      },
    );
  };

  return (
    <div className="flex flex-1 flex-col">
      <AppHeader title={schedule.name} subtitle={describeSchedule(schedule)} />

      <div className="flex-1 space-y-6 p-6">
        <div className="flex items-center justify-between">
          <Button variant="ghost" size="sm" asChild>
            <Link to="/schedules">
              <ArrowLeft className="mr-2 h-4 w-4" />
              All schedules
            </Link>
          </Button>

          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={archived || trigger.isPending}
              onClick={() => setRunDialogOpen(true)}
            >
              <PlayCircle className="mr-2 h-4 w-4" />
              Run now
            </Button>
            {schedule.status === 'Active' ? (
              <Button
                size="sm"
                variant="outline"
                disabled={setStatus.isPending}
                onClick={() => changeStatus('pause')}
              >
                <Pause className="mr-2 h-4 w-4" />
                Pause
              </Button>
            ) : (
              <Button
                size="sm"
                variant="outline"
                disabled={setStatus.isPending}
                onClick={() => changeStatus('resume')}
              >
                <Play className="mr-2 h-4 w-4" />
                Resume
              </Button>
            )}
            <Button
              size="sm"
              variant="outline"
              disabled={archived || setStatus.isPending}
              onClick={() => changeStatus('archive')}
            >
              <Archive className="mr-2 h-4 w-4" />
              Archive
            </Button>
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <section className="rounded-xl border border-border bg-card p-6">
            <h2 className="mb-4 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Trigger and policies
            </h2>
            <dl className="grid grid-cols-2 gap-3 text-sm">
              <Field label="Status">
                <Badge variant={schedule.status === 'Active' ? 'default' : 'outline'}>
                  {schedule.status}
                </Badge>
              </Field>
              <Field label="Kind">{schedule.kind}</Field>
              {schedule.cronExpression && (
                <Field label="Expression">
                  <code>{schedule.cronExpression}</code>
                </Field>
              )}
              {typeof schedule.intervalSeconds === 'number' && (
                <Field label="Interval">{schedule.intervalSeconds}s</Field>
              )}
              {schedule.runAt && <Field label="Run at">{formatAbsolute(schedule.runAt)}</Field>}
              <Field label="Time zone">{schedule.timeZone}</Field>
              <Field label="Overlap">{schedule.overlapPolicy}</Field>
              <Field label="Catch-up">
                {schedule.catchupPolicy}
                {schedule.catchupPolicy === 'All' && ` (max ${schedule.catchupMax})`}
              </Field>
              <Field label="Jitter">{schedule.jitterSeconds}s</Field>
              <Field label="Next run">
                {schedule.status === 'Active' && schedule.nextRunAt
                  ? `${formatRelative(schedule.nextRunAt)} · ${formatAbsolute(schedule.nextRunAt)}`
                  : '—'}
              </Field>
              <Field label="Version">
                v{schedule.definitionVersion}
                {schedule.createdBy && (
                  <span className="text-muted-foreground"> · {schedule.createdBy}</span>
                )}
              </Field>
              <Field label="Health">
                <span className={cn(health && healthToneClass[health.tone])}>
                  {health?.label}
                </span>
              </Field>
              <Field label="Recent">
                <RunOutcomeStrip outcomes={schedule.health?.outcomes ?? []} />
              </Field>
            </dl>
          </section>

          <section className="rounded-xl border border-border bg-card p-6">
            <h2 className="mb-4 text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Pipeline definition
            </h2>
            <p className="mb-3 text-sm">
              <span className="text-muted-foreground">Name: </span>
              {schedule.definition?.name}
            </p>
            <ol className="space-y-2">
              {(schedule.definition?.stages ?? []).map((stage, index) => (
                <li
                  key={`${stage.stageName}-${index}`}
                  className="rounded-lg border border-border/60 p-3 text-sm"
                >
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{index + 1}</span>
                    <span className="font-medium">{stage.stageName}</span>
                    <Badge variant="outline">{stage.stageHandlerName}</Badge>
                  </div>
                  {stage.input && (
                    <pre className="mt-2 overflow-x-auto rounded bg-muted p-2 text-xs">
                      {stage.input}
                    </pre>
                  )}
                </li>
              ))}
            </ol>
          </section>
        </div>

        <section className="rounded-xl border border-border bg-card">
          <div className="border-b border-border px-6 py-4">
            <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
              Run history
            </h2>
          </div>
          {runsLoading ? (
            <div className="space-y-3 p-6">
              {[0, 1, 2].map((row) => (
                <Skeleton key={row} className="h-8 w-full" />
              ))}
            </div>
          ) : (runs?.runs ?? []).length === 0 ? (
            <p className="p-6 text-sm text-muted-foreground">No runs recorded yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Scheduled</TableHead>
                  <TableHead>Fired</TableHead>
                  <TableHead>Lag</TableHead>
                  <TableHead>Duration</TableHead>
                  <TableHead>Trigger</TableHead>
                  <TableHead>Outcome</TableHead>
                  <TableHead>Pipeline</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(runs?.runs ?? []).map((run) => (
                  <TableRow key={run.id}>
                    <TableCell className="text-sm">{formatAbsolute(run.scheduledFor)}</TableCell>
                    <TableCell className="text-sm">{formatAbsolute(run.firedAt)}</TableCell>
                    <TableCell className="text-sm">{formatLag(run.lagSeconds)}</TableCell>
                    <TableCell className="text-sm">{runDuration(run)}</TableCell>
                    <TableCell className="text-sm">{run.trigger}</TableCell>
                    <TableCell>
                      <Badge variant={outcomeVariant[run.outcome]}>
                        {run.outcome}
                        {run.skipReason ? ` · ${run.skipReason}` : ''}
                      </Badge>
                      {run.error && (
                        <p className="mt-1 max-w-md truncate text-xs text-destructive" title={run.error}>
                          {run.error}
                        </p>
                      )}
                    </TableCell>
                    <TableCell className="text-sm">
                      {run.pipelineId ? (
                        <Link
                          to={`/pipelines/${run.pipelineId}`}
                          className="text-foreground hover:underline"
                        >
                          #{run.pipelineId}
                          {run.pipelineStatus && (
                            <span className="ml-2 text-muted-foreground">{run.pipelineStatus}</span>
                          )}
                        </Link>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </section>
      </div>

      <Dialog open={runDialogOpen} onOpenChange={setRunDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Run {schedule.name} now</DialogTitle>
            <DialogDescription>
              Creates a Manual run immediately. It does not move the schedule&apos;s next
              run, and overlap and catch-up policies do not apply to it.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-2">
            <label className="text-sm font-medium" htmlFor="schedule-input-override">
              First stage input (optional JSON)
            </label>
            <Textarea
              id="schedule-input-override"
              rows={6}
              value={inputOverride}
              placeholder={schedule.definition?.stages?.[0]?.input ?? '{}'}
              onChange={(event) => setInputOverride(event.target.value)}
            />
            {inputError && <p className="text-sm text-destructive">{inputError}</p>}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setRunDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={runNow} disabled={trigger.isPending}>
              {trigger.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Run now
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-muted-foreground">{label}</dt>
      <dd className="mt-1">{children}</dd>
    </div>
  );
}
