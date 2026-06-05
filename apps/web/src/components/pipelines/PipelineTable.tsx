import { StatusBadge } from "@/components/ui/status-badge";
import { cn } from "@/lib/utils";
import { PipelineExecution } from "./PipelineDetailPanel";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from "@/components/ui/tooltip";
import { Checkbox } from "@/components/ui/checkbox";
import { AlertTriangle, CheckCircle2, XCircle, PlayCircle, Clock, Timer, Pause, Loader2, Circle, Ban, RotateCcw } from "lucide-react";
import { format } from "date-fns";
import type { UIStatus } from "@/types/api";

const stageStatusIcons: Record<UIStatus, typeof CheckCircle2> = {
  success: CheckCircle2,
  error: XCircle,
  running: PlayCircle,
  waiting: Clock,
  rescheduled: RotateCcw,
  throttled: Timer,
  paused: Pause,
  queued: Circle,
  skipped: Ban,
};

const stageStatusColors: Record<UIStatus, string> = {
  success: "text-status-success",
  error: "text-status-error",
  running: "text-status-running",
  waiting: "text-muted-foreground",
  rescheduled: "text-status-warning",
  throttled: "text-status-throttled",
  paused: "text-status-paused",
  queued: "text-slate-400",
  skipped: "text-violet-500",
};

interface PipelineTableProps {
  pipelines: PipelineExecution[];
  selectedId: string | null;
  onSelect: (pipeline: PipelineExecution) => void;
  isPanelOpen?: boolean;
  selectedBulkIds?: Set<string>;
  onToggleBulkSelection?: (pipelineId: string, selected: boolean) => void;
  onToggleAllBulkSelection?: (selected: boolean) => void;
  allBulkSelected?: boolean;
  someBulkSelected?: boolean;
}

function pipelineStatusLabel(status: UIStatus): string {
  switch (status) {
    case "success":
      return "Completed";
    case "error":
      return "Failed";
    case "queued":
      return "Not Started";
    case "waiting":
      return "Pending";
    case "rescheduled":
      return "Rescheduled";
    default:
      return status.charAt(0).toUpperCase() + status.slice(1);
  }
}

export function PipelineTable({
  pipelines,
  selectedId,
  onSelect,
  isPanelOpen = false,
  selectedBulkIds = new Set<string>(),
  onToggleBulkSelection,
  onToggleAllBulkSelection,
  allBulkSelected = false,
  someBulkSelected = false,
}: PipelineTableProps) {
  if (pipelines.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 px-4 text-center">
        <p className="text-muted-foreground mb-2">No pipelines match your search</p>
        <p className="text-xs text-muted-foreground">
          Try adjusting your filters or search query
        </p>
      </div>
    );
  }

  return (
    <TooltipProvider>
    <Table>
      <TableHeader className="sticky top-0 bg-background z-10">
        <TableRow className="hover:bg-transparent border-b-2 border-border">
          <TableHead className="w-[42px]">
            <Checkbox
              checked={allBulkSelected ? true : someBulkSelected ? "indeterminate" : false}
              onCheckedChange={(checked) => onToggleAllBulkSelection?.(checked === true)}
              aria-label="Select all visible pipelines"
            />
          </TableHead>
          <TableHead className="w-[80px] text-sm font-semibold text-foreground">ID</TableHead>
          <TableHead className="text-sm font-semibold text-foreground">Name</TableHead>
          <TableHead className="text-sm font-semibold text-foreground">Status</TableHead>
          <TableHead className="text-sm font-semibold text-foreground">Created at</TableHead>
          <TableHead className="text-sm font-semibold text-foreground">Duration</TableHead>
          <TableHead className="text-sm font-semibold text-foreground">Finished at</TableHead>
          {!isPanelOpen && (
            <>
              <TableHead className="text-sm font-semibold text-foreground">Stages</TableHead>
            </>
          )}
        </TableRow>
      </TableHeader>
      <TableBody>
        {pipelines.map((pipeline) => {
          const isSelected = selectedId === pipeline.id;
          const isBulkSelected = selectedBulkIds.has(pipeline.id);
          const hasFailureHistory = pipeline.stages.some((stage) => stage.hasFailureHistory);
          const nextScheduledAt = pipeline.stages
            .filter((stage) => (stage.status === "rescheduled" || stage.status === "throttled") && stage.nextRetryAt)
            .map((stage) => ({
              raw: stage.nextRetryAt!,
              label: stage.nextRetryAtExact || format(new Date(stage.nextRetryAt!), "yyyy-MM-dd HH:mm:ss"),
            }))
            .sort((left, right) => new Date(left.raw).getTime() - new Date(right.raw).getTime())[0];

          return (
            <TableRow
              key={pipeline.id}
              onClick={() => onSelect(pipeline)}
              className={cn(
                "cursor-pointer transition-colors h-12 border-b border-border",
                isSelected ? "bg-primary/10 border-l-2 border-l-primary" : "hover:bg-muted/50"
              )}
            >
              <TableCell
                className="py-3"
                onClick={(event) => event.stopPropagation()}
              >
                <Checkbox
                  checked={isBulkSelected}
                  onCheckedChange={(checked) => onToggleBulkSelection?.(pipeline.id, checked === true)}
                  aria-label={`Select pipeline ${pipeline.executionNumber}`}
                />
              </TableCell>
              <TableCell className="py-3 font-mono text-sm font-medium text-foreground">
                {pipeline.executionNumber}
              </TableCell>
              <TableCell className="py-3">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm font-semibold text-foreground">
                    {pipeline.pipelineName}
                  </span>
                  {hasFailureHistory && (
                    <span
                      className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-red-50 text-red-700"
                      title="Had a previous failure"
                      aria-label="Had a previous failure"
                    >
                      <AlertTriangle className="h-3 w-3" />
                    </span>
                  )}
                </div>
              </TableCell>
              <TableCell className="py-3">
                <div className="space-y-1">
                  <StatusBadge
                    status={pipeline.status}
                    size="sm"
                    pulse={pipeline.status === "running"}
                  >
                    {pipelineStatusLabel(pipeline.status)}
                  </StatusBadge>
                  {nextScheduledAt && (
                    <div className="font-mono text-[11px] text-muted-foreground">
                      Next {nextScheduledAt.label}
                    </div>
                  )}
                </div>
              </TableCell>
              <TableCell className="py-3 text-sm text-foreground font-mono">
                <div>{pipeline.startedAtExact}</div>
                <div className="text-xs text-muted-foreground">{pipeline.startedAt}</div>
              </TableCell>
              <TableCell className="py-3 text-sm text-foreground font-mono">
                {(pipeline.status === "success" || pipeline.status === "error" || pipeline.status === "paused")
                  ? (pipeline.duration || "-")
                  : "-"}
              </TableCell>
              <TableCell className="py-3 text-sm text-foreground font-mono">
                {(pipeline.status === "success" || pipeline.status === "error") ? (
                  pipeline.completedAtExact ? (
                    <div>
                      <div>{pipeline.completedAtExact}</div>
                      <div className="text-xs text-muted-foreground">{pipeline.completedAt}</div>
                    </div>
                  ) : "-"
                ) : "-"}
              </TableCell>
              {!isPanelOpen && (
                <>

                  <TableCell className="py-3">
                    <div className="flex flex-wrap gap-1">
                      {pipeline.stages.map((stage, idx) => {
                        const Icon = stageStatusIcons[stage.status];
                        return (
                            <Tooltip key={idx}>
                              <TooltipTrigger asChild>
                          <span>
                            <StatusBadge status={stage.status} size="sm" pulse={stage.status === "running"}>
                              {stage.status === "running" ? (
                                  <Loader2 className={cn("h-3 w-3 animate-spin", stageStatusColors[stage.status])} />
                              ) : (
                                  <Icon className={cn("h-3 w-3", stageStatusColors[stage.status])} />
                              )}
                            </StatusBadge>
                          </span>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="text-xs">
                                <p className="font-medium">{stage.name}</p>
                                {stage.startedAt && (
                                    <p className="text-muted-foreground">
                                      Started: {format(new Date(stage.startedAt), "MMM d, HH:mm:ss")}
                                    </p>
                                )}
                                {stage.finishedAt && (
                                    <p className="text-muted-foreground">
                                      Finished: {format(new Date(stage.finishedAt), "MMM d, HH:mm:ss")}
                                    </p>
                                )}
                                {stage.nextRetryAtExact && (
                                    <p className="text-muted-foreground">
                                      Scheduled: {stage.nextRetryAtExact}
                                    </p>
                                )}
                              </TooltipContent>
                            </Tooltip>
                        );
                      })}
                    </div>
                  </TableCell>
                </>
              )}
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
    </TooltipProvider>
  );
}
