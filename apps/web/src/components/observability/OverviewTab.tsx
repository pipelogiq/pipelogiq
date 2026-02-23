import { useState } from "react";
import { KpiCard } from "@/components/ui/kpi-card";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/ui/status-badge";
import { Copy, Check, Zap, Loader2, Activity, AlertTriangle, Clock, TrendingUp, ArrowRight, ExternalLink } from "lucide-react";
import {
  useObservabilityStatus,
  useObservabilityInsights,
  useObservabilityConfig,
  useTestConnection,
} from "@/hooks/use-observability";
import { ConfigureOtelDialog } from "./ConfigureOtelDialog";
import { ConfigureLogsDialog } from "./ConfigureLogsDialog";
import { ConfigureGrafanaDialog } from "./ConfigureGrafanaDialog";
import { ConfigureAlertingDialog } from "./ConfigureAlertingDialog";
import type { TimeRange, OtelConfig, GrafanaConfig, Integration } from "@/types/observability";
import { cn } from "@/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { formatDistanceToNow } from "date-fns";

interface OverviewTabProps {
  timeRange: TimeRange;
}

export function OverviewTab({ timeRange }: OverviewTabProps) {
  const { data: status } = useObservabilityStatus();
  const { data: insights } = useObservabilityInsights(timeRange);
  const { data: config } = useObservabilityConfig();
  const testMutation = useTestConnection();

  const [otelDialogOpen, setOtelDialogOpen] = useState(false);
  const [logsDialogOpen, setLogsDialogOpen] = useState(false);
  const [grafanaDialogOpen, setGrafanaDialogOpen] = useState(false);
  const [alertingDialogOpen, setAlertingDialogOpen] = useState(false);
  const [copiedOtel, setCopiedOtel] = useState(false);

  const otelIntegration = config?.integrations.find(i => i.type === "opentelemetry");
  const alertingIntegration =
    config?.integrations.find(i => i.type === "alerting") ??
    ({
      type: "alerting",
      name: "Alerts",
      description: "Route notifications to chat, webhook, and on-call tools",
      icon: "🚨",
      status: status?.alerting.configured ? "configured" : "not_configured",
      config: {
        channels: status?.alerting.channels ?? [],
        enabledEvents: status?.alerting.events ?? [],
      },
    } satisfies Integration);
  const logsIntegration = config?.integrations.find(i => i.type === "graylog");
  const grafanaIntegration = config?.integrations.find(i => i.type === "grafana");
  const grafanaConfig = (grafanaIntegration?.config || {}) as Partial<GrafanaConfig>;

  const handleCopyOtelConfig = () => {
    const otelConfig = (otelIntegration?.config || {}) as Partial<OtelConfig>;
    const snippet = `OTEL_EXPORTER_OTLP_ENDPOINT=${otelConfig.endpoint || "<endpoint>"}
OTEL_EXPORTER_OTLP_PROTOCOL=${otelConfig.protocol || "grpc"}
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=${((otelConfig.samplingRate ?? 100) / 100).toFixed(2)}
OTEL_SERVICE_NAME=pipelogiq`;
    navigator.clipboard.writeText(snippet);
    setCopiedOtel(true);
    setTimeout(() => setCopiedOtel(false), 2000);
  };

  const statusToVariant = (s?: string) => {
    switch (s) {
      case "connected": return "success" as const;
      case "configured": return "warning" as const;
      case "disconnected": case "error": return "error" as const;
      default: return "default" as const;
    }
  };

  const statusLabel = (s?: string) => {
    switch (s) {
      case "connected": return "Connected";
      case "configured": return "Configured";
      case "disconnected": return "Disconnected";
      case "error": return "Error";
      default: return "Not configured";
    }
  };

  return (
    <TooltipProvider>
      <div className="space-y-6">
        {/* Section: Observability Health */}
        <div>
          <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
            Observability Health
          </h3>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            {/* OTel Card */}
            <div className="rounded-xl border border-border bg-card p-5 space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="text-lg">🔭</span>
                  <span className="font-semibold text-sm">OpenTelemetry</span>
                </div>
                <StatusBadge status={statusToVariant(otelIntegration?.status)} size="sm">
                  {statusLabel(otelIntegration?.status)}
                </StatusBadge>
              </div>
              <div className="grid grid-cols-2 gap-3 text-sm">
                <div>
                  <p className="text-muted-foreground text-xs">Export rate</p>
                  <p className="font-semibold">{status?.otel.exportRatePerMin ?? 0}/min</p>
                </div>
                <div>
                  <p className="text-muted-foreground text-xs">Drop rate</p>
                  <p className="font-semibold">{status?.otel.dropRate ?? 0}%</p>
                </div>
              </div>
              {status?.otel.lastSuccessExportAt && (
                <p className="text-xs text-muted-foreground">
                  Last export: {formatDistanceToNow(new Date(status.otel.lastSuccessExportAt), { addSuffix: true })}
                </p>
              )}
              <div className="flex gap-2 pt-1">
                <Button
                  variant="outline"
                  size="sm"
                  className="gap-1.5 flex-1"
                  onClick={() => testMutation.mutate("opentelemetry")}
                  disabled={otelIntegration?.status === "not_configured" || testMutation.isPending}
                >
                  {testMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Zap className="h-3.5 w-3.5" />}
                  Test
                </Button>
                <Button variant="outline" size="sm" className="gap-1.5 flex-1" onClick={handleCopyOtelConfig}>
                  {copiedOtel ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  {copiedOtel ? "Copied" : "Copy Config"}
                </Button>
                <Button size="sm" onClick={() => setOtelDialogOpen(true)}>
                  Configure
                </Button>
              </div>
            </div>

            {/* Alerts Card */}
            <div className="rounded-xl border border-border bg-card p-5 space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="text-lg">🚨</span>
                  <span className="font-semibold text-sm">Alerts</span>
                </div>
                <StatusBadge status={statusToVariant(alertingIntegration?.status)} size="sm">
                  {statusLabel(alertingIntegration?.status)}
                </StatusBadge>
              </div>
              <div className="text-sm space-y-1">
                <div>
                  <p className="text-muted-foreground text-xs">Channels</p>
                  <p className="text-xs">
                    {status?.alerting.channels?.length ? status.alerting.channels.join(", ") : "Not configured"}
                  </p>
                </div>
                <div>
                  <p className="text-muted-foreground text-xs">Events enabled</p>
                  <p className="text-xs">{status?.alerting.events?.length ?? 0}</p>
                </div>
                <p className="text-xs text-muted-foreground">
                  Suggested: stage fail, manual rerun/skip, worker up/down, policy triggered.
                </p>
              </div>
              <Button variant="outline" size="sm" className="w-full" onClick={() => setAlertingDialogOpen(true)}>
                Configure
              </Button>
            </div>

            {/* Logs Card */}
            <div className="rounded-xl border border-border bg-card p-5 space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="text-lg">📋</span>
                  <span className="font-semibold text-sm">Logs</span>
                </div>
                <StatusBadge
                  status={status?.logs.configured ? "success" : "default"}
                  size="sm"
                >
                  {status?.logs.configured ? status.logs.provider || "Configured" : "Not configured"}
                </StatusBadge>
              </div>
              <div className="text-sm">
                <p className="text-muted-foreground text-xs">Link template</p>
                <p className="text-xs">
                  {status?.logs.linkTemplateConfigured ? "Configured" : "Not set"}
                </p>
              </div>
              <Button variant="outline" size="sm" className="w-full" onClick={() => setLogsDialogOpen(true)}>
                Configure
              </Button>
            </div>

            {/* Grafana Card */}
            <div className="rounded-xl border border-border bg-card p-5 space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="text-lg">📈</span>
                  <span className="font-semibold text-sm">Grafana</span>
                </div>
                <StatusBadge status={statusToVariant(grafanaIntegration?.status)} size="sm">
                  {statusLabel(grafanaIntegration?.status)}
                </StatusBadge>
              </div>
              <div className="text-sm">
                <p className="text-muted-foreground text-xs">Dashboard URL</p>
                <p className="text-xs font-mono">{grafanaConfig.dashboardUrl || "http://localhost:3000"}</p>
              </div>
              <div className="flex gap-2">
                {grafanaConfig.dashboardUrl && (
                  <Button asChild variant="outline" size="sm" className="gap-1.5 flex-1">
                    <a href={grafanaConfig.dashboardUrl} target="_blank" rel="noreferrer">
                      <ExternalLink className="h-3.5 w-3.5" />
                      Open
                    </a>
                  </Button>
                )}
                <Button variant="outline" size="sm" className="flex-1" onClick={() => setGrafanaDialogOpen(true)}>
                  Configure
                </Button>
              </div>
            </div>
          </div>
        </div>

        {/* Section: Summary KPIs */}
        {insights && (
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
        )}

        {/* Section: Operational Insights */}
        {insights && (
          <div className="grid gap-6 lg:grid-cols-2">
            {/* Slowest Stages */}
            <div className="rounded-xl border border-border bg-card">
              <div className="flex items-center justify-between border-b border-border px-5 py-3">
                <div className="flex items-center gap-2">
                  <Clock className="h-4 w-4 text-muted-foreground" />
                  <h4 className="font-semibold text-sm">Slowest Stages (p95)</h4>
                </div>
                <span className="text-xs text-muted-foreground">{timeRangeLabel(timeRange)}</span>
              </div>
              <div className="divide-y divide-border">
                {insights.slowestStages.map((stage, idx) => (
                  <div key={idx} className="flex items-center justify-between px-5 py-3 hover:bg-muted/50 transition-colors">
                    <div className="min-w-0">
                      <p className="text-sm font-medium truncate">{stage.stageName}</p>
                      <p className="text-xs text-muted-foreground truncate">{stage.pipelineName}</p>
                    </div>
                    <div className="flex items-center gap-3 shrink-0">
                      <DurationBar durationMs={stage.p95Ms} maxMs={insights.slowestStages[0]?.p95Ms || 1} />
                      <span className="text-sm font-mono font-semibold w-16 text-right">{formatMs(stage.p95Ms)}</span>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button variant="ghost" size="icon" className="h-7 w-7">
                            <ArrowRight className="h-3.5 w-3.5" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>View executions</TooltipContent>
                      </Tooltip>
                    </div>
                  </div>
                ))}
                {insights.slowestStages.length === 0 && (
                  <div className="px-5 py-8 text-center text-sm text-muted-foreground">No data available</div>
                )}
              </div>
            </div>

            {/* Error Hotspots */}
            <div className="rounded-xl border border-border bg-card">
              <div className="flex items-center justify-between border-b border-border px-5 py-3">
                <div className="flex items-center gap-2">
                  <AlertTriangle className="h-4 w-4 text-muted-foreground" />
                  <h4 className="font-semibold text-sm">Error Hotspots</h4>
                </div>
                <span className="text-xs text-muted-foreground">{timeRangeLabel(timeRange)}</span>
              </div>
              <div className="divide-y divide-border">
                {insights.errorHotspots.map((hotspot, idx) => (
                  <div key={idx} className="flex items-center justify-between px-5 py-3 hover:bg-muted/50 transition-colors">
                    <div className="min-w-0">
                      <p className="text-sm font-medium truncate">{hotspot.stageName}</p>
                      <p className="text-xs text-muted-foreground truncate">{hotspot.pipelineName}</p>
                    </div>
                    <div className="flex items-center gap-4 shrink-0">
                      <div className="text-right">
                        <p className={cn("text-sm font-semibold", hotspot.failureRate > 10 ? "text-red-600" : hotspot.failureRate > 5 ? "text-amber-600" : "text-foreground")}>
                          {hotspot.failureRate.toFixed(1)}%
                        </p>
                        <p className="text-xs text-muted-foreground">{hotspot.avgRetries.toFixed(1)} retries</p>
                      </div>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button variant="ghost" size="icon" className="h-7 w-7">
                            <ArrowRight className="h-3.5 w-3.5" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>Filter executions</TooltipContent>
                      </Tooltip>
                    </div>
                  </div>
                ))}
                {insights.errorHotspots.length === 0 && (
                  <div className="px-5 py-8 text-center text-sm text-muted-foreground">No error hotspots detected</div>
                )}
              </div>
            </div>
          </div>
        )}

        {/* Dialogs */}
        {otelIntegration && (
          <ConfigureOtelDialog
            open={otelDialogOpen}
            onOpenChange={setOtelDialogOpen}
            integration={otelIntegration}
          />
        )}
        {logsIntegration && (
          <ConfigureLogsDialog
            open={logsDialogOpen}
            onOpenChange={setLogsDialogOpen}
            integration={logsIntegration}
          />
        )}
        {grafanaIntegration && (
          <ConfigureGrafanaDialog
            open={grafanaDialogOpen}
            onOpenChange={setGrafanaDialogOpen}
            integration={grafanaIntegration}
          />
        )}
        <ConfigureAlertingDialog
          open={alertingDialogOpen}
          onOpenChange={setAlertingDialogOpen}
          integration={alertingIntegration}
        />
      </div>
    </TooltipProvider>
  );
}

// ── Helpers ──

function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60_000).toFixed(1)}m`;
}

function DurationBar({ durationMs, maxMs }: { durationMs: number; maxMs: number }) {
  const pct = Math.max(5, (durationMs / maxMs) * 100);
  return (
    <div className="w-20 h-2 rounded-full bg-muted overflow-hidden">
      <div
        className={cn(
          "h-full rounded-full transition-all",
          pct > 80 ? "bg-red-500" : pct > 50 ? "bg-amber-500" : "bg-blue-500"
        )}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

function timeRangeLabel(tr: TimeRange): string {
  switch (tr) {
    case "15m": return "Last 15 minutes";
    case "1h": return "Last hour";
    case "6h": return "Last 6 hours";
    case "24h": return "Last 24 hours";
    case "7d": return "Last 7 days";
  }
}
