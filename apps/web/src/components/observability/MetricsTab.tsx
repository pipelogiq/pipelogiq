import { KpiCard } from "@/components/ui/kpi-card";
import { Activity, AlertTriangle, TrendingUp, Clock, Loader2 } from "lucide-react";
import { useObservabilityInsights } from "@/hooks/use-observability";
import type { TimeRange } from "@/types/observability";

interface MetricsTabProps {
  timeRange: TimeRange;
}

export function MetricsTab({ timeRange }: MetricsTabProps) {
  const { data: insights, isLoading } = useObservabilityInsights(timeRange);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 className="h-6 w-6 animate-spin text-primary" />
      </div>
    );
  }

  if (!insights) {
    return (
      <div className="text-center py-16 text-muted-foreground">
        No metrics data available
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Summary KPIs */}
      <div>
        <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
          Pipeline Throughput
        </h3>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <KpiCard
            title="Executions / min"
            value={insights.summary.executionsPerMin.toFixed(1)}
            icon={Activity}
          />
          <KpiCard
            title="Failures / min"
            value={insights.summary.failuresPerMin.toFixed(1)}
            icon={AlertTriangle}
          />
          <KpiCard
            title="Success Rate"
            value={`${insights.summary.successRate.toFixed(1)}%`}
            icon={TrendingUp}
            trend={{
              value: insights.summary.successRate >= 95 ? 2.1 : -1.3,
              direction: insights.summary.successRate >= 95 ? "up" : "down",
              isPositive: insights.summary.successRate >= 95,
            }}
          />
          <KpiCard
            title="Avg Stage Duration"
            value={formatMs(insights.summary.avgStageMs)}
            icon={Clock}
          />
        </div>
      </div>

      {/* Stage performance table */}
      <div>
        <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
          Stage Performance (p95)
        </h3>
        <div className="rounded-xl border border-border bg-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/50">
                <th className="px-5 py-3 text-left font-semibold text-muted-foreground">#</th>
                <th className="px-5 py-3 text-left font-semibold text-muted-foreground">Stage</th>
                <th className="px-5 py-3 text-left font-semibold text-muted-foreground">Pipeline</th>
                <th className="px-5 py-3 text-right font-semibold text-muted-foreground">p95 Duration</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {insights.slowestStages.map((stage, idx) => (
                <tr key={idx} className="hover:bg-muted/50 transition-colors">
                  <td className="px-5 py-3 text-muted-foreground">{idx + 1}</td>
                  <td className="px-5 py-3 font-medium">{stage.stageName}</td>
                  <td className="px-5 py-3 text-muted-foreground">{stage.pipelineName}</td>
                  <td className="px-5 py-3 text-right font-mono font-semibold">{formatMs(stage.p95Ms)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Error hotspots table */}
      <div>
        <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
          Error Hotspots
        </h3>
        <div className="rounded-xl border border-border bg-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/50">
                <th className="px-5 py-3 text-left font-semibold text-muted-foreground">Stage</th>
                <th className="px-5 py-3 text-left font-semibold text-muted-foreground">Pipeline</th>
                <th className="px-5 py-3 text-right font-semibold text-muted-foreground">Failure Rate</th>
                <th className="px-5 py-3 text-right font-semibold text-muted-foreground">Avg Retries</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {insights.errorHotspots.map((hotspot, idx) => (
                <tr key={idx} className="hover:bg-muted/50 transition-colors">
                  <td className="px-5 py-3 font-medium">{hotspot.stageName}</td>
                  <td className="px-5 py-3 text-muted-foreground">{hotspot.pipelineName}</td>
                  <td className={`px-5 py-3 text-right font-semibold ${hotspot.failureRate > 10 ? "text-red-600" : hotspot.failureRate > 5 ? "text-amber-600" : ""}`}>
                    {hotspot.failureRate.toFixed(1)}%
                  </td>
                  <td className="px-5 py-3 text-right font-mono">{hotspot.avgRetries.toFixed(1)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Metrics / Grafana / Tempo info */}
      <div className="rounded-xl border border-border bg-card p-5">
        <h4 className="mb-2 text-sm font-semibold">Metrics / Grafana / Tempo</h4>
        <p className="text-sm text-muted-foreground mb-3">
          Local compose defaults expose Prometheus-compatible `/metrics` endpoints and an OpenTelemetry trace backend via Tempo + Grafana.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <code className="rounded-md bg-muted px-3 py-1.5 text-sm font-mono">/metrics</code>
          <code className="rounded-md bg-muted px-3 py-1.5 text-sm font-mono">http://localhost:3000</code>
          <code className="rounded-md bg-muted px-3 py-1.5 text-sm font-mono">tempo:4317</code>
        </div>
      </div>
    </div>
  );
}

function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60_000).toFixed(1)}m`;
}
