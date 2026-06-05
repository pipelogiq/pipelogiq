import { PipelineStep, StepStatus } from "./PipelineGraph";
import type { PipelineAction } from "@/components/pipelines/PipelineDetailPanel";
import { StatusBadge } from "@/components/ui/status-badge";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  RotateCcw,
  Clock,
  AlertTriangle,
  Code,
  FileJson,
  Loader2,
  Shield,
} from "lucide-react";
import { useRerunStage } from "@/hooks/use-pipelines";
import { useEffectiveStagePolicies } from "@/hooks/use-policies";
import type {
  PolicyRuntimeAction,
  PolicyRuntimeMatchedPolicy,
  PolicyRuntimeResolvedRule,
} from "@/types/policies";

interface StepDetailsPanelProps {
  step: PipelineStep;
  action?: PipelineAction | null;
  onClose: () => void;
}

const statusLabels: Record<StepStatus, string> = {
  success: "Completed",
  error: "Failed",
  running: "Running",
  waiting: "Waiting",
  rescheduled: "Rescheduled",
  throttled: "Throttled",
  paused: "Paused",
  queued: "Not Started",
  skipped: "Skipped",
};

function prettyJson(value: Record<string, unknown> | undefined): string {
  if (!value || Object.keys(value).length === 0) {
    return "{}";
  }
  return JSON.stringify(value, null, 2);
}

function runtimeActionTone(action: PolicyRuntimeAction): "default" | "secondary" | "destructive" | "outline" {
  switch (action) {
    case "block":
      return "destructive";
    case "throttle":
      return "secondary";
    default:
      return "outline";
  }
}

function runtimeActionLabel(action: PolicyRuntimeAction): string {
  switch (action) {
    case "block":
      return "Block";
    case "throttle":
      return "Throttle";
    default:
      return "Allow";
  }
}

function outcomeTone(outcome?: string): "default" | "secondary" | "destructive" | "outline" {
  switch (outcome) {
    case "triggered":
      return "destructive";
    case "selected":
      return "default";
    case "merged":
      return "secondary";
    default:
      return "outline";
  }
}

function outcomeLabel(outcome?: string): string {
  switch (outcome) {
    case "triggered":
      return "Triggered";
    case "selected":
      return "Selected";
    case "shadowed":
      return "Shadowed";
    case "ignored":
      return "Ignored";
    case "merged":
      return "Merged";
    default:
      return "Candidate";
  }
}

function resolvedRuleTitle(rule: PolicyRuntimeResolvedRule): string {
  switch (rule.type) {
    case "rate_limit":
      return "Rate limit";
    case "retry":
      return "Retry";
    case "timeout":
      return "Timeout";
    case "circuit_breaker":
      return "Circuit breaker";
    default:
      return rule.type;
  }
}

function MatchedPolicyCard({ policy }: { policy: PolicyRuntimeMatchedPolicy }) {
  return (
    <div className="rounded-lg border border-border bg-background/60 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <p className="font-medium text-foreground">{policy.name}</p>
        <Badge variant="outline">{policy.type}</Badge>
        <Badge variant={outcomeTone(policy.outcome)}>{outcomeLabel(policy.outcome)}</Badge>
        <Badge variant="outline">{policy.source === "system" ? "System" : "Inline"}</Badge>
      </div>
      <div className="mt-2 flex flex-wrap gap-3 text-xs text-muted-foreground">
        <span>Precedence #{policy.precedence ?? "—"}</span>
        <span>Specificity {policy.specificityScore ?? 0}</span>
        <span>Action {runtimeActionLabel(policy.action)}</span>
      </div>
      {policy.explanation ? (
        <p className="mt-2 text-sm text-foreground/90">{policy.explanation}</p>
      ) : null}
      {policy.reason ? (
        <p className="mt-2 text-sm text-muted-foreground">Runtime reason: {policy.reason}</p>
      ) : null}
      {policy.effectiveTimeoutMs ? (
        <p className="mt-2 text-xs text-muted-foreground">Effective timeout: {policy.effectiveTimeoutMs}ms</p>
      ) : null}
    </div>
  );
}

export function StepDetailsPanel({ step, action, onClose }: StepDetailsPanelProps) {
  const rerunStage = useRerunStage();
  const stageId = Number(step.id);
  const effectivePoliciesQuery = useEffectiveStagePolicies(Number.isFinite(stageId) && stageId > 0 ? stageId : null);

  const logs = action?.logs?.trim() ? action.logs : "No logs captured yet.";
  const input = prettyJson(action?.input);
  const output = prettyJson(action?.output);

  return (
    <div className="flex h-full flex-col rounded-xl border border-border bg-card animate-slide-in-right">
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

      {step.throttleInfo && (
        <div className="mx-5 mt-4 flex items-center gap-2 rounded-lg bg-status-throttled-bg p-3">
          <AlertTriangle className="h-4 w-4 text-status-throttled" />
          <span className="text-sm text-status-throttled">{step.throttleInfo}</span>
        </div>
      )}

      {step.error && (
        <div className="mx-5 mt-4 flex items-start gap-2 rounded-lg bg-status-error-bg p-3">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-status-error" />
          <div>
            <p className="text-sm font-medium text-status-error">Error</p>
            <p className="mt-0.5 text-sm text-status-error/80">{step.error}</p>
          </div>
        </div>
      )}

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

      <Tabs defaultValue="policy" className="flex-1 overflow-hidden">
        <TabsList className="mx-5 mt-4 flex flex-wrap">
          <TabsTrigger value="policy" className="gap-1.5">
            <Shield className="h-3.5 w-3.5" />
            Policy
          </TabsTrigger>
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

        <TabsContent value="policy" className="flex-1 overflow-auto p-5">
          {effectivePoliciesQuery.isLoading ? (
            <div className="flex min-h-[180px] items-center justify-center rounded-lg border border-dashed border-border">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : effectivePoliciesQuery.error ? (
            <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
              {effectivePoliciesQuery.error instanceof Error ? effectivePoliciesQuery.error.message : "Failed to load effective policy resolution."}
            </div>
          ) : effectivePoliciesQuery.data ? (
            <div className="space-y-4">
              <div className="grid gap-3 md:grid-cols-3">
                <div className="rounded-lg border border-border p-4">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Runtime action</p>
                  <div className="mt-2 flex items-center gap-2">
                    <Badge variant={runtimeActionTone(effectivePoliciesQuery.data.action)}>
                      {runtimeActionLabel(effectivePoliciesQuery.data.action)}
                    </Badge>
                    {effectivePoliciesQuery.data.throttleUntil ? (
                      <span className="text-xs text-muted-foreground">until {new Date(effectivePoliciesQuery.data.throttleUntil).toLocaleString()}</span>
                    ) : null}
                  </div>
                </div>

                <div className="rounded-lg border border-border p-4">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Effective timeout</p>
                  <p className="mt-2 text-sm font-medium text-foreground">
                    {effectivePoliciesQuery.data.effectiveTimeoutMs ? `${effectivePoliciesQuery.data.effectiveTimeoutMs}ms` : "—"}
                  </p>
                </div>

                <div className="rounded-lg border border-border p-4">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Matched policies</p>
                  <p className="mt-2 text-sm font-medium text-foreground">
                    {effectivePoliciesQuery.data.matchedPolicies?.length ?? 0}
                  </p>
                </div>
              </div>

              {effectivePoliciesQuery.data.reason ? (
                <div className="rounded-lg border border-border bg-muted/40 p-4">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Runtime reason</p>
                  <p className="mt-2 text-sm text-foreground">{effectivePoliciesQuery.data.reason}</p>
                </div>
              ) : null}

              {(effectivePoliciesQuery.data.resolvedRules ?? []).length > 0 ? (
                <div className="space-y-3">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Resolved rules</p>
                  {(effectivePoliciesQuery.data.resolvedRules ?? []).map(rule => (
                    <div key={`${rule.type}-${rule.strategy}`} className="rounded-lg border border-border p-4">
                      <div className="flex flex-wrap items-center gap-2">
                        <p className="font-medium text-foreground">{resolvedRuleTitle(rule)}</p>
                        <Badge variant="outline">{rule.strategy}</Badge>
                        {rule.winningPolicyId ? (
                          <Badge>{rule.winningPolicyId}</Badge>
                        ) : null}
                      </div>
                      {rule.effectiveTimeoutMs ? (
                        <p className="mt-2 text-sm text-foreground">Effective timeout: {rule.effectiveTimeoutMs}ms</p>
                      ) : null}
                      {rule.explanation ? (
                        <p className="mt-2 text-sm text-muted-foreground">{rule.explanation}</p>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : null}

              {(effectivePoliciesQuery.data.explain ?? []).length > 0 ? (
                <div className="rounded-lg border border-border p-4">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Explainability</p>
                  <div className="mt-3 space-y-2">
                    {(effectivePoliciesQuery.data.explain ?? []).map(line => (
                      <p key={line} className="text-sm text-foreground">{line}</p>
                    ))}
                  </div>
                </div>
              ) : null}

              {(effectivePoliciesQuery.data.matchedPolicies ?? []).length > 0 ? (
                <div className="space-y-3">
                  <p className="text-xs uppercase tracking-wide text-muted-foreground">Matched policies</p>
                  {(effectivePoliciesQuery.data.matchedPolicies ?? []).map(policy => (
                    <MatchedPolicyCard key={policy.policyId} policy={policy} />
                  ))}
                </div>
              ) : (
                <div className="rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground">
                  No matching policies for this stage.
                </div>
              )}
            </div>
          ) : (
            <div className="rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground">
              No policy data available for this stage.
            </div>
          )}
        </TabsContent>

        <TabsContent value="logs" className="flex-1 overflow-auto p-5">
          <pre className="overflow-x-auto rounded-lg bg-foreground/5 p-4 font-mono text-xs leading-relaxed text-foreground/80">
            {logs}
          </pre>
        </TabsContent>

        <TabsContent value="input" className="flex-1 overflow-auto p-5">
          <pre className="overflow-x-auto rounded-lg bg-foreground/5 p-4 font-mono text-xs text-foreground/80">
            {input}
          </pre>
        </TabsContent>

        <TabsContent value="output" className="flex-1 overflow-auto p-5">
          <pre className="overflow-x-auto rounded-lg bg-foreground/5 p-4 font-mono text-xs text-foreground/80">
            {output}
          </pre>
        </TabsContent>
      </Tabs>
    </div>
  );
}
