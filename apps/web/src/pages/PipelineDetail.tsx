import { Fragment, type ReactNode } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ArrowLeft, ChevronDown, ChevronRight, Copy, FileJson, ListTree, Loader2, Pause, Play, RefreshCw, RotateCcw, SkipForward } from 'lucide-react';
import { ApiError } from '@/api/client';
import { AppHeader } from '@/components/layout/AppHeader';
import { ReadOnlyNotice } from '@/components/auth/ReadOnlyNotice';
import { PipelinePayload } from '@/components/pipelines/PipelinePayload';
import { PipelineRunLogs } from '@/components/pipelines/PipelineRunLogs';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { PipelineContextTable } from '@/components/pipelines/PipelineContextTable';
import { StatusBadge } from '@/components/ui/status-badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useAuth } from '@/contexts/AuthContext';
import { usePipelineDetail } from '@/hooks/use-pipeline-detail';
import { usePausePipeline, useResumePipeline, useRerunStage, useSkipStage } from '@/hooks/use-pipelines';
import { useToast } from '@/hooks/use-toast';
import { cn } from '@/lib/utils';
import { mapPipelineStatusToUI, mapStageStatusToUI, type PipelineResponse, type StageResponse } from '@/types/api';

const tabs = ['stages', 'logs', 'context', 'data'];

function positiveId(value: string | null | undefined) {
  if (!value || !/^[1-9]\d*$/.test(value)) return undefined;
  const id = Number(value);
  return Number.isSafeInteger(id) ? id : undefined;
}

function timestamp(value?: string) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isFinite(date.getTime())
    ? date.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })
    : '—';
}

function duration(start?: string, end?: string) {
  if (!start) return '—';
  const elapsed = (end ? Date.parse(end) : Date.now()) - Date.parse(start);
  if (!Number.isFinite(elapsed) || elapsed < 0) return '—';
  if (elapsed < 1000) return Math.round(elapsed) + ' ms';
  if (elapsed < 60000) return (elapsed / 1000).toFixed(1) + ' s';
  if (elapsed < 3600000) return Math.floor(elapsed / 60000) + ' m ' + Math.floor(elapsed % 60000 / 1000) + ' s';
  return Math.floor(elapsed / 3600000) + ' h ' + Math.floor(elapsed % 3600000 / 60000) + ' m';
}

function statusLabel(status?: string) {
  return (status || 'NotStarted').replace(/([a-z])([A-Z])/g, '$1 $2');
}

function RunStatus({ pipeline }: { pipeline: PipelineResponse }) {
  return <StatusBadge status={pipeline.status === 'Cancelled' ? 'default' : mapPipelineStatusToUI(pipeline.status)} pulse={pipeline.status === 'Running'}>
    {statusLabel(pipeline.status)}
  </StatusBadge>;
}

function StageStatus({ stage }: { stage: StageResponse }) {
  return <StatusBadge size="sm" status={stage.status === 'Cancelled' ? 'default' : mapStageStatusToUI(stage.status)} pulse={stage.status === 'Running'}>
    {statusLabel(stage.isSkipped ? 'Skipped' : stage.status)}
  </StatusBadge>;
}

function Metric({ label, children }: { label: string; children: ReactNode }) {
  return <div className="min-w-0 space-y-1">
    <dt className="text-xs text-muted-foreground">{label}</dt>
    <dd className="break-words text-sm [overflow-wrap:anywhere]">{children}</dd>
  </div>;
}

export default function PipelineDetail() {
  const { id } = useParams();
  const pipelineId = positiveId(id);
  const query = usePipelineDetail(pipelineId ?? 0);
  const [searchParams, setSearchParams] = useSearchParams();
  const { canOperate } = useAuth();
  const { toast } = useToast();
  const pause = usePausePipeline();
  const resume = useResumePipeline();
  const rerun = useRerunStage();
  const skip = useSkipStage();
  const tab = tabs.includes(searchParams.get('tab') || '') ? searchParams.get('tab')! : 'stages';
  const requestedStage = positiveId(searchParams.get('stage'));
  const busy = pause.isPending || resume.isPending || rerun.isPending || skip.isPending;

  function setParam(key: string, value?: string) {
    setSearchParams(previous => {
      const next = new URLSearchParams(previous);
      if (value) next.set(key, value);
      else next.delete(key);
      return next;
    });
  }

  async function operate(action: () => Promise<unknown>, title: string) {
    if (!canOperate || busy) return;
    try {
      await action();
      toast({ title });
    } catch (error) {
      toast({ title: 'Operation failed', description: error instanceof Error ? error.message : 'Please try again.', variant: 'destructive' });
    }
  }

  async function copyLink() {
    try {
      await navigator.clipboard.writeText(window.location.href);
      toast({ title: 'Link copied' });
    } catch {
      toast({ title: 'Could not copy link', description: 'Copy the address from your browser.', variant: 'destructive' });
    }
  }

  const backLink = <Link to="/pipelines" className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground">
    <ArrowLeft className="h-4 w-4" /> Back to pipelines
  </Link>;

  if (!pipelineId || query.isError || (!query.isPending && !query.data)) {
    const missing = !pipelineId || (query.error instanceof ApiError && [403, 404].includes(query.error.status));
    return <div className="min-w-0">
      <AppHeader title="Pipeline run" />
      <div className="space-y-6 p-4 sm:p-6">
        {backLink}
        <div role="alert" className="space-y-3 rounded-xl border border-border bg-card p-6">
          <h2 className="font-semibold">{missing ? 'Pipeline unavailable' : 'Could not load pipeline'}</h2>
          <p className="text-sm text-muted-foreground">{missing ? 'This run does not exist or you do not have access to it.' : 'The server could not return this run. Try again.'}</p>
          {pipelineId && <Button variant="outline" size="sm" onClick={() => void query.refetch()} disabled={query.isFetching}>Try again</Button>}
        </div>
      </div>
    </div>;
  }

  if (!query.data) return <div className="min-w-0">
    <AppHeader title="Pipeline run" />
    <div role="status" className="flex items-center justify-center gap-2 p-16 text-sm text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /> Loading pipeline…</div>
  </div>;

  const pipeline = query.data;
  const stages = pipeline.stages ?? [];
  const selectedStage = searchParams.get('stage') === 'none'
    ? undefined
    : requestedStage
      ? stages.find(stage => stage.id === requestedStage)
      : stages.find(stage => stage.status === 'Failed') ?? stages.find(stage => stage.status === 'Running');
  const terminal = pipeline.isTerminal ?? ['Completed', 'Failed', 'Cancelled'].includes(pipeline.status);
  const startedAt = stages.map(stage => stage.startedAt).filter((value): value is string => !!value && Number.isFinite(Date.parse(value))).sort((a, b) => Date.parse(a) - Date.parse(b))[0];
  const finishedAt = terminal ? pipeline.finishedAt : undefined;
  const completed = stages.filter(stage => stage.status === 'Completed' && !stage.isSkipped).length;
  const skipped = stages.filter(stage => stage.status === 'Skipped' || stage.isSkipped).length;
  const failed = stages.filter(stage => stage.status === 'Failed').length;
  const logCount = stages.reduce((count, stage) => count + (stage.logs?.length ?? 0), 0);
  const context = pipeline.pipelineContextItems ?? [];
  const keywords = pipeline.pipelineKeywords ?? [];
  const canPause = !terminal && ['NotStarted', 'Pending', 'Running'].includes(pipeline.status);
  const canResume = !terminal && pipeline.status === 'Paused';

  function openStageLogs(stageId: number) {
    setSearchParams(previous => {
      const next = new URLSearchParams(previous);
      next.set('tab', 'logs');
      next.set('stage', String(stageId));
      return next;
    });
  }

  return <div className="min-w-0">
    <AppHeader title="Pipeline run" subtitle={'#' + pipeline.id} />
    <div className="space-y-5 p-4 sm:p-6">
      <section aria-label="Run summary" className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          {backLink}
          <div className="flex items-center gap-2">
            {canOperate && (canPause || canResume) && <Button variant="outline" size="sm" className="h-8 text-xs" disabled={busy}
              onClick={() => void operate(() => canResume ? resume.mutateAsync(pipeline.id) : pause.mutateAsync(pipeline.id), canResume ? 'Pipeline resumed' : 'Pipeline paused')}>
              {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : canResume ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
              {canResume ? 'Resume pipeline' : 'Pause pipeline'}
            </Button>}
            <Button variant="ghost" size="sm" className="h-8 text-xs" onClick={copyLink}><Copy className="h-3.5 w-3.5" /> Copy link</Button>
            <Button variant="ghost" size="sm" className="h-8 text-xs" onClick={() => void query.refetch()} disabled={query.isFetching}>
              <RefreshCw className={cn('h-3.5 w-3.5', query.isFetching && 'animate-spin')} /> Refresh
            </Button>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h2 className="min-w-0 break-words text-xl font-semibold tracking-tight [overflow-wrap:anywhere]">{pipeline.name}</h2>
          <RunStatus pipeline={pipeline} />
          {pipeline.isEvent && <Badge variant="outline">Event</Badge>}
        </div>
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-xs text-muted-foreground">
          <span>Application <span className="ml-1 text-foreground">{pipeline.applicationId !== undefined ? '#' + pipeline.applicationId : '—'}</span></span>
          <span>Created <span className="ml-1 text-foreground">{timestamp(pipeline.createdAt)}</span></span>
          <span>{terminal ? 'Duration' : 'Elapsed'} <span className="ml-1 font-medium tabular-nums text-foreground">{terminal && !finishedAt ? '—' : duration(startedAt, finishedAt)}</span></span>
          <span>{completed} of {stages.length} stages completed{skipped > 0 && ` · ${skipped} skipped`}{failed > 0 && <span className="text-status-error"> · {failed} failed</span>}</span>
        </div>
        <details className="group/run-info">
          <summary className="inline-flex cursor-pointer list-none items-center gap-1 text-xs text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
            <ChevronRight className="h-3.5 w-3.5 transition-transform group-open/run-info:rotate-90" /> Run info
          </summary>
          <div className="mt-3 space-y-3 border-l-2 border-border pl-4">
            <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2 xl:grid-cols-4">
              <Metric label="Run ID">#{pipeline.id}</Metric>
              <Metric label="Created">{timestamp(pipeline.createdAt)}</Metric>
              <Metric label="Started">{timestamp(startedAt)}</Metric>
              <Metric label="Finished">{timestamp(finishedAt)}</Metric>
              {pipeline.traceId && <Metric label="Trace ID"><span className="select-all font-mono text-xs">{pipeline.traceId}</span></Metric>}
              {pipeline.idempotencyKey && <Metric label="Idempotency key"><span className="select-all font-mono text-xs">{pipeline.idempotencyKey}</span></Metric>}
            </dl>
            {keywords.length > 0 && <div className="flex flex-wrap gap-2">{keywords.map((keyword, index) => <Badge key={index} variant="outline" className="max-w-full whitespace-normal break-all font-normal">{keyword.key ? keyword.key + ': ' : ''}{keyword.value}</Badge>)}</div>}
          </div>
        </details>
      </section>
      <ReadOnlyNotice />

      <Tabs value={tab} onValueChange={value => setParam('tab', value)} activationMode="manual" className="min-w-0 overflow-hidden rounded-lg border border-border bg-card">
        <div className="overflow-x-auto border-b border-border px-4">
          <TabsList aria-label="Pipeline details" className="min-w-max border-0">
            <TabsTrigger value="stages" className="gap-2 py-3"><ListTree className="h-4 w-4" /> Stages ({stages.length})</TabsTrigger>
            <TabsTrigger value="logs" className="py-3">Logs ({logCount})</TabsTrigger>
            <TabsTrigger value="context" className="py-3">Context ({context.length})</TabsTrigger>
            <TabsTrigger value="data" className="gap-2 py-3"><FileJson className="h-4 w-4" /> All data</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="stages" className="mt-0">
          {requestedStage && !selectedStage && <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3" role="status">
            <p className="text-sm text-muted-foreground">This stage is not part of this run.</p>
            <Button size="sm" variant="outline" onClick={() => setParam('stage')}>Select a stage</Button>
          </div>}
          {!stages.length ? <EmptyState message="No stages have been added to this run." /> : <div className="overflow-x-auto">
            <table aria-label="Pipeline stages" className="w-full min-w-[760px] table-fixed text-left text-sm">
              <thead className="border-b border-border bg-muted/30 text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="px-4 py-2.5 font-medium">Stage</th>
                  <th scope="col" className="w-36 px-3 py-2.5 font-medium">Status</th>
                  <th scope="col" className="w-32 px-3 py-2.5 font-medium">Started</th>
                  <th scope="col" className="w-28 px-3 py-2.5 font-medium">Duration</th>
                  <th scope="col" className="w-24 px-3 py-2.5 font-medium">Attempt</th>
                  <th scope="col" className="w-20 px-3 py-2.5 text-right font-medium">Logs</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {stages.map((stage, index) => {
                  const expanded = selectedStage?.id === stage.id;
                  const toggleStage = () => setParam('stage', expanded ? 'none' : String(stage.id));
                  const elapsed = stage.finishedAt ? duration(stage.startedAt, stage.finishedAt) : stage.status === 'Running' ? duration(stage.startedAt) : '—';
                  return <Fragment key={stage.id}>
                    <tr onClick={toggleStage} className={cn('cursor-pointer transition-colors hover:bg-muted/40', expanded && 'bg-primary/5')}>
                      <td className="p-0">
                        <button type="button" onClick={event => { event.stopPropagation(); toggleStage(); }} aria-expanded={expanded} aria-controls={expanded ? `stage-${stage.id}-details` : undefined}
                          className="flex w-full min-w-0 items-center gap-3 px-4 py-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
                          {expanded ? <ChevronDown className="h-4 w-4 shrink-0 text-primary" /> : <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />}
                          <span className="w-5 shrink-0 font-mono text-xs text-muted-foreground">{String(index + 1).padStart(2, '0')}</span>
                          <span className="min-w-0 break-words font-medium [overflow-wrap:anywhere]">{stage.name}<span className="ml-2 whitespace-nowrap font-mono text-xs font-normal text-muted-foreground">#{stage.id}</span></span>
                        </button>
                      </td>
                      <td className="px-3 py-3"><StageStatus stage={stage} /></td>
                      <td className="px-3 py-3 font-mono text-xs tabular-nums text-muted-foreground" title={timestamp(stage.startedAt)}>{clockTime(stage.startedAt)}</td>
                      <td className="px-3 py-3 font-mono text-xs tabular-nums">{elapsed}</td>
                      <td className="px-3 py-3 text-xs tabular-nums">{stage.attempt ?? '—'}</td>
                      <td className="px-3 py-3 text-right font-mono text-xs tabular-nums text-muted-foreground">{stage.logs?.length ?? 0}</td>
                    </tr>
                    {expanded && <tr>
                      <td colSpan={6} className="bg-muted/20 p-4" id={`stage-${stage.id}-details`}>
                        <StageInspector stage={stage} canOperate={canOperate && pipeline.status !== 'Cancelled'} busy={busy}
                          onRerun={() => void operate(() => rerun.mutateAsync(stage.id), 'Stage queued for rerun')}
                          onSkip={() => void operate(() => skip.mutateAsync(stage.id), 'Stage skipped')}
                          onLogs={() => openStageLogs(stage.id)} />
                      </td>
                    </tr>}
                  </Fragment>;
                })}
              </tbody>
            </table>
          </div>}
        </TabsContent>
        <TabsContent value="logs" className="mt-0">
          <PipelineRunLogs stages={stages} stageId={requestedStage} onStageChange={id => setParam('stage', id === undefined ? undefined : String(id))} />
        </TabsContent>
        <TabsContent value="context" className="mt-0"><PipelineContextTable key={pipeline.id} items={context} /></TabsContent>
        <TabsContent value="data" className="m-0 p-4">
          <PipelinePayload title="Run data" value={JSON.stringify(pipeline)} />
        </TabsContent>
      </Tabs>
    </div>
  </div>;
}

function clockTime(value?: string) {
  if (!value || !Number.isFinite(Date.parse(value))) return '—';
  return new Date(value).toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function StageInspector({ stage, canOperate, busy, onRerun, onSkip, onLogs }: {
  stage: StageResponse; canOperate: boolean; busy: boolean; onRerun: () => void; onSkip: () => void; onLogs: () => void;
}) {
  return <section aria-label="Stage details" className="min-w-0 space-y-3">
    <h3 className="sr-only">{stage.name}</h3>
    <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="min-w-0 flex-1 break-words text-xs [overflow-wrap:anywhere]"><span className="mr-2 text-muted-foreground">Handler</span><code className="font-mono">{stage.stageHandlerName || '—'}</code></p>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" className="h-8 text-xs" onClick={onLogs}>View stage logs</Button>
        {canOperate && stage.status === 'Failed' && <>
          <Button size="sm" variant="outline" className="h-8 text-xs" disabled={busy} onClick={onRerun}><RotateCcw className="h-3.5 w-3.5" /> Rerun stage</Button>
          <Button size="sm" variant="outline" className="h-8 text-xs" disabled={busy} onClick={onSkip}><SkipForward className="h-3.5 w-3.5" /> Skip stage</Button>
        </>}
      </div>
    </div>
    {stage.description && <p className="whitespace-pre-wrap break-words text-sm text-muted-foreground">{stage.description}</p>}
    {stage.lastErrorCode && <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-status-error/20 bg-status-error-bg px-3 py-2 text-xs">
      <span className="text-status-error">Error <code className="ml-1 font-mono">{stage.lastErrorCode}</code></span>
      {stage.failureDisposition && <span className="text-muted-foreground">{stage.failureDisposition.replaceAll('_', ' ')}</span>}
      {stage.failureCount !== undefined && <span className="text-muted-foreground">{stage.failureCount} recorded failures</span>}
    </div>}
    <div className="grid min-w-0 items-start gap-3 lg:grid-cols-2">
      <PipelinePayload title="Input" value={stage.input} compact />
      <PipelinePayload title="Output" value={stage.output} compact />
    </div>
    <details className="group/stage-info">
      <summary className="inline-flex cursor-pointer list-none items-center gap-1 text-xs text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
        <ChevronRight className="h-3.5 w-3.5 transition-transform group-open/stage-info:rotate-90" /> Stage info
      </summary>
      <div className="mt-3 space-y-3 border-l-2 border-border pl-3">
        <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2 xl:grid-cols-4">
          <Metric label="Created">{timestamp(stage.createdAt)}</Metric>
          <Metric label="Started">{timestamp(stage.startedAt)}</Metric>
          <Metric label="Finished">{timestamp(stage.finishedAt)}</Metric>
          <Metric label="Attempt">{stage.attempt ?? '—'}</Metric>
          <Metric label="Retry attempt">{stage.retryAttempt ?? '—'}</Metric>
          {stage.nextRetryAt && <Metric label="Next retry">{timestamp(stage.nextRetryAt)}</Metric>}
          {stage.failureCount !== undefined && <Metric label="Recorded failures">{stage.failureCount}</Metric>}
          {stage.lastFailedAt && <Metric label="Last failure">{timestamp(stage.lastFailedAt)}</Metric>}
          {stage.failureDisposition && <Metric label="Failure disposition">{stage.failureDisposition}</Metric>}
          {stage.spanId && <Metric label="Span ID"><span className="select-all font-mono text-xs">{stage.spanId}</span></Metric>}
        </dl>
        {stage.options && <PipelinePayload title="Stage options" value={JSON.stringify(stage.options)} compact />}
      </div>
    </details>
  </section>;
}

function EmptyState({ message }: { message: string }) {
  return <p className="px-4 py-12 text-center text-sm text-muted-foreground">{message}</p>;
}
