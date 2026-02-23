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
import { CheckCircle2, XCircle, PlayCircle, Clock, Timer, Pause, Loader2, Circle, Ban } from "lucide-react";
import { format } from "date-fns";
import type { UIStatus } from "@/types/api";

const stageStatusIcons: Record<UIStatus, typeof CheckCircle2> = {
  success: CheckCircle2,
  error: XCircle,
  running: PlayCircle,
  waiting: Clock,
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
}

export function PipelineTable({ pipelines, selectedId, onSelect, isPanelOpen = false }: PipelineTableProps) {
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
          <TableHead className="w-[80px] text-sm font-semibold text-foreground">ID</TableHead>
          <TableHead className="text-sm font-semibold text-foreground">Name</TableHead>
          <TableHead className="text-sm font-semibold text-foreground">Status</TableHead>
          <TableHead className="text-sm font-semibold text-foreground">Created at</TableHead>
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

          return (
            <TableRow
              key={pipeline.id}
              onClick={() => onSelect(pipeline)}
              className={cn(
                "cursor-pointer transition-colors h-12 border-b border-border",
                isSelected ? "bg-primary/10 border-l-2 border-l-primary" : "hover:bg-muted/50"
              )}
            >
              <TableCell className="py-3 font-mono text-sm font-medium text-foreground">
                {pipeline.executionNumber}
              </TableCell>
              <TableCell className="py-3">
                <span className="font-semibold text-sm text-foreground">
                  {pipeline.pipelineName}
                </span>
              </TableCell>
              <TableCell className="py-3">
                <StatusBadge
                  status={pipeline.status}
                  size="sm"
                  pulse={pipeline.status === "running"}
                >
                  {pipeline.status === "success" ? "Completed" :
                   pipeline.status === "error" ? "Failed" :
                   pipeline.status === "queued" ? "Not Started" :
                   pipeline.status === "waiting" ? "Pending" :
                   pipeline.status.charAt(0).toUpperCase() + pipeline.status.slice(1)}
                </StatusBadge>
              </TableCell>
              <TableCell className="py-3 text-sm text-foreground font-mono">
                {pipeline.startedAt}
              </TableCell>
              <TableCell className="py-3 text-sm text-foreground font-mono">
                {pipeline.completedAt || "-"}
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
