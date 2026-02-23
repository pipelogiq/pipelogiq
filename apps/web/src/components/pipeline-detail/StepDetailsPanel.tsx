import { PipelineStep, StepStatus } from "./PipelineGraph";
import { StatusBadge } from "@/components/ui/status-badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  RotateCcw,
  SkipForward,
  Pause,
  Square,
  Clock,
  AlertTriangle,
  Code,
  FileJson,
  Loader2,
} from "lucide-react";
import { useRerunStage } from "@/hooks/use-pipelines";

interface StepDetailsPanelProps {
  step: PipelineStep;
  onClose: () => void;
}

const statusLabels: Record<StepStatus, string> = {
  success: "Completed",
  error: "Failed",
  running: "Running",
  waiting: "Waiting",
  throttled: "Throttled",
  paused: "Paused",
  queued: "Not Started",
  skipped: "Skipped",
};

const mockLogs = `[2024-01-15T10:23:45.123Z] INFO  Starting step execution
[2024-01-15T10:23:45.125Z] DEBUG Fetching configuration from cache
[2024-01-15T10:23:45.130Z] INFO  Configuration loaded successfully
[2024-01-15T10:23:45.135Z] DEBUG Processing payload: { orderId: "ord_12345" }
[2024-01-15T10:23:45.245Z] INFO  Validation completed
[2024-01-15T10:23:45.350Z] DEBUG Calling external service...
[2024-01-15T10:23:45.890Z] INFO  External service responded with status 200
[2024-01-15T10:23:45.895Z] INFO  Step completed successfully`;

const mockInput = {
  orderId: "ord_12345",
  userId: "usr_abc123",
  amount: 99.99,
  currency: "USD",
  items: [
    { sku: "ITEM-001", quantity: 2 },
    { sku: "ITEM-002", quantity: 1 },
  ],
};

const mockOutput = {
  success: true,
  validatedAt: "2024-01-15T10:23:45.245Z",
  paymentIntentId: "pi_xyz789",
  status: "processing",
};

export function StepDetailsPanel({ step, onClose }: StepDetailsPanelProps) {
  const rerunStage = useRerunStage();

  return (
    <div className="flex h-full flex-col rounded-xl border border-border bg-card animate-slide-in-right">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-border px-5 py-4">
        <div>
          <h3 className="font-semibold text-foreground">{step.name}</h3>
          <div className="mt-1 flex items-center gap-2">
            <StatusBadge status={step.status} pulse={step.status === "running"}>
              {statusLabels[step.status]}
            </StatusBadge>
            {step.duration && (
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <Clock className="h-3 w-3" />
                {step.duration}
              </span>
            )}
          </div>
        </div>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground"
        >
          ✕
        </button>
      </div>

      {/* Throttle Info */}
      {step.throttleInfo && (
        <div className="mx-5 mt-4 flex items-center gap-2 rounded-lg bg-status-throttled-bg p-3">
          <AlertTriangle className="h-4 w-4 text-status-throttled" />
          <span className="text-sm text-status-throttled">{step.throttleInfo}</span>
        </div>
      )}

      {/* Error Info */}
      {step.error && (
        <div className="mx-5 mt-4 flex items-start gap-2 rounded-lg bg-status-error-bg p-3">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-status-error" />
          <div>
            <p className="text-sm font-medium text-status-error">Error</p>
            <p className="mt-0.5 text-sm text-status-error/80">{step.error}</p>
          </div>
        </div>
      )}

      {/* Actions */}
      {step.status === "error" && (
        <div className="flex gap-2 border-b border-border px-5 py-4">
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5"
            disabled={rerunStage.isPending}
            onClick={() => rerunStage.mutate(Number(step.id))}
          >
            {rerunStage.isPending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RotateCcw className="h-3.5 w-3.5" />
            )}
            Rerun Stage
          </Button>
        </div>
      )}

      {/* Content Tabs */}
      <Tabs defaultValue="logs" className="flex-1 overflow-hidden">
        <TabsList className="mx-5 mt-4">
          <TabsTrigger value="logs" className="gap-1.5">
            <Code className="h-3.5 w-3.5" />
            Logs
          </TabsTrigger>
          <TabsTrigger value="input" className="gap-1.5">
            <FileJson className="h-3.5 w-3.5" />
            Input
          </TabsTrigger>
          <TabsTrigger value="output" className="gap-1.5">
            <FileJson className="h-3.5 w-3.5" />
            Output
          </TabsTrigger>
        </TabsList>

        <TabsContent value="logs" className="flex-1 overflow-auto p-5">
          <pre className="rounded-lg bg-foreground/5 p-4 font-mono text-xs leading-relaxed text-foreground/80 overflow-x-auto">
            {mockLogs}
          </pre>
        </TabsContent>

        <TabsContent value="input" className="flex-1 overflow-auto p-5">
          <pre className="rounded-lg bg-foreground/5 p-4 font-mono text-xs text-foreground/80 overflow-x-auto">
            {JSON.stringify(mockInput, null, 2)}
          </pre>
        </TabsContent>

        <TabsContent value="output" className="flex-1 overflow-auto p-5">
          <pre className="rounded-lg bg-foreground/5 p-4 font-mono text-xs text-foreground/80 overflow-x-auto">
            {JSON.stringify(mockOutput, null, 2)}
          </pre>
        </TabsContent>
      </Tabs>
    </div>
  );
}
