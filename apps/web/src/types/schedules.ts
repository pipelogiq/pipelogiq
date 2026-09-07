/** Mirrors internal/types/schedule.go. */

export type ScheduleKind = 'Cron' | 'Interval' | 'Once';
export type ScheduleStatus = 'Active' | 'Paused' | 'Archived';
export type OverlapPolicy = 'Skip' | 'Queue' | 'Replace' | 'Allow';
export type CatchupPolicy = 'None' | 'One' | 'All';
export type ScheduleTrigger = 'Cron' | 'Interval' | 'Once' | 'Manual' | 'Catchup';
export type ScheduleRunOutcome = 'Created' | 'Skipped' | 'Failed';
export type ScheduleSkipReason = 'Overlap' | 'Paused' | 'Catchup';

export interface ScheduleHealth {
  recentRuns: number;
  recentFailed: number;
  recentSkipped: number;
  /** Newest first, at most 20 entries. */
  outcomes: ScheduleRunOutcome[];
  lagSeconds?: number;
}

export interface ScheduleDefinitionStage {
  stageName: string;
  stageHandlerName: string;
  description?: string;
  input?: string;
}

export interface ScheduleDefinition {
  name: string;
  stages?: ScheduleDefinitionStage[];
  pipelineKeywords?: { key: string; value: string }[];
  pipelineContextItems?: { key: string; value: string; valueType?: string }[];
}

export interface Schedule {
  id: number;
  applicationId: number;
  name: string;
  status: ScheduleStatus;
  kind: ScheduleKind;
  cronExpression?: string;
  timeZone: string;
  intervalSeconds?: number;
  runAt?: string;
  definition: ScheduleDefinition;
  definitionVersion: number;
  overlapPolicy: OverlapPolicy;
  catchupPolicy: CatchupPolicy;
  catchupMax: number;
  jitterSeconds: number;
  nextRunAt?: string;
  lastRunAt?: string;
  lastRunStatus?: ScheduleRunOutcome;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
  health?: ScheduleHealth;
}

export interface ScheduleRun {
  id: number;
  scheduleId: number;
  scheduledFor: string;
  firedAt: string;
  trigger: ScheduleTrigger;
  outcome: ScheduleRunOutcome;
  skipReason?: ScheduleSkipReason;
  pipelineId?: number;
  definitionVersion: number;
  error?: string;
  lagSeconds: number;
  pipelineStatus?: string;
  pipelineFinishedAt?: string;
}

export interface ScheduleListResponse {
  schedules: Schedule[];
  nextCursor?: string;
}

export interface ScheduleRunListResponse {
  runs: ScheduleRun[];
  nextCursor?: string;
}

export interface TriggerScheduleResponse {
  runId: number;
  pipelineId: number;
  trigger: ScheduleTrigger;
}
