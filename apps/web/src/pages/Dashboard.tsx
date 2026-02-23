import { formatDistanceToNow } from "date-fns";
import {
  AlertCircle,
  Clock,
  Cpu,
  HardDrive,
  Server,
  ServerCog,
  ShieldAlert,
} from "lucide-react";
import { AppHeader } from "@/components/layout/AppHeader";
import { KpiCard } from "@/components/ui/kpi-card";
import { useWorkerEvents, useWorkerStatus } from "@/hooks/use-workers";
import type { WorkerState, WorkerStatusResponse } from "@/types/api";

export default function Dashboard() {
  const { data: workers, isLoading: workersLoading, error: workersError } = useWorkerStatus({ limit: 50 });
  const { data: events, isLoading: eventsLoading, error: eventsError } = useWorkerEvents({ limit: 80 });

  const status = workers ?? {
    items: [],
    totalCount: 0,
    onlineCount: 0,
    offlineCount: 0,
    degradedCount: 0,
    offlineAfterSec: 45,
  };

  return (
    <div className="flex flex-col">
      <AppHeader
        title="Dashboard"
        subtitle="Real-time worker registry and activity logs"
      />

      <div className="flex-1 space-y-6 p-6">
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4 xl:grid-cols-6">
          <KpiCard
            title="Workers Online"
            value={status.onlineCount}
            subtitle={`${status.totalCount} tracked`}
            icon={Server}
          />
          <KpiCard
            title="Workers Degraded"
            value={status.degradedCount}
            subtitle="Need attention"
            icon={ShieldAlert}
          />
          <KpiCard
            title="Workers Offline"
            value={status.offlineCount}
            subtitle={`>${status.offlineAfterSec}s no heartbeat`}
            icon={AlertCircle}
          />
          <KpiCard
            title="In Flight Jobs"
            value={status.items.reduce((sum, w) => sum + (w.inFlightJobs || 0), 0)}
            subtitle="Across online workers"
            icon={ServerCog}
          />
          <KpiCard
            title="Jobs Processed"
            value={status.items.reduce((sum, w) => sum + (w.jobsProcessed || 0), 0).toLocaleString()}
            subtitle="All tracked workers"
            icon={Cpu}
          />
          <KpiCard
            title="Jobs Failed"
            value={status.items.reduce((sum, w) => sum + (w.jobsFailed || 0), 0).toLocaleString()}
            subtitle="All tracked workers"
            icon={HardDrive}
          />
        </div>

        <div className="grid gap-6 lg:grid-cols-5">
          <div className="rounded-xl border border-border bg-card lg:col-span-3">
            <div className="flex items-center justify-between border-b border-border px-5 py-4">
              <h3 className="font-semibold text-foreground">Worker Registry</h3>
              <span className="text-xs text-muted-foreground">
                {workersLoading ? "Refreshing..." : `${status.totalCount} workers`}
              </span>
            </div>
            <div className="max-h-[520px] overflow-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted/40">
                  <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="px-4 py-3">Worker</th>
                    <th className="px-4 py-3">App</th>
                    <th className="px-4 py-3">State</th>
                    <th className="px-4 py-3">Last Seen</th>
                    <th className="px-4 py-3 text-right">In Flight</th>
                    <th className="px-4 py-3 text-right">Processed</th>
                    <th className="px-4 py-3 text-right">Failed</th>
                  </tr>
                </thead>
                <tbody>
                  {workersError && (
                    <tr>
                      <td className="px-4 py-4 text-status-error" colSpan={7}>
                        Failed to load workers
                      </td>
                    </tr>
                  )}
                  {!workersError && status.items.length === 0 && !workersLoading && (
                    <tr>
                      <td className="px-4 py-6 text-muted-foreground" colSpan={7}>
                        No workers registered yet
                      </td>
                    </tr>
                  )}
                  {status.items.map((worker) => (
                    <WorkerRow key={worker.id} worker={worker} />
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <div className="rounded-xl border border-border bg-card lg:col-span-2">
            <div className="flex items-center justify-between border-b border-border px-5 py-4">
              <h3 className="font-semibold text-foreground">Worker Activity</h3>
              <span className="text-xs text-muted-foreground">
                {eventsLoading ? "Refreshing..." : `${events?.length || 0} events`}
              </span>
            </div>
            <div className="max-h-[520px] overflow-auto p-4">
              {eventsError && (
                <p className="text-sm text-status-error">Failed to load worker events</p>
              )}
              {!eventsError && (!events || events.length === 0) && !eventsLoading && (
                <p className="text-sm text-muted-foreground">No worker events yet</p>
              )}
              <div className="space-y-3">
                {events?.map((event) => (
                  <div key={event.id} className="rounded-lg border border-border bg-background p-3">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium text-foreground">{event.workerName}</span>
                      <span className="text-xs text-muted-foreground">
                        {formatDistanceToNow(new Date(event.ts), { addSuffix: true })}
                      </span>
                    </div>
                    <p className="mt-1 text-sm text-foreground">{event.message}</p>
                    <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
                      <span className="rounded bg-muted px-1.5 py-0.5">{event.level}</span>
                      <span className="rounded bg-muted px-1.5 py-0.5">{event.eventType}</span>
                      <span>{event.applicationName}</span>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function WorkerRow({ worker }: { worker: WorkerStatusResponse }) {
  const state = worker.effectiveState || worker.state;
  return (
    <tr className="border-b border-border last:border-0">
      <td className="px-4 py-3">
        <div className="font-medium text-foreground">{worker.workerName}</div>
        <div className="text-xs text-muted-foreground">
          {worker.hostName || "unknown-host"}
          {worker.workerVersion ? ` • ${worker.workerVersion}` : ""}
        </div>
      </td>
      <td className="px-4 py-3">
        <div className="text-foreground">{worker.applicationName}</div>
        <div className="text-xs text-muted-foreground">{worker.appId}</div>
      </td>
      <td className="px-4 py-3">
        <span className={`inline-flex rounded px-2 py-0.5 text-xs font-semibold ${stateBadgeClass(state)}`}>
          {state}
        </span>
      </td>
      <td className="px-4 py-3 text-muted-foreground">
        <div className="flex items-center gap-1">
          <Clock className="h-3.5 w-3.5" />
          <span className="text-xs">{formatDistanceToNow(new Date(worker.lastSeenAt), { addSuffix: true })}</span>
        </div>
      </td>
      <td className="px-4 py-3 text-right font-mono">{worker.inFlightJobs || 0}</td>
      <td className="px-4 py-3 text-right font-mono">{(worker.jobsProcessed || 0).toLocaleString()}</td>
      <td className="px-4 py-3 text-right font-mono">{(worker.jobsFailed || 0).toLocaleString()}</td>
    </tr>
  );
}

function stateBadgeClass(state: WorkerState): string {
  switch (state) {
    case "ready":
      return "bg-emerald-100 text-emerald-800";
    case "starting":
      return "bg-blue-100 text-blue-800";
    case "degraded":
      return "bg-amber-100 text-amber-800";
    case "draining":
      return "bg-orange-100 text-orange-800";
    case "stopped":
      return "bg-slate-200 text-slate-700";
    case "error":
      return "bg-red-100 text-red-800";
    case "offline":
      return "bg-rose-100 text-rose-800";
    default:
      return "bg-slate-100 text-slate-700";
  }
}
