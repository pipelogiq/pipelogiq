import { useMemo, useState } from 'react';
import { Copy, Search } from 'lucide-react';
import { format } from 'date-fns';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { toast } from '@/hooks/use-toast';
import { cn } from '@/lib/utils';
import type { StageResponse } from '@/types/api';

interface PipelineRunLogsProps {
  stages: StageResponse[];
  stageId?: number;
  onStageChange: (id: number | undefined) => void;
}

function timestamp(value: string): number {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : Number.POSITIVE_INFINITY;
}

function formatTimestamp(value: string): string {
  return Number.isFinite(timestamp(value)) ? format(new Date(value), 'yyyy-MM-dd HH:mm:ss.SSS') : value || '—';
}

export function PipelineRunLogs({ stages, stageId, onStageChange }: PipelineRunLogsProps) {
  const [severity, setSeverity] = useState('');
  const [search, setSearch] = useState('');
  const entries = useMemo(() => stages.flatMap((stage, stageIndex) => (stage.logs ?? []).map((log, logIndex) => ({
    log, stageId: stage.id, stageName: stage.name, stageIndex, logIndex,
    severity: log.logLevel?.trim().toUpperCase() || 'UNSPECIFIED',
    timestamp: timestamp(log.created),
  }))).sort((a, b) => a.timestamp === b.timestamp
    ? a.stageIndex - b.stageIndex || a.logIndex - b.logIndex
    : a.timestamp - b.timestamp), [stages]);
  const severities = useMemo(() => [...new Set(entries.map((entry) => entry.severity))].sort(), [entries]);
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    return entries.filter((entry) => (
      (stageId === undefined || entry.stageId === stageId) &&
      (!severity || entry.severity === severity) &&
      (!query || `${entry.log.message}\n${entry.stageName}\n${entry.log.created}`.toLowerCase().includes(query))
    ));
  }, [entries, stageId, severity, search]);
  const hasFilters = stageId !== undefined || severity !== '' || search !== '';

  async function copyLogs() {
    try {
      await navigator.clipboard.writeText(filtered.map((entry) =>
        `[${entry.log.created}] [${entry.severity}] [${entry.stageName} #${entry.stageId}] ${entry.log.message}`,
      ).join('\n'));
      toast({ title: 'Logs copied', description: `${filtered.length} matching log entries copied to the clipboard.` });
    } catch {
      toast({ title: 'Could not copy logs', description: 'Select the log text and copy it manually.', variant: 'destructive' });
    }
  }

  return (
    <section aria-label="Pipeline run logs" className="min-w-0">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <div className="relative min-w-48 flex-1">
          <Search aria-hidden="true" className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input aria-label="Search logs" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search messages, stages or timestamps…" className="h-9 pl-9" />
        </div>
        <Select value={String(stageId ?? 0)} onValueChange={value => onStageChange(Number(value) || undefined)}>
          <SelectTrigger aria-label="Filter logs by stage" className="h-9 w-48 max-w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="0">All stages</SelectItem>
            {stageId !== undefined && !stages.some(stage => stage.id === stageId) && <SelectItem value={String(stageId)}>Stage #{stageId} (unavailable)</SelectItem>}
            {stages.map((stage, index) => <SelectItem key={stage.id} value={String(stage.id)}>{index + 1}. {stage.name}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={severity || '__all__'} onValueChange={value => setSeverity(value === '__all__' ? '' : value)}>
          <SelectTrigger aria-label="Filter logs by severity" className="h-9 w-32 max-w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">All levels</SelectItem>
            {severities.map(level => <SelectItem key={level} value={level}>{level === 'UNSPECIFIED' ? 'Unspecified' : level}</SelectItem>)}
          </SelectContent>
        </Select>
        {hasFilters && (
          <Button type="button" variant="ghost" size="sm" onClick={() => { onStageChange(undefined); setSeverity(''); setSearch(''); }}>Clear filters</Button>
        )}
        <Button type="button" variant="ghost" size="sm" disabled={filtered.length === 0} onClick={() => void copyLogs()}>
          <Copy className="h-3.5 w-3.5" />
          Copy filtered logs
        </Button>
      </div>
      {entries.length === 0 ? (
        <div className="flex min-h-80 items-center justify-center p-8 text-center text-sm text-muted-foreground">No logs have been recorded for this pipeline.</div>
      ) : filtered.length === 0 ? (
        <div className="flex min-h-80 items-center justify-center p-8 text-center text-sm text-muted-foreground">No logs match these filters.</div>
      ) : (
        <div className="max-h-[max(20rem,calc(100vh-23rem))] min-h-80 min-w-0 overflow-auto">
          <table className="w-full min-w-[650px] table-fixed text-left text-xs" aria-label="Pipeline log entries">
            <thead className="sticky top-0 z-10 bg-muted text-muted-foreground">
              <tr>
                <th scope="col" className="w-44 px-4 py-2 font-medium">Time</th>
                <th scope="col" className="w-44 px-4 py-2 font-medium">Stage</th>
                <th scope="col" className="w-24 px-4 py-2 font-medium">Level</th>
                <th scope="col" className="px-4 py-2 font-medium">Message</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border bg-card">
              {filtered.map((entry) => (
                <tr key={`${entry.stageId}-${entry.logIndex}`} className="align-top hover:bg-muted/30">
                  <td className="px-4 py-2 font-mono text-[11px] leading-5 text-muted-foreground"><time dateTime={entry.log.created} title={entry.log.created}>{formatTimestamp(entry.log.created)}</time></td>
                  <td className="break-words px-4 py-2 leading-5 [overflow-wrap:anywhere]">{entry.stageName}<span className="block font-mono text-[10px] leading-4 text-muted-foreground">#{entry.stageId}</span></td>
                  <td className={cn('break-words px-4 py-2 font-mono text-[11px] leading-5 [overflow-wrap:anywhere]', ['ERROR', 'FATAL', 'CRITICAL'].includes(entry.severity) ? 'text-status-error' : ['WARN', 'WARNING'].includes(entry.severity) ? 'text-status-warning' : 'text-muted-foreground')}>{entry.log.logLevel?.trim() || '—'}</td>
                  <td className="px-4 py-2"><pre className="whitespace-pre-wrap break-words font-mono text-xs leading-5 [overflow-wrap:anywhere]">{entry.log.message}</pre></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p role="status" className="border-t border-border px-4 py-2 text-xs text-muted-foreground">
        Showing {filtered.length} of {entries.length} retained log {entries.length === 1 ? 'entry' : 'entries'}
      </p>
    </section>
  );
}
