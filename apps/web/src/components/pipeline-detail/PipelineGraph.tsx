import { useState } from "react";
import { cn } from "@/lib/utils";
import {
  CheckCircle2,
  XCircle,
  PlayCircle,
  Clock,
  Timer,
  Pause,
  Circle,
  Ban,
} from "lucide-react";

export type StepStatus = "success" | "error" | "running" | "waiting" | "throttled" | "paused" | "queued" | "skipped";

export interface PipelineStep {
  id: string;
  name: string;
  status: StepStatus;
  duration?: string;
  retries?: number;
  throttleInfo?: string;
  error?: string;
}

interface PipelineGraphProps {
  steps: PipelineStep[];
  selectedStepId?: string;
  onStepSelect: (step: PipelineStep) => void;
}

const statusIcons: Record<StepStatus, typeof CheckCircle2> = {
  success: CheckCircle2,
  error: XCircle,
  running: PlayCircle,
  waiting: Clock,
  throttled: Timer,
  paused: Pause,
  queued: Circle,
  skipped: Ban,
};

const statusStyles: Record<StepStatus, string> = {
  success: "border-status-success bg-status-success-bg",
  error: "border-status-error bg-status-error-bg",
  running: "border-status-running bg-status-running-bg",
  waiting: "border-border bg-muted",
  throttled: "border-status-throttled bg-status-throttled-bg",
  paused: "border-status-paused bg-status-paused-bg",
  queued: "border-slate-200 bg-slate-50",
  skipped: "border-violet-200 bg-violet-50",
};

const iconColors: Record<StepStatus, string> = {
  success: "text-status-success",
  error: "text-status-error",
  running: "text-status-running",
  waiting: "text-muted-foreground",
  throttled: "text-status-throttled",
  paused: "text-status-paused",
  queued: "text-slate-400",
  skipped: "text-violet-500",
};

export function PipelineGraph({ steps, selectedStepId, onStepSelect }: PipelineGraphProps) {
  return (
    <div className="rounded-xl border border-border bg-card p-6">
      <h3 className="mb-6 font-semibold text-foreground">Pipeline Flow</h3>
      
      <div className="flex items-start gap-2 overflow-x-auto pb-4">
        {steps.map((step, index) => {
          const Icon = statusIcons[step.status];
          const isSelected = step.id === selectedStepId;
          
          return (
            <div key={step.id} className="flex items-center">
              {/* Node */}
              <button
                onClick={() => onStepSelect(step)}
                className={cn(
                  "relative flex min-w-[140px] flex-col items-center gap-2 rounded-xl border-2 p-4 transition-all",
                  statusStyles[step.status],
                  isSelected && "ring-2 ring-primary ring-offset-2",
                  step.status === "running" && "animate-pulse-ring",
                  "hover:scale-105"
                )}
              >
                <Icon className={cn("h-6 w-6", iconColors[step.status])} />
                <span className="text-sm font-medium text-foreground text-center">
                  {step.name}
                </span>
                {step.duration && (
                  <span className="text-xs font-mono text-muted-foreground">
                    {step.duration}
                  </span>
                )}
                {step.retries && step.retries > 0 && (
                  <span className="absolute -right-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full bg-status-warning text-[10px] font-bold text-white">
                    {step.retries}
                  </span>
                )}
                {step.throttleInfo && (
                  <span className="text-[10px] text-status-throttled">
                    {step.throttleInfo}
                  </span>
                )}
              </button>

              {/* Arrow */}
              {index < steps.length - 1 && (
                <div className="flex items-center px-2">
                  <div className="h-0.5 w-8 bg-edge" />
                  <div className="h-0 w-0 border-y-[5px] border-l-[8px] border-y-transparent border-l-edge" />
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
