import { useState } from "react";
import { X, Clock, ChevronDown, Copy, Check, CheckCircle2, XCircle, AlertCircle, Pause, Circle, Loader2, RotateCcw, ExternalLink, SkipForward, Ban } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { PipelineExecution, PipelineAction } from "./PipelineDetailPanel";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { usePipeline, useRerunStage, useSkipStage } from "@/hooks/use-pipelines";
import { Button } from "@/components/ui/button";

interface PipelineSidePanelProps {
  pipelineId: number;
  onClose: () => void;
}

type TabType = "stages" | "logs" | "context";

export function PipelineSidePanel({ pipelineId, onClose }: PipelineSidePanelProps) {
  const { data: pipeline, isLoading, error } = usePipeline(pipelineId);
  const [copied, setCopied] = useState(false);
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

  const copyId = () => {
    navigator.clipboard.writeText(pipeline.correlationId);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

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
            <div className="p-4 font-mono text-sm leading-relaxed text-slate-800 bg-white m-4 rounded-lg border border-slate-200">
              {pipeline.actions
                .flatMap(a => a.logs ? a.logs.split("\n") : [])
                .filter(Boolean)
                .map((line, i) => (
                  <p key={i} className="whitespace-pre-wrap pb-2">{line}</p>
                ))}
              {pipeline.actions.every(a => !a.logs) && (
                <p className="text-slate-500">No logs available</p>
              )}
            </div>
          </ScrollArea>
        )}

        {activeTab === "context" && (
          <ScrollArea className="h-full">
            <div className="p-4">
              <div className="bg-white rounded-lg border border-slate-200 overflow-hidden">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 bg-slate-50">
                      <th className="px-4 py-3 text-left font-bold text-slate-700 uppercase text-xs tracking-wider">Key</th>
                      <th className="px-4 py-3 text-left font-bold text-slate-700 uppercase text-xs tracking-wider">Value</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pipeline.context.map((ctx, index) => (
                      <tr key={index} className="border-b border-slate-100 last:border-0 hover:bg-slate-50">
                        <td className="px-4 py-3 font-mono font-bold text-slate-900">
                          {ctx.key}
                        </td>
                        <td className="px-4 py-3 font-mono text-slate-700 break-all">
                          {ctx.value}
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

function StageCard({ action, index, traceUrl, isExpanded, onToggle }: StageCardProps) {
  const rerunStage = useRerunStage();
  const skipStage = useSkipStage();
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
            {/* Output Preview */}
            <div className="rounded-lg bg-white border-2 border-slate-200 p-3">
              <p className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-2">
                Output
              </p>
              <p className={cn(
                "text-sm font-medium max-w-full whitespace-pre-wrap break-all",
                action.status === "error" ? "text-red-700" : "text-slate-800"
              )}>
                {getOutputPreview()}
              </p>
            </div>

          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  );
}
