import { useState } from "react";
import { X, Clock, ChevronDown, CheckCircle2, XCircle, AlertCircle, Pause, Circle, Loader2, RotateCcw, ExternalLink, SkipForward, Ban } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { PipelineAction } from "./PipelineDetailPanel";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { usePipeline, useRerunStage, useSkipStage } from "@/hooks/use-pipelines";
import { Button } from "@/components/ui/button";
import { ContextValueViewer } from "@/components/pipelines/ContextValueViewer";

interface PipelineSidePanelProps {
  pipelineId: number;
  onClose: () => void;
}

type TabType = "stages" | "logs" | "context";

type LlmUsageView = {
  provider?: string;
  model?: string;
  inputTokens?: number;
  outputTokens?: number;
  cacheReadTokens?: number;
  cacheCreationTokens?: number;
  estimatedCostUsd?: number;
};

type LlmUsageModelSummaryView = LlmUsageView & {
  calls?: number;
};

type LlmUsageSummaryView = {
  totalCalls?: number;
  inputTokens?: number;
  outputTokens?: number;
  cacheReadTokens?: number;
  cacheCreationTokens?: number;
  estimatedCostUsd?: number;
  models: LlmUsageModelSummaryView[];
};

type StageLlmOutputView = {
  raw?: string;
  llmOperation?: string;
  llmUsage?: LlmUsageView | null;
  sessionUsage?: LlmUsageSummaryView | null;
};

export function PipelineSidePanel({ pipelineId, onClose }: PipelineSidePanelProps) {
  const { data: pipeline, isLoading, error } = usePipeline(pipelineId);
  const [expandedActions, setExpandedActions] = useState<Set<string>>(new Set());
  const [activeTab, setActiveTab] = useState<TabType>("stages");

  if (isLoading) {
    return (
      <div className="h-full flex flex-col bg-white">
        <div className="flex items-center justify-end px-6 py-5 border-b-2 border-slate-200">
          <button
            onClick={onClose}
            className="p-1.5 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-200 transition-colors"
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="flex-1 flex items-center justify-center">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      </div>
    );
  }

  if (error || !pipeline) {
    return (
      <div className="h-full flex flex-col bg-white">
        <div className="flex items-center justify-end px-6 py-5 border-b-2 border-slate-200">
          <button
            onClick={onClose}
            className="p-1.5 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-200 transition-colors"
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="flex-1 flex items-center justify-center">
          <p className="text-sm text-muted-foreground">Failed to load pipeline details</p>
        </div>
      </div>
    );
  }

  const toggleAction = (actionId: string) => {
    setExpandedActions(prev => {
      const next = new Set(prev);
      if (next.has(actionId)) {
        next.delete(actionId);
      } else {
        next.add(actionId);
      }
      return next;
    });
  };

  const stageCount = pipeline.actions.length;
  const pipelineUsageSummary = getPipelineUsageSummary(pipeline.context);

  const getStatusDisplay = () => {
    switch (pipeline.status) {
      case "running":
        return { bg: "bg-blue-100", text: "text-blue-800", dot: "bg-blue-600", label: "Running" };
      case "success":
        return { bg: "bg-emerald-100", text: "text-emerald-800", dot: "bg-emerald-600", label: "Completed" };
      case "error":
        return { bg: "bg-red-100", text: "text-red-800", dot: "bg-red-600", label: "Failed" };
      case "paused":
        return { bg: "bg-amber-100", text: "text-amber-800", dot: "bg-amber-600", label: "Paused" };
      case "throttled":
        return { bg: "bg-orange-100", text: "text-orange-800", dot: "bg-orange-600", label: "Throttled" };
      case "queued":
        return { bg: "bg-slate-50", text: "text-slate-400", dot: "bg-slate-300", label: "Not Started" };
      case "skipped":
        return { bg: "bg-violet-50", text: "text-violet-600", dot: "bg-violet-400", label: "Skipped" };
      default:
        return { bg: "bg-slate-100", text: "text-slate-700", dot: "bg-slate-500", label: "Pending" };
    }
  };

  const status = getStatusDisplay();

  return (
    <div className="h-full flex flex-col bg-white">
      {/* Header */}
      <div className="px-4 py-4 bg-slate-50 border-b-2 border-slate-200">
        <div className="flex items-start justify-between gap-3">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="text-lg font-bold text-slate-900 truncate">
                {pipeline.pipelineName}
              </h2>
              <span className={cn(
                  "inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold shrink-0",
                  status.bg, status.text
              )}>
                <span className={cn("h-1.5 w-1.5 rounded-full", status.dot)} />
                {status.label}
              </span>
            </div>
          </div>
          <button
              onClick={onClose}
              className="p-1 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-200 transition-colors"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex items-center gap-4 mt-2.5 text-xs">
          <div>
            <span className="font-semibold text-slate-500 uppercase tracking-wider">Started </span>
            <span className="font-mono font-bold text-slate-900">{pipeline.startedAt}</span>
          </div>
          <span className="text-slate-300">|</span>
          <div>
            <span className="font-semibold text-slate-500 uppercase tracking-wider">Duration </span>
            <span className="font-mono font-bold text-slate-900">{pipeline.duration || "—"}</span>
          </div>
        </div>

        {pipeline.traceId && (
            <div className="mt-2.5 flex items-center justify-between gap-2 p-2 bg-white rounded-md border border-slate-200 text-xs">
              <div className="flex items-center gap-2 min-w-0">
                <span className="font-semibold text-slate-500 uppercase tracking-wider shrink-0">Trace</span>
                <span className="font-mono text-slate-600 truncate">{pipeline.traceId}</span>
              </div>
              <div className="shrink-0 flex items-center gap-1">
                {pipeline.traceUrl ? (
                  <a
                    href={pipeline.traceUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-semibold text-blue-700 bg-blue-50 hover:bg-blue-100 rounded transition-colors border border-blue-200"
                  >
                    <ExternalLink className="h-3 w-3" />
                    Trace
                  </a>
                ) : (
                  <span title="Trace url not configured">
                    <button
                      type="button"
                      disabled
                      className="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-semibold text-slate-400 bg-slate-100 rounded border border-slate-200 cursor-not-allowed"
                    >
                      <ExternalLink className="h-3 w-3" />
                      Trace
                    </button>
                  </span>
                )}
                {pipeline.logTraceUrl ? (
                  <a
                    href={pipeline.logTraceUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-semibold text-teal-700 bg-teal-50 hover:bg-teal-100 rounded transition-colors border border-teal-200"
                  >
                    <ExternalLink className="h-3 w-3" />
                    Logs
                  </a>
                ) : (
                  <span title="Logs url not configured">
                    <button
                      type="button"
                      disabled
                      className="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-semibold text-slate-400 bg-slate-100 rounded border border-slate-200 cursor-not-allowed"
                    >
                      <ExternalLink className="h-3 w-3" />
                      Logs
                    </button>
                  </span>
                )}
              </div>
            </div>
        )}
      </div>

      {/* Tabs */}
      <div className="px-6 border-b border-border bg-white">
        <div className="flex">
          {([
            { key: "stages" as TabType, label: `Stages (${stageCount})` },
            { key: "logs" as TabType, label: "Logs" },
            { key: "context" as TabType, label: "Context" },
          ]).map(tab => (
              <button
                  key={tab.key}
                  onClick={() => setActiveTab(tab.key)}
                  className={cn(
                      "flex-1 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors -mb-px text-center",
                      activeTab === tab.key
                          ? "border-primary text-foreground"
                          : "border-transparent text-muted-foreground hover:text-foreground hover:border-border"
                  )}
              >
                {tab.label}
              </button>
          ))}
        </div>
      </div>
      {/* Tab Content */}
      <div className="flex-1 min-h-0 overflow-hidden bg-slate-50">
        {activeTab === "stages" && (
          <ScrollArea className="h-full">
            <div className="p-4 space-y-3">
              {pipeline.actions.map((action, index) => (
                <StageCard
                  key={action.id}
                  action={action}
                  index={index}
                  traceUrl={pipeline.traceUrl}
                  isExpanded={expandedActions.has(action.id)}
                  onToggle={() => toggleAction(action.id)}
                />
              ))}
            </div>
          </ScrollArea>
        )}

        {activeTab === "logs" && (
          <ScrollArea className="h-full">
            <div className="p-4 space-y-4">
              {pipeline.actions.map((action, index) => {
                const hasInput = hasPayloadContent(action.input);
                const hasOutput = hasPayloadContent(action.output);
                const hasEntries = (action.logEntries?.length || 0) > 0;

                if (!hasInput && !hasOutput && !hasEntries) {
                  return null;
                }

                return (
                  <div key={action.id} className="min-w-0 overflow-hidden rounded-lg border border-slate-200 bg-white">
                    <div className="px-4 py-3 border-b border-slate-200 bg-slate-50">
                      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
                        <span className="font-mono text-xs font-bold text-slate-500">
                          #{(index + 1).toString().padStart(2, "0")}
                        </span>
                        <span className="min-w-0 break-words text-sm font-bold text-slate-900 [overflow-wrap:anywhere]">
                          {action.name}
                        </span>
                        <span className="min-w-0 break-all text-xs font-semibold uppercase tracking-wider text-slate-500">
                          {action.handlerName || "handler unknown"}
                        </span>
                      </div>
                      <div className="mt-2 flex min-w-0 flex-wrap gap-x-4 gap-y-1 font-mono text-xs text-slate-500">
                        {action.createdAt && <span>CREATED {action.createdAt}</span>}
                        {action.startedAt && <span>STARTED {action.startedAt}</span>}
                        {action.completedAt && <span>FINISHED {action.completedAt}</span>}
                        {action.duration && <span>DURATION {action.duration}</span>}
                      </div>
                    </div>

                    <div className="p-4 space-y-4">
                      {hasInput && (
                        <div>
                          <p className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-2">Input</p>
                          <pre className="w-full overflow-hidden whitespace-pre-wrap break-words rounded-md border border-slate-200 bg-slate-50 p-3 font-mono text-xs text-slate-700 [overflow-wrap:anywhere]">
                            {formatPayloadPreview(action.input)}
                          </pre>
                        </div>
                      )}

                      {(hasOutput || !hasEntries) && (
                        <div>
                          <p className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-2">Output</p>
                          {hasOutput ? (
                            <pre className="w-full overflow-hidden whitespace-pre-wrap break-words rounded-md border border-slate-200 bg-slate-50 p-3 font-mono text-xs text-slate-700 [overflow-wrap:anywhere]">
                              {formatPayloadPreview(action.output)}
                            </pre>
                          ) : (
                            <p className="w-full overflow-hidden whitespace-pre-wrap break-words rounded-md border border-slate-200 bg-slate-50 p-3 font-mono text-xs text-slate-600 [overflow-wrap:anywhere]">
                              {formatLogOutputFallback(action)}
                            </p>
                          )}
                        </div>
                      )}

                      {hasEntries && (
                        <div>
                          <p className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-2">Stage Logs</p>
                          <div className="space-y-2 overflow-hidden rounded-md border border-slate-200 bg-slate-50 p-3">
                            {action.logEntries!.map((entry, logIndex) => (
                              <p
                                key={`${action.id}-${logIndex}`}
                                className="w-full max-w-full overflow-hidden whitespace-pre-wrap break-words font-mono text-xs text-slate-800 [overflow-wrap:anywhere]"
                              >
                                [{entry.created}] {(entry.logLevel || "INFO").toUpperCase()} {entry.message}
                              </p>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                );
              })}
              {pipeline.actions.every(action =>
                !hasPayloadContent(action.input) &&
                !hasPayloadContent(action.output) &&
                (!action.logEntries || action.logEntries.length === 0)
              ) && (
                <div className="bg-white m-0 rounded-lg border border-slate-200 p-6">
                  <p className="text-slate-500">No logs available</p>
                </div>
              )}
            </div>
          </ScrollArea>
        )}

        {activeTab === "context" && (
          <ScrollArea className="h-full">
            <div className="p-4">
              {pipelineUsageSummary && (
                <div className="mb-4">
                  <UsageSummaryCard
                    title="Pipeline LLM Usage"
                    summary={pipelineUsageSummary}
                  />
                </div>
              )}
              <div className="bg-white rounded-lg border border-slate-200 overflow-hidden">
                <table className="w-full table-fixed text-sm">
                  <colgroup>
                    <col className="w-[30%]" />
                    <col className="w-[70%]" />
                  </colgroup>
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="px-4 py-3 text-left font-bold text-slate-700 uppercase text-xs tracking-wider">Key</th>
                      <th className="px-4 py-3 text-left font-bold text-slate-700 uppercase text-xs tracking-wider">Value</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pipeline.context.map((ctx, index) => (
                      <tr key={index} className="border-b border-slate-100 last:border-0 hover:bg-slate-50">
                        <td className="max-w-0 break-all px-4 py-3 align-top font-mono font-bold text-slate-900 [overflow-wrap:anywhere]">
                          {ctx.key}
                        </td>
                        <td className="min-w-0 px-4 py-3 align-top">
                          <ContextValueViewer item={ctx} className="w-full min-w-0" />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {/*<div className="mt-5">
                <p className="text-xs font-bold text-slate-600 uppercase tracking-wider mb-2">Tags</p>
                <div className="flex flex-wrap gap-2">
                  {pipeline.tags.map(tag => (
                    <span key={tag} className="px-3 py-1 text-sm font-semibold bg-slate-200 text-slate-800 rounded-full">
                      {tag}
                    </span>
                  ))}
                </div>
              </div>*/}
            </div>
          </ScrollArea>
        )}
      </div>
    </div>
  );
}

interface StageCardProps {
  action: PipelineAction;
  index: number;
  traceUrl?: string;
  isExpanded: boolean;
  onToggle: () => void;
}

function hasPayloadContent(payload: unknown): boolean {
  if (payload === null || payload === undefined) {
    return false;
  }

  if (typeof payload === "string") {
    return payload.trim().length > 0;
  }

  if (Array.isArray(payload)) {
    return payload.length > 0;
  }

  if (typeof payload === "object") {
    return Object.keys(payload as Record<string, unknown>).length > 0;
  }

  return true;
}

function formatPayloadPreview(payload: unknown): string {
  if (payload === null || payload === undefined) {
    return "";
  }

  if (typeof payload === "string") {
    return payload;
  }

  if (typeof payload === "number" || typeof payload === "boolean") {
    return String(payload);
  }

  try {
    return JSON.stringify(payload, null, 2);
  } catch {
    return String(payload);
  }
}

function formatLogOutputFallback(action: PipelineAction): string {
  if (action.status === "queued") {
    return "Not started";
  }
  if (action.status === "waiting") {
    return "Pending...";
  }
  if (action.status === "running") {
    return "Processing...";
  }
  if (action.status === "success") {
    return "Completed without structured output payload.";
  }
  if (action.status === "error") {
    return action.error || "Stage failed without structured output payload.";
  }
  return "No structured output payload.";
}

function StageCard({ action, index, traceUrl, isExpanded, onToggle }: StageCardProps) {
  const rerunStage = useRerunStage();
  const skipStage = useSkipStage();
  const parsedStageOutput = parseStageLlmOutput(action.output);
  const spanUrl = action.spanId && traceUrl
    ? `${traceUrl}${traceUrl.includes('?') ? '&' : '?'}spanId=${encodeURIComponent(action.spanId)}`
    : '';
  const getStatusIcon = () => {
    const baseClass = "h-5 w-5";
    switch (action.status) {
      case "success":
        return <CheckCircle2 className={cn(baseClass, "text-emerald-600")} />;
      case "error":
        return <XCircle className={cn(baseClass, "text-red-600")} />;
      case "running":
        return (
          <div className="h-5 w-5 flex items-center justify-center gap-0.5">
            <span className="h-2 w-2 rounded-full bg-blue-600 animate-pulse" />
            <span className="h-2 w-2 rounded-full bg-blue-600 animate-pulse [animation-delay:150ms]" />
          </div>
        );
      case "throttled":
        return <AlertCircle className={cn(baseClass, "text-orange-600")} />;
      case "paused":
        return <Pause className={cn(baseClass, "text-amber-600")} />;
      case "queued":
        return <Circle className={cn(baseClass, "text-slate-300")} />;
      case "skipped":
        return <Ban className={cn(baseClass, "text-violet-500")} />;
      default:
        return <Clock className={cn(baseClass, "text-slate-400")} />;
    }
  };

  const getStatusLabel = () => {
    switch (action.status) {
      case "success": return { text: "Completed", class: "text-emerald-700 bg-emerald-100" };
      case "error": return { text: "Failed", class: "text-red-700 bg-red-100" };
      case "running": return { text: "Running", class: "text-blue-700 bg-blue-100" };
      case "throttled": return { text: "Throttled", class: "text-orange-700 bg-orange-100" };
      case "paused": return { text: "Paused", class: "text-amber-700 bg-amber-100" };
      case "waiting": return { text: "Pending", class: "text-slate-600 bg-slate-100" };
      case "queued": return { text: "Not Started", class: "text-slate-400 bg-slate-50" };
      case "skipped": return { text: "Skipped", class: "text-violet-600 bg-violet-50" };
      default: return { text: action.status, class: "text-slate-600 bg-slate-100" };
    }
  };

  const getOutputPreview = () => {
    if (action.output && Object.keys(action.output).length > 0) {
      const firstValue = Object.values(action.output)[0];
      if (typeof firstValue === "string") return firstValue;
      if (typeof firstValue === "boolean") return firstValue ? "Success" : "Failed";
      return JSON.stringify(firstValue);
    }
    return action.status === "queued" ? "Not started" : action.status === "waiting" ? "Pending..." : "Processing...";
  };

  const statusInfo = getStatusLabel();
  const hasInput = hasPayloadContent(action.input);
  const hasOutput = hasPayloadContent(action.output);

  return (
    <Collapsible open={isExpanded} onOpenChange={onToggle}>
      <div className="border-2 border-slate-200 rounded-lg overflow-hidden bg-white shadow-sm hover:border-slate-300 transition-colors">
        <CollapsibleTrigger asChild>
          <button className="w-full flex items-center gap-3 px-4 py-3.5 hover:bg-slate-50 transition-colors text-left">
            {getStatusIcon()}
            <span className="font-mono text-sm font-bold text-slate-500">
              #{(index + 1).toString().padStart(2, '0')}
            </span>
            <span className="flex-1 text-sm font-bold text-slate-900">
              {action.name}
            </span>
            <span className={cn("text-xs font-bold px-2.5 py-1 rounded-full", statusInfo.class)}>
              {statusInfo.text}
            </span>
            <ChevronDown className={cn(
              "h-5 w-5 text-slate-400 transition-transform",
              isExpanded && "rotate-180"
            )} />
          </button>
        </CollapsibleTrigger>

        <CollapsibleContent>
          <div className="px-4 pb-4 pt-3 border-t-2 border-slate-100 bg-slate-50">
            {/* Rerun / Skip buttons for failed stages */}
            {action.status === "error" && (
                <div className="mb-3 flex gap-2">
                  <Button
                      variant="outline"
                      size="sm"
                      className="gap-1.5 flex-1 justify-center text-sm font-semibold text-red-700 bg-red-50 hover:bg-red-100 border-2 border-red-200 rounded-lg transition-colors"
                      disabled={rerunStage.isPending || skipStage.isPending}
                      onClick={(e) => {
                        e.stopPropagation();
                        rerunStage.mutate(Number(action.id));
                      }}
                  >
                    {rerunStage.isPending ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                        <RotateCcw className="h-3.5 w-3.5" />
                    )}
                    Rerun Stage
                  </Button>
                  <Button
                      variant="outline"
                      size="sm"
                      className="gap-1.5 flex-1 justify-center text-sm font-semibold text-amber-700 bg-amber-50 hover:bg-amber-100 border-2 border-amber-200 rounded-lg transition-colors"
                      disabled={rerunStage.isPending || skipStage.isPending}
                      onClick={(e) => {
                        e.stopPropagation();
                        skipStage.mutate(Number(action.id));
                      }}
                  >
                    {skipStage.isPending ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                        <SkipForward className="h-3.5 w-3.5" />
                    )}
                    Skip Stage
                  </Button>
                </div>
            )}
            <div className="grid grid-cols-3 gap-4 text-sm mb-4">
              {action.startedAt && (
                <div>
                  <p className="text-xs font-bold text-slate-500 uppercase tracking-wider mb-1">Created</p>
                  <p className="font-mono font-bold text-slate-900">{action.startedAt}</p>
                </div>
              )}
              {action.completedAt && (
                <div>
                  <p className="text-xs font-bold text-slate-500 uppercase tracking-wider mb-1">Finished</p>
                  <p className="font-mono font-bold text-slate-900">{action.completedAt}</p>
                </div>
              )}
              {action.duration && (
                <div>
                  <p className="text-xs font-bold text-slate-500 uppercase tracking-wider mb-1">Duration</p>
                  <p className="font-mono font-bold text-slate-900">{action.duration}</p>
                </div>
              )}

            </div>

            {action.spanId && (
                <div className="flex items-center justify-between mb-4 p-2 bg-white rounded-md border border-slate-200">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="text-xs font-semibold text-slate-500 uppercase tracking-wider shrink-0">Span</span>
                    <span className="font-mono text-xs text-slate-600 truncate">{action.spanId}</span>
                  </div>
                  {spanUrl && (
                      <a
                          href={spanUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="shrink-0 ml-2 inline-flex items-center gap-1 px-2 py-0.5 text-xs font-semibold text-blue-700 hover:text-blue-900 transition-colors"
                      >
                        <ExternalLink className="h-3 w-3" />
                        Span
                    </a>
                  )}
                </div>
            )}
            <div className="space-y-3">
              {hasInput && (
                <div className="rounded-lg bg-white border-2 border-slate-200 p-3">
                  <p className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-2">
                    Input
                  </p>
                  <pre className="w-full overflow-hidden whitespace-pre-wrap break-words font-mono text-xs text-slate-700 [overflow-wrap:anywhere]">
                    {formatPayloadPreview(action.input)}
                  </pre>
                </div>
              )}

              <div className="rounded-lg bg-white border-2 border-slate-200 p-3">
                <p className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-2">
                  Output
                </p>
                {parsedStageOutput ? (
                  <div className="space-y-3">
                    {parsedStageOutput.llmUsage && (
                      <UsageCard
                        title={parsedStageOutput.llmOperation
                          ? `LLM Usage · ${parsedStageOutput.llmOperation}`
                          : "LLM Usage"}
                        usage={parsedStageOutput.llmUsage}
                      />
                    )}
                    {parsedStageOutput.sessionUsage && (
                      <UsageSummaryCard
                        title="Pipeline Usage So Far"
                        summary={parsedStageOutput.sessionUsage}
                        compact
                      />
                    )}
                    {parsedStageOutput.raw && (
                      <pre className={cn(
                        "w-full overflow-hidden whitespace-pre-wrap break-words font-mono text-xs [overflow-wrap:anywhere]",
                        action.status === "error" ? "text-red-700" : "text-slate-700"
                      )}>
                        {parsedStageOutput.raw}
                      </pre>
                    )}
                  </div>
                ) : hasOutput ? (
                  <pre className={cn(
                    "w-full overflow-hidden whitespace-pre-wrap break-words font-mono text-xs [overflow-wrap:anywhere]",
                    action.status === "error" ? "text-red-700" : "text-slate-700"
                  )}>
                    {formatPayloadPreview(action.output)}
                  </pre>
                ) : (
                  <p className={cn(
                    "w-full overflow-hidden whitespace-pre-wrap break-words text-sm font-medium [overflow-wrap:anywhere]",
                    action.status === "error" ? "text-red-700" : "text-slate-800"
                  )}>
                    {getOutputPreview()}
                  </p>
                )}
              </div>
            </div>

          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  );
}

function UsageCard({ title, usage }: { title: string; usage: LlmUsageView }) {
  const providerModel = [usage.provider, usage.model].filter(Boolean).join(" · ");
  const hasCache = (usage.cacheReadTokens || 0) > 0 || (usage.cacheCreationTokens || 0) > 0;

  return (
    <div className="rounded-lg border border-blue-200 bg-blue-50/60 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs font-bold uppercase tracking-wider text-blue-800">{title}</p>
        {providerModel && (
          <span className="rounded-full border border-blue-200 bg-white px-2 py-0.5 text-[11px] font-semibold text-blue-700">
            {providerModel}
          </span>
        )}
      </div>
      <div className="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
        <UsageMetric label="Input Tokens" value={formatInteger(usage.inputTokens)} />
        <UsageMetric label="Output Tokens" value={formatInteger(usage.outputTokens)} />
        <UsageMetric label="Estimated Cost" value={formatUsd(usage.estimatedCostUsd)} />
        {hasCache ? (
          <UsageMetric
            label="Cache"
            value={`${formatInteger(usage.cacheReadTokens)} read / ${formatInteger(usage.cacheCreationTokens)} write`}
          />
        ) : (
          <UsageMetric label="Cache" value="—" />
        )}
      </div>
    </div>
  );
}

function UsageSummaryCard({
  title,
  summary,
  compact = false,
}: {
  title: string;
  summary: LlmUsageSummaryView;
  compact?: boolean;
}) {
  return (
    <div className={cn(
      "rounded-lg border border-emerald-200 bg-emerald-50/60 p-3",
      compact && "bg-emerald-50/40"
    )}>
      <p className="text-xs font-bold uppercase tracking-wider text-emerald-800">{title}</p>
      <div className="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
        <UsageMetric label="LLM Calls" value={formatInteger(summary.totalCalls)} />
        <UsageMetric label="Input Tokens" value={formatInteger(summary.inputTokens)} />
        <UsageMetric label="Output Tokens" value={formatInteger(summary.outputTokens)} />
        <UsageMetric label="Estimated Cost" value={formatUsd(summary.estimatedCostUsd)} />
      </div>
      {((summary.cacheReadTokens || 0) > 0 || (summary.cacheCreationTokens || 0) > 0) && (
        <div className="mt-2 text-xs text-emerald-900">
          Cache: {formatInteger(summary.cacheReadTokens)} read / {formatInteger(summary.cacheCreationTokens)} write
        </div>
      )}
      {summary.models.length > 0 && (
        <div className="mt-3 overflow-hidden rounded-md border border-emerald-200 bg-white">
          <table className="w-full text-xs">
            <thead className="bg-emerald-50 text-emerald-900">
              <tr>
                <th className="px-3 py-2 text-left font-bold uppercase tracking-wider">Model</th>
                <th className="px-3 py-2 text-left font-bold uppercase tracking-wider">Calls</th>
                <th className="px-3 py-2 text-left font-bold uppercase tracking-wider">Tokens</th>
                <th className="px-3 py-2 text-left font-bold uppercase tracking-wider">Cost</th>
              </tr>
            </thead>
            <tbody>
              {summary.models.map((model, index) => (
                <tr key={`${model.provider || "unknown"}-${model.model || "unknown"}-${index}`} className="border-t border-emerald-100 text-slate-700">
                  <td className="px-3 py-2 font-medium">
                    {[model.provider, model.model].filter(Boolean).join(" · ") || "Unknown model"}
                  </td>
                  <td className="px-3 py-2">{formatInteger(model.calls)}</td>
                  <td className="px-3 py-2">
                    {formatInteger((model.inputTokens || 0) + (model.outputTokens || 0))}
                  </td>
                  <td className="px-3 py-2">{formatUsd(model.estimatedCostUsd)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function UsageMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-white/80 bg-white/80 px-3 py-2">
      <p className="text-[11px] font-bold uppercase tracking-wider text-slate-500">{label}</p>
      <p className="mt-1 font-mono text-sm font-bold text-slate-900">{value}</p>
    </div>
  );
}

function parseStageLlmOutput(output: Record<string, unknown>): StageLlmOutputView | null {
  const raw = typeof output.raw === "string" ? output.raw : undefined;
  const llmOperation = typeof output.llmOperation === "string" ? output.llmOperation : undefined;
  const llmUsage = parseLlmUsage(output.llmUsage);
  const sessionUsage = parseLlmUsageSummary(output.sessionUsage);

  if (!raw && !llmOperation && !llmUsage && !sessionUsage) {
    return null;
  }

  return {
    raw,
    llmOperation,
    llmUsage,
    sessionUsage,
  };
}

function getPipelineUsageSummary(contextItems: Array<{ key: string; value: string }>): LlmUsageSummaryView | null {
  const summaryItem = contextItems.find((item) => item.key === "agent:session:usageSummary");
  if (summaryItem) {
    const parsed = parseJsonString(summaryItem.value);
    const summary = parseLlmUsageSummary(parsed);
    if (summary) {
      return summary;
    }
  }

  const lookupNumber = (key: string): number | undefined => {
    const item = contextItems.find((ctx) => ctx.key === key);
    if (!item) return undefined;
    const parsed = parseJsonString(item.value);
    return toNumber(parsed);
  };

  const totalCalls = lookupNumber("agent:session:llmCallCount");
  const inputTokens = lookupNumber("agent:session:inputTokens");
  const outputTokens = lookupNumber("agent:session:outputTokens");
  const cacheReadTokens = lookupNumber("agent:session:cacheReadTokens");
  const cacheCreationTokens = lookupNumber("agent:session:cacheCreationTokens");
  const estimatedCostUsd = lookupNumber("agent:session:estimatedCostUsd");

  if (
    totalCalls === undefined &&
    inputTokens === undefined &&
    outputTokens === undefined &&
    cacheReadTokens === undefined &&
    cacheCreationTokens === undefined &&
    estimatedCostUsd === undefined
  ) {
    return null;
  }

  return {
    totalCalls,
    inputTokens,
    outputTokens,
    cacheReadTokens,
    cacheCreationTokens,
    estimatedCostUsd,
    models: [],
  };
}

function parseLlmUsage(value: unknown): LlmUsageView | null {
  const record = asRecord(value);
  if (!record) {
    return null;
  }

  return {
    provider: toStringValue(record.provider),
    model: toStringValue(record.model),
    inputTokens: toNumber(record.inputTokens),
    outputTokens: toNumber(record.outputTokens),
    cacheReadTokens: toNumber(record.cacheReadTokens),
    cacheCreationTokens: toNumber(record.cacheCreationTokens),
    estimatedCostUsd: toNumber(record.estimatedCostUsd),
  };
}

function parseLlmUsageSummary(value: unknown): LlmUsageSummaryView | null {
  const record = asRecord(value);
  if (!record) {
    return null;
  }

  const models = Array.isArray(record.models)
    ? record.models
        .map((item): LlmUsageModelSummaryView | null => {
          const usage = parseLlmUsage(item);
          const modelRecord = asRecord(item);
          if (!usage || !modelRecord) {
            return null;
          }

          return {
            ...usage,
            calls: toNumber(modelRecord.calls),
          };
        })
        .filter((item): item is LlmUsageModelSummaryView => item !== null)
    : [];

  return {
    totalCalls: toNumber(record.totalCalls),
    inputTokens: toNumber(record.inputTokens),
    outputTokens: toNumber(record.outputTokens),
    cacheReadTokens: toNumber(record.cacheReadTokens),
    cacheCreationTokens: toNumber(record.cacheCreationTokens),
    estimatedCostUsd: toNumber(record.estimatedCostUsd),
    models,
  };
}

function parseJsonString(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function toStringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0 ? value : undefined;
}

function toNumber(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string" && value.trim().length > 0) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  return undefined;
}

function formatInteger(value?: number): string {
  if (value === undefined) {
    return "—";
  }
  return Math.round(value).toLocaleString("en-US");
}

function formatUsd(value?: number): string {
  if (value === undefined) {
    return "—";
  }

  if (value === 0) {
    return "$0.000000";
  }

  if (Math.abs(value) < 0.01) {
    return `$${value.toFixed(6)}`;
  }

  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  }).format(value);
}
