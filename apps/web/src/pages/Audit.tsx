import { Fragment, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { format } from 'date-fns';
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Loader2,
  RefreshCw,
  ScrollText,
  X,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useAuditLog, useAuditOptions } from '@/hooks/use-audit';
import { cn } from '@/lib/utils';
import type { AuditOutcome, AuditRecord } from '@/types/api';

const actionLabels: Record<string, string> = {
  'pipeline.pause': 'Pause pipeline',
  'pipeline.resume': 'Resume pipeline',
  'stage.rerun': 'Rerun stage',
  'stage.skip': 'Skip stage',
  'pipeline.bulk_pause': 'Pause pipelines (bulk)',
  'pipeline.bulk_resume': 'Resume pipelines (bulk)',
  'pipeline.bulk_action': 'Run bulk pipeline action',
  'stage.bulk_rerun': 'Rerun stages (bulk)',
  'stage.bulk_skip': 'Skip stages (bulk)',
  'schedule.pause': 'Pause schedule',
  'schedule.resume': 'Resume schedule',
  'schedule.archive': 'Archive schedule',
  'schedule.trigger': 'Run schedule now',
  'schedule.update_policies': 'Update schedule policies',
  'schedule.policies_update': 'Update schedule policies',
  'user.create': 'Create user',
  'user.update': 'Update user',
  'user.password_reset': 'Reset user password',
  'auth.login': 'Sign in',
  'auth.logout': 'Sign out',
  'auth.password': 'Change password',
  'application.create': 'Create application',
  'application.update': 'Update application',
  'application.save': 'Save application',
  'api_key.create': 'Create API key',
  'api_key.disable': 'Disable API key',
  'policy.create': 'Create policy',
  'policy.update': 'Update policy',
  'policy.duplicate': 'Duplicate policy',
  'policy.enable': 'Enable policy',
  'policy.disable': 'Disable policy',
  'policy.pause': 'Pause policy',
  'policy.resume': 'Resume policy',
  'policy.delete': 'Delete policy',
  'policy.promote': 'Promote policy',
  'policy.preview': 'Preview policy',
  'observability.update': 'Update observability',
  'observability.configure': 'Configure observability',
  'observability.delete': 'Delete observability integration',
  'observability.update_config': 'Update observability configuration',
  'observability.test': 'Test observability connection',
  'observability.test_connection': 'Test observability connection',
  'retention.update': 'Update retention',
  'retention.purge': 'Purge retained data',
  'audit.export': 'Export audit log',
};

const outcomeLabels: Record<AuditOutcome, string> = {
  ok: 'Success',
  failed: 'Failed',
  partial: 'Partial success',
  denied: 'Access denied',
  rate_limited: 'Rate limited',
};
const outcomeTone: Record<string, string> = {
  ok: 'bg-status-success-bg text-status-success',
  failed: 'bg-status-error-bg text-status-error',
  partial: 'bg-status-warning-bg text-status-warning',
  denied: 'bg-status-error-bg text-status-error',
  rate_limited: 'bg-status-warning-bg text-status-warning',
};
const allFilters = '__all__';
const sensitiveDetailKey =
  /password|secret|token|credential|authorization|api.?key$|dsn|connection.?string|^(body|payload|context|input|output)$/i;

function humanize(value: string) {
  return value
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/[._-]/g, ' ')
    .replace(/^./, (first) => first.toUpperCase())
    .replace(/\bIds?\b/g, (word) => word.toUpperCase());
}

function actionLabel(action: string) {
  return actionLabels[action] ?? humanize(action);
}

function Outcome({ value }: { value: string }) {
  return (
    <span
      className={cn(
        'inline-flex whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium',
        outcomeTone[value] ?? 'bg-muted text-muted-foreground',
      )}
    >
      {outcomeLabels[value as AuditOutcome] ?? humanize(value)}
    </span>
  );
}

function DetailValue({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (value === null || value === undefined) return <span className="text-muted-foreground">—</span>;
  if (typeof value === 'boolean') return <span>{value ? 'Yes' : 'No'}</span>;
  if (typeof value !== 'object') return <span className="break-words [overflow-wrap:anywhere]">{String(value)}</span>;
  if (depth > 4) return <span className="text-muted-foreground">Additional nested details</span>;
  if (Array.isArray(value)) {
    if (!value.length) return <span className="text-muted-foreground">None</span>;
    if (value.every((item) => typeof item !== 'object')) return <span className="break-words">{value.join(', ')}</span>;
    return (
      <ul className="space-y-2">
        {value.map((item, index) => (
          <li key={index} className="rounded-md border border-border bg-background p-3">
            <DetailValue value={item} depth={depth + 1} />
          </li>
        ))}
      </ul>
    );
  }
  const entries = Object.entries(value).filter(([key]) => !sensitiveDetailKey.test(key));
  if (!entries.length) return <span className="text-muted-foreground">No additional details</span>;
  return (
    <dl className="grid grid-cols-[minmax(90px,140px)_minmax(0,1fr)] gap-x-4 gap-y-2">
      {entries.map(([key, item]) => (
        <Fragment key={key}>
          <dt className="text-muted-foreground">{key === 'success' ? 'Result' : humanize(key)}</dt>
          <dd className="min-w-0">
            {key === 'outcome' && typeof item === 'string' ? (
              <Outcome value={item} />
            ) : key === 'success' && typeof item === 'boolean' ? (
              <Outcome value={item ? 'ok' : 'failed'} />
            ) : (
              <DetailValue value={item} depth={depth + 1} />
            )}
          </dd>
        </Fragment>
      ))}
    </dl>
  );
}

function AuditDetails({ details }: { details: string }) {
  let parsed: unknown;
  try {
    parsed = JSON.parse(details);
  } catch {
    return <p className="text-muted-foreground">No structured details available for this event.</p>;
  }
  return <DetailValue value={parsed} />;
}

function EventRow({ row }: { row: AuditRecord }) {
  const [expanded, setExpanded] = useState(false);
  const hasDetails = !!row.details && row.details !== '{}' && row.details !== 'null';
  return (
    <Fragment>
      <tr className="border-b border-border/60 last:border-0 hover:bg-muted/20">
        <td className="whitespace-nowrap px-4 py-3 font-mono text-xs text-muted-foreground">
          {format(new Date(row.ts), 'yyyy-MM-dd HH:mm:ss')}
        </td>
        <td className="px-4 py-3">
          <div className="max-w-[260px] truncate" title={row.email}>
            {row.email || (row.userId ? `User #${row.userId}` : 'Unauthenticated')}
          </div>
          {row.userId ? (
            <div className="text-xs text-muted-foreground">User #{row.userId}</div>
          ) : row.email ? (
            <div className="text-xs text-muted-foreground">Unauthenticated</div>
          ) : null}
        </td>
        <td className="px-4 py-3">
          <div className="whitespace-nowrap font-medium">{actionLabel(row.action)}</div>
          <div className="font-mono text-xs text-muted-foreground">{row.action}</div>
        </td>
        <td className="max-w-[220px] truncate px-4 py-3 font-mono text-xs text-muted-foreground" title={row.target}>
          {row.target || '—'}
        </td>
        <td className="px-4 py-3">
          <Outcome value={row.outcome} />
        </td>
        <td className="whitespace-nowrap px-4 py-3 font-mono text-xs text-muted-foreground">{row.remoteAddr || '—'}</td>
        <td className="px-3 py-3">
          {hasDetails ? (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2"
              aria-label={`${expanded ? 'Hide' : 'Show'} details for event ${row.id}`}
              aria-expanded={expanded}
              aria-controls={`audit-details-${row.id}`}
              onClick={() => setExpanded(!expanded)}
            >
              <ChevronDown className={cn('h-4 w-4 transition-transform', expanded && 'rotate-180')} />
            </Button>
          ) : (
            <span className="text-muted-foreground">—</span>
          )}
        </td>
      </tr>
      {expanded && (
        <tr className="border-b border-border/60 bg-muted/20">
          <td colSpan={7} className="px-5 py-4">
            <div id={`audit-details-${row.id}`} className="max-w-3xl text-sm">
              <p className="mb-3 font-medium">Event details</p>
              <AuditDetails details={row.details!} />
            </div>
          </td>
        </tr>
      )}
    </Fragment>
  );
}

function integerParam(value: string | null, fallback: number, minimum = 1, maximum = Number.MAX_SAFE_INTEGER) {
  if (!value || !/^\d+$/.test(value)) return fallback;
  const number = Number(value);
  return Number.isSafeInteger(number) && number >= minimum && number <= maximum ? number : fallback;
}

export default function Audit() {
  const [searchParams, setSearchParams] = useSearchParams();
  const action = searchParams.get('action') || '';
  const userId = integerParam(searchParams.get('userId'), -1, 0);
  const outcomeValue = searchParams.get('outcome') || '';
  const outcome = Object.hasOwn(outcomeLabels, outcomeValue) ? (outcomeValue as AuditOutcome) : undefined;
  const page = integerParam(searchParams.get('page'), 1, 1, 10_000_000);
  const pageSize = integerParam(searchParams.get('pageSize'), 50, 1, 100);
  const query = useAuditLog({
    action: action || undefined,
    userId: userId >= 0 ? userId : undefined,
    outcome,
    page,
    pageSize,
  });
  const options = useAuditOptions();
  const hasFilters = !!action || userId >= 0 || !!outcome;
  const actions = [...new Set([...(options.data?.actions ?? []), ...(action ? [action] : [])])].sort((a, b) =>
    actionLabel(a).localeCompare(actionLabel(b)),
  );
  const users = options.data?.users ?? [];
  const rows = query.data?.items ?? [];
  const totalCount = query.data?.totalCount ?? 0;
  const totalPages = Math.max(1, query.data?.totalPages ?? 1);
  const currentPage = query.data?.page ?? page;
  const currentPageSize = query.data?.pageSize ?? pageSize;

  function updateParam(key: string, value: string) {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous);
      if (value) next.set(key, value);
      else next.delete(key);
      if (key !== 'page') next.delete('page');
      return next;
    });
  }
  function clearFilters() {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous);
      ['action', 'userId', 'outcome', 'page'].forEach((key) => next.delete(key));
      return next;
    });
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">Audit log</h2>
          <p className="text-sm text-muted-foreground">
            User activity across pipelines, stages, schedules and settings, newest first.
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            void query.refetch();
            void options.refetch();
          }}
          disabled={query.isFetching || options.isFetching}
        >
          <RefreshCw className={cn('h-4 w-4', query.isFetching && 'animate-spin')} />
          Refresh
        </Button>
      </div>
      <div className="rounded-xl border border-border bg-card p-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-[200px] flex-1 space-y-1.5 text-xs font-medium text-muted-foreground">
            <label htmlFor="audit-action">Action</label>
            <Select
              value={action || allFilters}
              onValueChange={(value) => updateParam('action', value === allFilters ? '' : value)}
            >
              <SelectTrigger id="audit-action" aria-label="Filter by action" className="h-9 text-foreground"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value={allFilters}>All actions</SelectItem>
                {actions.map((value) => <SelectItem key={value} value={value}>{actionLabel(value)}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          <div className="min-w-[240px] flex-1 space-y-1.5 text-xs font-medium text-muted-foreground">
            <label htmlFor="audit-user">User</label>
            <Select
              value={userId >= 0 ? String(userId) : allFilters}
              onValueChange={(value) => updateParam('userId', value === allFilters ? '' : value)}
            >
              <SelectTrigger id="audit-user" aria-label="Filter by user" className="h-9 text-foreground"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value={allFilters}>All users</SelectItem>
                <SelectItem value="0">Unauthenticated</SelectItem>
                {users
                  .filter((user) => user.id > 0)
                  .map((user) => (
                    <SelectItem key={user.id} value={String(user.id)}>
                      {user.email || 'Unknown user'} · #{user.id}
                    </SelectItem>
                  ))}
                {userId > 0 && !users.some((user) => user.id === userId) && (
                  <SelectItem value={String(userId)}>User #{userId}</SelectItem>
                )}
              </SelectContent>
            </Select>
          </div>
          <div className="min-w-[160px] flex-1 space-y-1.5 text-xs font-medium text-muted-foreground">
            <label htmlFor="audit-outcome">Outcome</label>
            <Select
              value={outcome ?? allFilters}
              onValueChange={(value) => updateParam('outcome', value === allFilters ? '' : value)}
            >
              <SelectTrigger id="audit-outcome" aria-label="Filter by outcome" className="h-9 text-foreground"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value={allFilters}>All outcomes</SelectItem>
                {Object.entries(outcomeLabels).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}
              </SelectContent>
            </Select>
          </div>
          {hasFilters && (
            <Button size="sm" variant="ghost" onClick={clearFilters}>
              <X className="h-4 w-4" />
              Clear filters
            </Button>
          )}
        </div>
        {options.isError && (
          <p role="alert" className="mt-3 text-xs text-status-error">
            Could not load filter choices.{' '}
            <button className="underline underline-offset-2" onClick={() => void options.refetch()}>
              Retry filters
            </button>
          </p>
        )}
      </div>
      <div className="overflow-hidden rounded-xl border border-border bg-card" aria-busy={query.isFetching}>
        {query.isLoading ? (
          <div role="status" className="flex items-center justify-center gap-2 py-14 text-sm text-muted-foreground">
            <Loader2 className="h-5 w-5 animate-spin" />
            Loading audit log…
          </div>
        ) : query.isError ? (
          <div role="alert" className="space-y-3 px-5 py-10 text-center">
            <p className="text-sm text-status-error">Could not load the audit log.</p>
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
              Try again
            </Button>
          </div>
        ) : !rows.length ? (
          <div className="space-y-2 px-5 py-12 text-center">
            <ScrollText className="mx-auto mb-3 h-7 w-7 text-muted-foreground" />
            <p className="text-sm font-medium">
              {page > 1 ? 'No events on this page' : hasFilters ? 'No matching events' : 'Nothing recorded yet'}
            </p>
            <p className="text-sm text-muted-foreground">
              {page > 1
                ? 'Return to the first page to see the latest activity.'
                : hasFilters
                  ? 'Try another action, user or outcome, or clear the filters.'
                  : 'User actions will appear here as they happen.'}
            </p>
            {page > 1 ? (
              <Button variant="outline" size="sm" onClick={() => updateParam('page', '')}>
                Go to first page
              </Button>
            ) : (
              hasFilters && (
                <Button variant="outline" size="sm" onClick={clearFilters}>
                  Clear filters
                </Button>
              )
            )}
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-muted/40">
                <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground">
                  {['When', 'User', 'Action', 'Target', 'Outcome', 'From', 'Details'].map((label) => (
                    <th key={label} className="px-4 py-3">
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <EventRow key={row.id} row={row} />
                ))}
              </tbody>
            </table>
          </div>
        )}
        {query.data && !query.isError && totalCount > 0 && (
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-4 py-3">
            <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
              <span className="tabular-nums">
                {rows.length ? ((currentPage - 1) * currentPageSize + 1).toLocaleString() : 0}–
                {Math.min(currentPage * currentPageSize, totalCount).toLocaleString()} of {totalCount.toLocaleString()}{' '}
                events
              </span>
              <div className="flex items-center gap-2">
                <Select
                  value={String(pageSize)}
                  onValueChange={(value) => updateParam('pageSize', value)}
                >
                  <SelectTrigger id="audit-page-size" aria-label="Events per page" className="h-7 w-20 px-2 text-xs text-foreground"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {[...new Set([25, 50, 100, pageSize])]
                      .sort((a, b) => a - b)
                      .map((size) => <SelectItem key={size} value={String(size)}>{size}</SelectItem>)}
                  </SelectContent>
                </Select>
                <label htmlFor="audit-page-size">per page</label>
              </div>
            </div>
            <nav aria-label="Audit pagination" className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                aria-label="First page"
                onClick={() => updateParam('page', '')}
                disabled={currentPage <= 1 || query.isFetching}
              >
                <ChevronsLeft />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                aria-label="Previous page"
                onClick={() => updateParam('page', String(currentPage - 1))}
                disabled={currentPage <= 1 || query.isFetching}
              >
                <ChevronLeft />
              </Button>
              <span className="px-2 text-xs tabular-nums text-muted-foreground">
                Page {currentPage.toLocaleString()} of {totalPages.toLocaleString()}
              </span>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                aria-label="Next page"
                onClick={() => updateParam('page', String(currentPage + 1))}
                disabled={currentPage >= totalPages || query.isFetching}
              >
                <ChevronRight />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                aria-label="Last page"
                onClick={() => updateParam('page', String(totalPages))}
                disabled={currentPage >= totalPages || query.isFetching}
              >
                <ChevronsRight />
              </Button>
            </nav>
          </div>
        )}
      </div>
    </div>
  );
}
