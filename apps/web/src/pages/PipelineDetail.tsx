import { useState } from "react";
import { useParams, Link } from "react-router-dom";
import { AppHeader } from "@/components/layout/AppHeader";
import { PipelineGraph, PipelineStep } from "@/components/pipeline-detail/PipelineGraph";
import { StepDetailsPanel } from "@/components/pipeline-detail/StepDetailsPanel";
import { StatusBadge } from "@/components/ui/status-badge";
import { Button } from "@/components/ui/button";
import { usePipeline } from "@/hooks/use-pipelines";
import {
  ChevronLeft,
  Play,
  Pause,
  RotateCcw,
  Square,
  Clock,
  GitBranch,
  User,
  Calendar,
  Loader2,
} from "lucide-react";

export default function PipelineDetail() {
  const { id } = useParams();
  const pipelineId = Number(id) || 0;
  const { data: pipeline, isLoading, error } = usePipeline(pipelineId);
  const [selectedStep, setSelectedStep] = useState<PipelineStep | null>(null);

  // Convert pipeline actions to steps format
  const steps: PipelineStep[] = pipeline?.actions?.map((action) => ({
    id: action.id,
    name: action.name,
    status: action.status,
    duration: action.duration,
    throttleInfo: action.throttleInfo,
  })) || [];

  if (isLoading) {
    return (
      <div className="flex flex-col h-screen">
        <AppHeader title="Loading..." subtitle="Pipeline Execution Details" />
        <div className="flex-1 flex items-center justify-center">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      </div>
    );
  }

  if (error || !pipeline) {
    return (
      <div className="flex flex-col h-screen">
        <AppHeader title="Pipeline Not Found" subtitle="Pipeline Execution Details" />
        <div className="flex-1 flex flex-col items-center justify-center gap-4">
          <p className="text-muted-foreground">
            {error instanceof Error ? error.message : "Pipeline not found"}
          </p>
          <Link to="/pipelines">
            <Button variant="outline">
              <ChevronLeft className="h-4 w-4 mr-2" />
              Back to Pipelines
            </Button>
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-screen">
      <AppHeader
        title={pipeline.pipelineName}
        subtitle="Pipeline Execution Details"
      />

      <div className="flex-1 overflow-hidden">
        <div className="h-full flex">
          {/* Main Content */}
          <div className="flex-1 overflow-y-auto p-6 space-y-6">
            {/* Breadcrumb */}
            <Link
              to="/pipelines"
              className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <ChevronLeft className="h-4 w-4" />
              Back to Pipelines
            </Link>

            {/* Pipeline Header */}
            <div className="rounded-xl border border-border bg-card p-6">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-4">
                  <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-primary/10">
                    <GitBranch className="h-6 w-6 text-primary" />
                  </div>
                  <div>
                    <h2 className="text-xl font-semibold text-foreground">
                      {pipeline.pipelineName}
                    </h2>
                    <p className="text-sm text-muted-foreground">
                      {pipeline.description}
                    </p>
                  </div>
                </div>
                <StatusBadge status={pipeline.status} pulse={pipeline.status === "running"}>
                  {pipeline.status}
                </StatusBadge>
              </div>

              <div className="mt-6 flex flex-wrap items-center gap-6 text-sm text-muted-foreground">
                <div className="flex items-center gap-1.5">
                  <User className="h-4 w-4" />
                  <span>{pipeline.owner}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <Clock className="h-4 w-4" />
                  <span>Started {pipeline.startedAt}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <Calendar className="h-4 w-4" />
                  <span>Execution #{pipeline.executionNumber.toLocaleString()}</span>
                </div>
              </div>

              <div className="mt-6 flex gap-2">
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
                <Button variant="outline" size="sm" className="gap-1.5 text-status-error hover:bg-status-error-bg hover:text-status-error">
                  <Square className="h-3.5 w-3.5" />
                  Stop Pipeline
                </Button>
              </div>
            </div>

            {/* Pipeline Graph */}
            <PipelineGraph
              steps={steps}
              selectedStepId={selectedStep?.id}
              onStepSelect={setSelectedStep}
            />

            {/* Execution Timeline */}
            <div className="rounded-xl border border-border bg-card">
              <div className="border-b border-border px-5 py-4">
                <h3 className="font-semibold text-foreground">Execution Timeline</h3>
              </div>
              <div className="divide-y divide-border">
                {steps.map((step, index) => (
                  <div
                    key={step.id}
                    className="flex items-center gap-4 px-5 py-3 transition-colors hover:bg-muted/50 cursor-pointer"
                    onClick={() => setSelectedStep(step)}
                  >
                    <div className="w-8 text-center text-xs font-mono text-muted-foreground">
                      {index + 1}
                    </div>
                    <div className="flex-1">
                      <span className="font-medium text-foreground">{step.name}</span>
                    </div>
                    <StatusBadge status={step.status} size="sm">
                      {step.status}
                    </StatusBadge>
                    {step.duration && (
                      <span className="w-16 text-right text-sm font-mono text-muted-foreground">
                        {step.duration}
                      </span>
                    )}
                  </div>
                ))}
              </div>
            </div>
          </div>

          {/* Step Details Panel */}
          {selectedStep && (
            <div className="w-[420px] border-l border-border overflow-y-auto">
              <StepDetailsPanel
                step={selectedStep}
                onClose={() => setSelectedStep(null)}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
