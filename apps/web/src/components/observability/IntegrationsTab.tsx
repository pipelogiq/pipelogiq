import { useState } from "react";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/ui/status-badge";
import { Settings, Zap, Loader2, Copy, Check, ExternalLink } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useObservabilityConfig, useSaveIntegrationConfig, useTestConnection } from "@/hooks/use-observability";
import { ConfigureOtelDialog } from "./ConfigureOtelDialog";
import { ConfigureLogsDialog } from "./ConfigureLogsDialog";
import { ConfigureGrafanaDialog } from "./ConfigureGrafanaDialog";
import { ConfigureAlertingDialog } from "./ConfigureAlertingDialog";
import type { Integration, IntegrationType, OtelConfig, GrafanaConfig, AlertingConfig } from "@/types/observability";
import { cn } from "@/lib/utils";
import { formatDistanceToNow } from "date-fns";

const localTempoTraceLinkTemplate =
  "http://localhost:3000/explore?orgId=1&left=%7B%22datasource%22:%22Tempo%22,%22queries%22:%5B%7B%22query%22:%22${traceId}%22,%22queryType%22:%22traceql%22%7D%5D%7D";

export function IntegrationsTab() {
  const { data: config, isLoading } = useObservabilityConfig();
  const testMutation = useTestConnection();
  const saveMutation = useSaveIntegrationConfig();

  const [otelDialogOpen, setOtelDialogOpen] = useState(false);
  const [logsDialogOpen, setLogsDialogOpen] = useState(false);
  const [grafanaDialogOpen, setGrafanaDialogOpen] = useState(false);
  const [alertingDialogOpen, setAlertingDialogOpen] = useState(false);
  const [copiedOtel, setCopiedOtel] = useState(false);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 className="h-6 w-6 animate-spin text-primary" />
      </div>
    );
  }

  const integrations = config?.integrations || [];

  const handleConfigure = (type: IntegrationType) => {
    switch (type) {
      case "opentelemetry":
        setOtelDialogOpen(true);
        break;
      case "graylog":
        setLogsDialogOpen(true);
        break;
      case "grafana":
        setGrafanaDialogOpen(true);
        break;
      case "alerting":
        setAlertingDialogOpen(true);
        break;
    }
  };

  const handleTest = (type: IntegrationType) => {
    testMutation.mutate(type);
  };

  const handleCopyOtelConfig = (integration: Integration) => {
    const otelConfig = (integration.config || {}) as Partial<OtelConfig>;
    const snippet = `OTEL_EXPORTER_OTLP_ENDPOINT=${otelConfig.endpoint || "<endpoint>"}
OTEL_EXPORTER_OTLP_PROTOCOL=${otelConfig.protocol || "grpc"}
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=${((otelConfig.samplingRate ?? 100) / 100).toFixed(2)}
OTEL_SERVICE_NAME=pipelogiq`;
    navigator.clipboard.writeText(snippet);
    setCopiedOtel(true);
    setTimeout(() => setCopiedOtel(false), 2000);
  };

  const applyLocalTempoDefaults = () => {
    saveMutation.mutate({
      type: "opentelemetry",
      config: {
        endpoint: "tempo:4317",
        protocol: "grpc",
        headers: "",
        samplingRate: 100,
        traceLinkTemplate: localTempoTraceLinkTemplate,
      },
    });
  };

  const applyLocalGrafanaDefaults = () => {
    saveMutation.mutate({
      type: "grafana",
      config: {
        dashboardUrl: "http://localhost:3000",
      },
    });
  };

  const statusToVariant = (s: string) => {
    switch (s) {
      case "connected": return "success" as const;
      case "configured": return "warning" as const;
      case "disconnected":
      case "error":
        return "error" as const;
      default:
        return "default" as const;
    }
  };

  const statusLabel = (s: string) => {
    switch (s) {
      case "connected": return "Connected";
      case "configured": return "Configured";
      case "disconnected": return "Disconnected";
      case "error": return "Error";
      default: return "Not configured";
    }
  };

  const hasConfigDialog = (type: IntegrationType) =>
    type === "opentelemetry" || type === "graylog" || type === "grafana" || type === "alerting";

  const canTest = (integration: Integration) =>
    integration.status !== "not_configured" &&
    integration.type === "opentelemetry";

  const otelIntegration = integrations.find(i => i.type === "opentelemetry");
  const logsIntegration = integrations.find(i => i.type === "graylog");
  const grafanaIntegration = integrations.find(i => i.type === "grafana");
  const alertingIntegration = integrations.find(i => i.type === "alerting");

  return (
    <TooltipProvider>
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Manage connections to your observability backends. Tempo + Grafana local defaults are available for one-click setup.
        </p>

        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {integrations.map((integration) => {
            const grafanaConfig = (integration.config || {}) as Partial<GrafanaConfig>;
            const alertingConfig = (integration.config || {}) as Partial<AlertingConfig>;
            return (
              <div
                key={integration.type}
                className={cn(
                  "rounded-xl border bg-card p-5 transition-all hover:shadow-soft",
                  integration.status === "connected" ? "border-green-200" :
                  integration.status === "error" || integration.status === "disconnected" ? "border-red-200" :
                  "border-border",
                )}
              >
                {/* Header */}
                <div className="mb-3 flex items-start justify-between">
                  <div className="flex items-center gap-3">
                    <span className="text-2xl">{integration.icon}</span>
                    <div>
                      <h4 className="font-semibold text-foreground">{integration.name}</h4>
                      <p className="text-sm text-muted-foreground">{integration.description}</p>
                    </div>
                  </div>
                </div>

                {/* Status */}
                <div className="mb-3 flex items-center gap-2">
                  <StatusBadge status={statusToVariant(integration.status)} size="sm">
                    {statusLabel(integration.status)}
                  </StatusBadge>
                  {integration.lastTestedAt && (
                    <span className="text-xs text-muted-foreground">
                      Tested {formatDistanceToNow(new Date(integration.lastTestedAt), { addSuffix: true })}
                    </span>
                  )}
                </div>

                {/* Error */}
                {integration.lastError && (
                  <div className="mb-3 rounded-md border border-red-200 bg-red-50 p-2">
                    <p className="text-xs text-red-700">{integration.lastError}</p>
                  </div>
                )}

                {/* Actions */}
                <div className="flex flex-wrap gap-2">
                  {hasConfigDialog(integration.type) && (
                    <Button
                      variant={integration.status === "not_configured" ? "default" : "outline"}
                      size="sm"
                      className="gap-1.5"
                      onClick={() => handleConfigure(integration.type)}
                    >
                      <Settings className="h-3.5 w-3.5" />
                      {integration.status === "not_configured" ? "Configure" : "Edit"}
                    </Button>
                  )}

                  {!hasConfigDialog(integration.type) && (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span>
                          <Button variant="outline" size="sm" className="gap-1.5" disabled>
                            <Settings className="h-3.5 w-3.5" />
                            Configure
                          </Button>
                        </span>
                      </TooltipTrigger>
                      <TooltipContent>Coming soon</TooltipContent>
                    </Tooltip>
                  )}

                  {canTest(integration) && (
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1.5"
                      onClick={() => handleTest(integration.type)}
                      disabled={testMutation.isPending}
                    >
                      {testMutation.isPending ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Zap className="h-3.5 w-3.5" />
                      )}
                      Test
                    </Button>
                  )}

                  {integration.type === "opentelemetry" && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1.5"
                      onClick={applyLocalTempoDefaults}
                      disabled={saveMutation.isPending}
                    >
                      Use Local Tempo
                    </Button>
                  )}

                  {integration.type === "grafana" && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1.5"
                      onClick={applyLocalGrafanaDefaults}
                      disabled={saveMutation.isPending}
                    >
                      Use Local Grafana
                    </Button>
                  )}

                  {integration.type === "grafana" && grafanaConfig.dashboardUrl && (
                    <Button asChild variant="ghost" size="sm" className="gap-1.5">
                      <a href={grafanaConfig.dashboardUrl} target="_blank" rel="noreferrer">
                        <ExternalLink className="h-3.5 w-3.5" />
                        Open
                      </a>
                    </Button>
                  )}

                  {integration.type === "opentelemetry" && integration.status !== "not_configured" && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1.5"
                      onClick={() => handleCopyOtelConfig(integration)}
                    >
                      {copiedOtel ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                      {copiedOtel ? "Copied" : "Copy Config"}
                    </Button>
                  )}
                </div>

                {integration.type === "alerting" && (
                  <div className="mt-3 space-y-1 text-xs text-muted-foreground">
                    <div>
                      Channels: {Array.isArray(alertingConfig.channels) && alertingConfig.channels.length > 0
                        ? alertingConfig.channels.join(", ")
                        : "not configured"}
                    </div>
                    <div>
                      Events: {Array.isArray(alertingConfig.enabledEvents) ? alertingConfig.enabledEvents.length : 0}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>

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
        {alertingIntegration && (
          <ConfigureAlertingDialog
            open={alertingDialogOpen}
            onOpenChange={setAlertingDialogOpen}
            integration={alertingIntegration}
          />
        )}
      </div>
    </TooltipProvider>
  );
}
