import { Clock, User, ChevronRight, Activity } from "lucide-react";
import { StatusBadge } from "@/components/ui/status-badge";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { PipelineExecution } from "./PipelineDetailPanel";

interface PipelineListItemProps {
  pipeline: PipelineExecution;
  isSelected: boolean;
  onClick: () => void;
}

export function PipelineListItem({ pipeline, isSelected, onClick }: PipelineListItemProps) {
  const completedActions = pipeline.actions.filter(a => a.status === "success").length;
  const totalActions = pipeline.actions.length;
  const progressPercent = (completedActions / totalActions) * 100;

  return (
    <button
      onClick={onClick}
      className={cn(
        "w-full text-left p-4 border-b border-border transition-colors hover:bg-muted/50",
        isSelected && "bg-primary/5 border-l-2 border-l-primary"
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1">
            <span className="font-medium text-foreground truncate">
              {pipeline.pipelineName}
            </span>
            <StatusBadge 
              status={pipeline.status} 
              size="sm"
              pulse={pipeline.status === "running"}
            >
              {pipeline.status}
            </StatusBadge>
          </div>
          
          <p className="text-xs text-muted-foreground truncate mb-2">
            {pipeline.description}
          </p>

          {/* Progress bar for running pipelines */}
          {pipeline.status === "running" && (
            <div className="mb-2">
              <div className="h-1.5 bg-muted rounded-full overflow-hidden">
                <div 
                  className="h-full bg-primary transition-all duration-500"
                  style={{ width: `${progressPercent}%` }}
                />
              </div>
              <span className="text-[10px] text-muted-foreground mt-0.5">
                {completedActions}/{totalActions} actions
              </span>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <div className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              <span>{pipeline.startedAt}</span>
            </div>
            <div className="flex items-center gap-1">
              <User className="h-3 w-3" />
              <span>{pipeline.owner}</span>
            </div>
            <div className="flex items-center gap-1">
              <Activity className="h-3 w-3" />
              <span>#{pipeline.executionNumber.toLocaleString()}</span>
            </div>
          </div>

          <div className="flex flex-wrap gap-1 mt-2">
            <Badge variant="outline" className="text-[10px] h-5">
              {pipeline.environment}
            </Badge>
            {pipeline.tags.slice(0, 2).map(tag => (
              <Badge key={tag} variant="secondary" className="text-[10px] h-5">
                {tag}
              </Badge>
            ))}
            {pipeline.tags.length > 2 && (
              <Badge variant="secondary" className="text-[10px] h-5">
                +{pipeline.tags.length - 2}
              </Badge>
            )}
          </div>
        </div>

        <ChevronRight className={cn(
          "h-5 w-5 text-muted-foreground shrink-0 transition-transform",
          isSelected && "rotate-90 text-primary"
        )} />
      </div>
    </button>
  );
}
