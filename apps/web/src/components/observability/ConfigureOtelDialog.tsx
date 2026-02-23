import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { StatusBadge } from "@/components/ui/status-badge";
import { Copy, Check, Loader2, Zap } from "lucide-react";
import { useSaveIntegrationConfig, useTestConnection } from "@/hooks/use-observability";
import type { Integration, OtelConfig } from "@/types/observability";

interface ConfigureOtelDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  integration: Integration;
}

export function ConfigureOtelDialog({ open, onOpenChange, integration }: ConfigureOtelDialogProps) {
  const existing = (integration.config || {}) as Partial<OtelConfig>;

  const [endpoint, setEndpoint] = useState(existing.endpoint || "");
  const [protocol, setProtocol] = useState<"grpc" | "http">(existing.protocol || "grpc");
  const [headers, setHeaders] = useState(existing.headers || "");
  const [samplingRate, setSamplingRate] = useState(String(existing.samplingRate ?? 100));
  const [traceLinkTemplate, setTraceLinkTemplate] = useState(existing.traceLinkTemplate || "");
  const [copied, setCopied] = useState(false);

  // Sync form state from latest integration config each time the dialog opens
  useEffect(() => {
    if (open) {
      const cfg = (integration.config || {}) as Partial<OtelConfig>;
      setEndpoint(cfg.endpoint || "");
      setProtocol(cfg.protocol || "grpc");
      setHeaders(cfg.headers || "");
      setSamplingRate(String(cfg.samplingRate ?? 100));
      setTraceLinkTemplate(cfg.traceLinkTemplate || "");
    }
  }, [open, integration.config]);

  const saveMutation = useSaveIntegrationConfig();
  const testMutation = useTestConnection();

  const handleSave = () => {
    saveMutation.mutate(
      {
        type: "opentelemetry",
        config: { endpoint, protocol, headers, samplingRate: Number(samplingRate), traceLinkTemplate },
      },
      { onSuccess: () => onOpenChange(false) },
    );
  };

  const handleTest = () => {
    testMutation.mutate("opentelemetry");
  };

  const otelSnippet = `# Environment variables for OpenTelemetry SDK
OTEL_EXPORTER_OTLP_ENDPOINT=${endpoint || "<your-endpoint>"}
OTEL_EXPORTER_OTLP_PROTOCOL=${protocol}
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=${(Number(samplingRate) / 100).toFixed(2)}
OTEL_SERVICE_NAME=pipelogiq${headers ? `\nOTEL_EXPORTER_OTLP_HEADERS=${headers.split("\n").join(",")}` : ""}`;

  const handleCopyConfig = () => {
    navigator.clipboard.writeText(otelSnippet);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Configure OpenTelemetry</DialogTitle>
          <DialogDescription>
            Set up OTLP trace export from Pipelogiq to your observability backend (Tempo default: <code>tempo:4317</code>).
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="space-y-2">
            <Label htmlFor="otel-endpoint">OTLP Endpoint</Label>
            <Input
              id="otel-endpoint"
              placeholder="tempo:4317"
              value={endpoint}
              onChange={(e) => setEndpoint(e.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label>Protocol</Label>
            <Select value={protocol} onValueChange={(v) => setProtocol(v as "grpc" | "http")}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="grpc">gRPC (port 4317)</SelectItem>
                <SelectItem value="http">HTTP/protobuf (port 4318)</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="otel-headers">Headers (key=value, one per line)</Label>
            <Textarea
              id="otel-headers"
              placeholder={"Authorization=Bearer <token>\nX-Custom-Header=value"}
              value={headers}
              onChange={(e) => setHeaders(e.target.value)}
              rows={3}
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="otel-sampling">Sampling Rate (%)</Label>
              <Input
                id="otel-sampling"
                type="number"
                min={0}
                max={100}
                value={samplingRate}
                onChange={(e) => setSamplingRate(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>Status</Label>
              <div className="flex items-center h-10">
                <StatusBadge
                  status={
                    integration.status === "connected" ? "success" :
                    integration.status === "disconnected" || integration.status === "error" ? "error" :
                    integration.status === "configured" ? "warning" :
                    "default"
                  }
                >
                  {integration.status === "not_configured" ? "Not configured" : integration.status}
                </StatusBadge>
              </div>
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="otel-trace-link">Trace Link Template</Label>
            <Input
              id="otel-trace-link"
              placeholder="http://localhost:3000/explore?...${traceId}"
              value={traceLinkTemplate}
              onChange={(e) => setTraceLinkTemplate(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {"Use ${traceId} as placeholder. Used by 'View in APM' buttons."}
            </p>
          </div>

          {/* Config snippet */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>OTel Config Snippet</Label>
              <Button variant="ghost" size="sm" className="gap-1.5 h-7" onClick={handleCopyConfig}>
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                {copied ? "Copied" : "Copy"}
              </Button>
            </div>
            <pre className="rounded-md bg-muted p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap">
              {otelSnippet}
            </pre>
          </div>

          {/* Save error */}
          {saveMutation.isError && (
            <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
              Failed to save: {saveMutation.error instanceof Error ? saveMutation.error.message : "Unknown error"}
            </div>
          )}

          {/* Test result */}
          {testMutation.data && (
            <div className={`rounded-md p-3 text-sm ${testMutation.data.success ? "bg-green-50 text-green-800 border border-green-200" : "bg-red-50 text-red-800 border border-red-200"}`}>
              {testMutation.data.message}
              {testMutation.data.latencyMs != null && ` (${testMutation.data.latencyMs}ms)`}
            </div>
          )}
          {testMutation.isError && (
            <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
              Test failed: {testMutation.error instanceof Error ? testMutation.error.message : "Unknown error"}
            </div>
          )}
        </div>

        <DialogFooter className="gap-2 sm:gap-0">
          <Button
            variant="outline"
            onClick={handleTest}
            disabled={!endpoint || testMutation.isPending}
            className="gap-1.5"
          >
            {testMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Zap className="h-4 w-4" />}
            Test Connection
          </Button>
          <Button onClick={handleSave} disabled={!endpoint || saveMutation.isPending}>
            {saveMutation.isPending && <Loader2 className="h-4 w-4 animate-spin mr-1.5" />}
            Save Configuration
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
