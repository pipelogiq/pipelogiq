import { format } from 'date-fns';
import { Loader2 } from 'lucide-react';
import { AppHeader } from '@/components/layout/AppHeader';
import { useAuditLog } from '@/hooks/use-audit';
import { cn } from '@/lib/utils';

const outcomeTone: Record<string, string> = {
  ok: 'bg-status-success-bg text-status-success',
  failed: 'bg-status-error-bg text-status-error',
  rate_limited: 'bg-status-warning-bg text-status-warning',
};

export default function Audit() {
  const { data, isLoading, isError } = useAuditLog();

  return (
    <div className="flex flex-col">
      <AppHeader title="Audit log" subtitle="Sign-ins, password changes and every administrative change, newest first" />
      <div className="flex-1 p-6">
        <div className="overflow-hidden rounded-xl border border-border bg-card">
          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-6 w-6 animate-spin text-primary" />
            </div>
          ) : isError ? (
            <div className="px-5 py-8 text-sm text-status-error">Could not load the audit log.</div>
          ) : !data?.length ? (
            <div className="px-5 py-8 text-center text-sm text-muted-foreground">Nothing recorded yet.</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted/40">
                  <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="px-4 py-3">When</th>
                    <th className="px-4 py-3">Who</th>
                    <th className="px-4 py-3">Action</th>
                    <th className="px-4 py-3">Target</th>
                    <th className="px-4 py-3">Outcome</th>
                    <th className="px-4 py-3">From</th>
                  </tr>
                </thead>
                <tbody>
                  {data.map((row) => (
                    <tr key={row.id} className="border-b border-border/60 last:border-0">
                      <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-muted-foreground">
                        {format(new Date(row.ts), 'yyyy-MM-dd HH:mm:ss')}
                      </td>
                      <td className="px-4 py-2.5 text-foreground">{row.email ?? '—'}</td>
                      <td className="px-4 py-2.5 font-mono text-xs text-foreground">{row.action}</td>
                      <td className="max-w-[280px] truncate px-4 py-2.5 font-mono text-xs text-muted-foreground">{row.target ?? '—'}</td>
                      <td className="px-4 py-2.5">
                        <span className={cn('rounded-full px-2 py-0.5 text-xs font-medium', outcomeTone[row.outcome] ?? 'bg-muted text-muted-foreground')}>
                          {row.outcome}
                        </span>
                      </td>
                      <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">{row.remoteAddr ?? '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
