import { useState } from "react";
import { X, Clock, User, Calendar, Tag, Play, Pause, RotateCcw, Square, ChevronRight, CheckCircle2, XCircle, PlayCircle, Timer, Loader2, Circle, Ban } from "lucide-react";
import { StatusBadge } from "@/components/ui/status-badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import type { UIStatus } from "@/types/api";

export interface PipelineAction {
  id: string;
  name: string;
  spanId?: string;
  status: UIStatus;
  duration?: string;
  startedAt?: string;
  completedAt?: string;
  retries?: number;
  throttleInfo?: string;
  error?: string;
  logs: string;
  input: Record<string, unknown>;
  output: Record<string, unknown>;
}

export interface PipelineContext {
  key: string;
  value: string;
}

export interface PipelineExecution {
  id: string;
  pipelineId: string;
  pipelineName: string;
  description: string;
  status: UIStatus;
  environment: string;
  startedAt: string;
  completedAt?: string;
  duration?: string;
  owner: string;
  tags: string[];
  correlationId: string;
  context: PipelineContext[];
  actions: PipelineAction[];
  stages: { name: string; status: UIStatus; startedAt?: string; finishedAt?: string }[];
  executionNumber: number;
  traceId: string;
  traceUrl: string;
  logTraceUrl: string;
}

interface PipelineDetailPanelProps {
  pipeline: PipelineExecution;
  onClose: () => void;
}

const statusIcons: Record<UIStatus, typeof CheckCircle2> = {
  success: CheckCircle2,
  error: XCircle,
  running: PlayCircle,
  waiting: Clock,
  throttled: Timer,
  paused: Pause,
  queued: Circle,
  skipped: Ban,
};

const statusColors: Record<UIStatus, string> = {
  success: "text-status-success",
  error: "text-status-error",
  running: "text-status-running",
  waiting: "text-muted-foreground",
  throttled: "text-status-throttled",
  paused: "text-status-paused",
  queued: "text-slate-400",
  skipped: "text-violet-500",
};

export function PipelineDetailPanel({ pipeline, onClose }: PipelineDetailPanelProps) {
  const [selectedAction, setSelectedAction] = useState<PipelineAction | null>(null);

  return (
    <div className="flex h-full flex-col bg-background">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-border px-5 py-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold text-foreground truncate">
              {pipeline.pipelineName}
            </h2>
            <StatusBadge status={pipeline.status} pulse={pipeline.status === "running"}>
              {pipeline.status}
            </StatusBadge>
          </div>
          <p className="mt-1 text-sm text-muted-foreground truncate">
            {pipeline.description}
          </p>
        </div>
        <button
          onClick={onClose}
          className="ml-4 rounded-lg p-2 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
        >
          <X className="h-5 w-5" />
        </button>
      </div>

      {/* Metadata */}
      <div className="border-b border-border px-5 py-3 bg-muted/30">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
          <div className="flex items-center gap-1.5 text-muted-foreground">
            <User className="h-3.5 w-3.5" />
            <span>{pipeline.owner}</span>
          </div>
          <div className="flex items-center gap-1.5 text-muted-foreground">
            <Clock className="h-3.5 w-3.5" />
            <span>{pipeline.duration || "In progress..."}</span>
          </div>
          <div className="flex items-center gap-1.5 text-muted-foreground">
            <Calendar className="h-3.5 w-3.5" />
            <span>#{pipeline.executionNumber.toLocaleString()}</span>
          </div>
          <div className="flex items-center gap-1.5">
            <Tag className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="font-mono text-xs text-muted-foreground">
              {pipeline.correlationId}
            </span>
          </div>
        </div>
        <div className="flex flex-wrap gap-1.5 mt-2">
          <Badge variant="outline" className="text-xs">
            {pipeline.environment}
          </Badge>
          {pipeline.tags.map(tag => (
            <Badge key={tag} variant="secondary" className="text-xs">
              {tag}
            </Badge>
          ))}
        </div>
      </div>

      {/* Actions Bar */}
      <div className="flex gap-2 border-b border-border px-5 py-3">
        <Button variant="outline" size="sm" className="gap-1.5">
          <Play className="h-3.5 w-3.5" />
          Resume
        </Button>
        <Button variant="outline" size="sm" className="gap-1.5">
          <Pause className="h-3.5 w-3.5" />
          Pause
        </Button>
        <Button variant="outline" size="sm" className="gap-1.5">
          <RotateCcw className="h-3.5 w-3.5" />
          Restart
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="gap-1.5 text-status-error hover:bg-status-error-bg hover:text-status-error"
        >
          <Square className="h-3.5 w-3.5" />
          Stop
        </Button>
      </div>

      {/* Tabs Content */}
      <Tabs defaultValue="actions" className="flex-1 flex flex-col min-h-0">
        <TabsList className="mx-5 mt-3 justify-start w-fit">
          <TabsTrigger value="actions">Actions</TabsTrigger>
          <TabsTrigger value="context">Context</TabsTrigger>
          <TabsTrigger value="history">History</TabsTrigger>
          <TabsTrigger value="logs">Full Logs</TabsTrigger>
        </TabsList>

        <TabsContent value="actions" className="flex-1 m-0 flex min-h-0">
          <div className="flex flex-1 min-h-0">
            {/* Actions List */}
            <ScrollArea className="w-1/2 border-r border-border">
              <div className="divide-y divide-border">
                {pipeline.actions.map((action, index) => {
                  const Icon = statusIcons[action.status];
                  const isSelected = selectedAction?.id === action.id;

                  return (
                    <button
                      key={action.id}
                      onClick={() => setSelectedAction(action)}
                      className={cn(
                        "w-full flex items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/50",
                        isSelected && "bg-muted"
                      )}
                    >
                      <div className="flex items-center justify-center w-6 h-6">
                        {action.status === "running" ? (
                          <Loader2 className={cn("h-4 w-4 animate-spin", statusColors[action.status])} />
                        ) : (
                          <Icon className={cn("h-4 w-4", statusColors[action.status])} />
                        )}
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-xs text-muted-foreground font-mono">
                            {index + 1}.
                          </span>
                          <span className="font-medium text-foreground truncate text-sm">
                            {action.name}
                          </span>
                        </div>
                        {action.throttleInfo && (
                          <p className="text-xs text-status-throttled mt-0.5">
                            {action.throttleInfo}
                          </p>
                        )}
                        {action.error && (
                          <p className="text-xs text-status-error mt-0.5 truncate">
                            {action.error}
                          </p>
                        )}
                      </div>
                      {action.duration && (
                        <span className="text-xs font-mono text-muted-foreground">
                          {action.duration}
                        </span>
                      )}
                      <ChevronRight className={cn(
                        "h-4 w-4 text-muted-foreground transition-transform",
                        isSelected && "rotate-90"
                      )} />
                    </button>
                  );
                })}
              </div>
            </ScrollArea>

            {/* Action Details */}
            <div className="w-1/2 flex flex-col min-h-0">
              {selectedAction ? (
                <ActionDetailView action={selectedAction} />
              ) : (
                <div className="flex-1 flex items-center justify-center text-muted-foreground text-sm">
                  Select an action to view details
                </div>
              )}
            </div>
          </div>
        </TabsContent>

        <TabsContent value="context" className="flex-1 m-0 p-5 overflow-auto">
          <div className="rounded-lg border border-border">
            <table className="w-full">
              <thead>
                <tr className="border-b border-border bg-muted/50">
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Key
                  </th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Value
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {pipeline.context.map((ctx, index) => (
                  <tr key={index} className="hover:bg-muted/30">
                    <td className="px-4 py-2.5 font-mono text-sm text-foreground">
                      {ctx.key}
                    </td>
                    <td className="px-4 py-2.5 font-mono text-sm text-muted-foreground break-all">
                      {ctx.value}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </TabsContent>

        <TabsContent value="history" className="flex-1 m-0 p-5 overflow-auto">
          <div className="space-y-4">
            <div className="text-sm text-muted-foreground">
              Recent executions of this pipeline
            </div>
            <div className="space-y-2">
              {[
                { id: "1", time: "2 min ago", status: "running" as const, duration: "..." },
                { id: "2", time: "15 min ago", status: "success" as const, duration: "2.3s" },
                { id: "3", time: "1 hour ago", status: "success" as const, duration: "1.8s" },
                { id: "4", time: "3 hours ago", status: "error" as const, duration: "12.5s" },
                { id: "5", time: "5 hours ago", status: "success" as const, duration: "2.1s" },
              ].map((exec) => (
                <div
                  key={exec.id}
                  className="flex items-center gap-3 rounded-lg border border-border p-3 hover:bg-muted/30 cursor-pointer"
                >
                  <StatusBadge status={exec.status} size="sm">
                    {exec.status}
                  </StatusBadge>
                  <span className="flex-1 text-sm text-foreground">{exec.time}</span>
                  <span className="text-sm font-mono text-muted-foreground">{exec.duration}</span>
                </div>
              ))}
            </div>
          </div>
        </TabsContent>

        <TabsContent value="logs" className="flex-1 m-0 p-5 overflow-auto">
          <pre className="rounded-lg bg-foreground/5 p-4 font-mono text-xs leading-relaxed text-foreground/80 overflow-x-auto whitespace-pre-wrap">
            {pipeline.actions.map(a => a.logs).join("\n\n")}
          </pre>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function ActionDetailView({ action }: { action: PipelineAction }) {
  return (
    <Tabs defaultValue="logs" className="flex-1 flex flex-col min-h-0">
      <div className="px-4 pt-3 pb-2 border-b border-border">
        <div className="flex items-center gap-2 mb-2">
          <h4 className="font-medium text-foreground">{action.name}</h4>
          <StatusBadge status={action.status} size="sm">
            {action.status}
          </StatusBadge>
        </div>
        <TabsList className="h-8">
          <TabsTrigger value="logs" className="text-xs h-7">Logs</TabsTrigger>
          <TabsTrigger value="input" className="text-xs h-7">Input</TabsTrigger>
          <TabsTrigger value="output" className="text-xs h-7">Output</TabsTrigger>
        </TabsList>
      </div>

      <TabsContent value="logs" className="flex-1 m-0 p-4 overflow-auto">
        <pre className="rounded-lg bg-foreground/5 p-3 font-mono text-xs leading-relaxed text-foreground/80 whitespace-pre-wrap">
          {action.logs}
        </pre>
      </TabsContent>

      <TabsContent value="input" className="flex-1 m-0 p-4 overflow-auto">
        <pre className="rounded-lg bg-foreground/5 p-3 font-mono text-xs text-foreground/80 whitespace-pre-wrap">
          {JSON.stringify(action.input, null, 2)}
        </pre>
      </TabsContent>

      <TabsContent value="output" className="flex-1 m-0 p-4 overflow-auto">
        <pre className="rounded-lg bg-foreground/5 p-3 font-mono text-xs text-foreground/80 whitespace-pre-wrap">
          {JSON.stringify(action.output, null, 2)}
        </pre>
      </TabsContent>
    </Tabs>
  );
}
