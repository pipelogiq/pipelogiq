// User types
export interface User {
  id: number;
  firstName: string;
  lastName?: string;
  email: string;
  role: string;
  createdAt: string;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface VersionInfoResponse {
  version: string;
  commit: string;
  buildDate: string;
  goVersion: string;
}

// Pipeline types
export interface PipelineResponse {
  id: number;
  name: string;
  traceId?: string;
  status: PipelineStatus;
  createdAt: string;
  finishedAt?: string;
  applicationId?: number;
  stageStatuses?: string[];
  stages?: StageResponse[];
  pipelineContextItems?: ContextItem[];
  pipelineKeywords?: PipelineKeyword[];
  isEvent?: boolean;
}

export interface StageResponse {
  id: number;
  pipelineId: number;
  spanId?: string;
  name: string;
  stageHandlerName?: string;
  description?: string;
  status?: StageStatus;
  createdAt: string;
  finishedAt?: string;
  startedAt?: string;
  nextRetryAt?: string;
  output?: string;
  input?: string;
  isSkipped?: boolean;
  isEvent?: boolean;
  nextStageId?: number;
  logs?: StageLog[];
  options?: StageOptions;
  failureCount?: number;
  lastFailedAt?: string;
  hasFailureHistory?: boolean;
}

export interface StageLog {
  id?: number;
  stageId?: number;
  message: string;
  logLevel?: string;
  created: string;
}

export interface StageOptions {
  runNextIfFailed?: boolean;
  retryInterval?: number;
  timeOut?: number;
  maxRetries?: number;
  dependsOn?: string[];
  runInParallelWith?: string[];
  failIfOutputEmpty?: boolean;
  notifyOnFailure?: boolean;
  runAsUser?: string;
}

export interface ContextItem {
  key: string;
  value: string;
  valueType?: string;
}

export interface PipelineKeyword {
  key: string;
  value: string;
}

// Pagination
export interface PagedResult<T> {
  items: T[];
  totalCount: number;
  pageNumber: number;
  pageSize: number;
}

export interface GetPipelinesParams {
  pageNumber?: number;
  pageSize?: number;
  applicationId?: number;
  search?: string;
  keywords?: string[];
  statuses?: string[];
  pipelineStartFrom?: string;
  pipelineStartTo?: string;
  pipelineEndFrom?: string;
  pipelineEndTo?: string;
}

// Stage actions
export interface RerunStageRequest {
  stageId: number;
  rerunAllNextStages: boolean;
}

export interface SkipStageRequest {
  stageId: number;
}

export type BulkPipelineAction = 'pause' | 'resume' | 'rerun' | 'skip';

export interface BulkPipelineActionRequest {
  action: BulkPipelineAction;
  pipelineIds?: number[];
  stageIds?: number[];
  rerunAllNextStages?: boolean;
}

export interface BulkPipelineActionItemResult {
  id: number;
  scope: 'pipeline' | 'stage' | string;
  success: boolean;
  error?: string;
}

export interface BulkPipelineActionResponse {
  action: BulkPipelineAction;
  requested: number;
  succeeded: number;
  failed: number;
  results: BulkPipelineActionItemResult[];
}

// Application types
export interface ApplicationResponse {
  id: number;
  name: string;
  description?: string;
  apiKeys?: ApiKeyResponse[];
}

export interface SaveApplicationRequest {
  id?: number;
  name: string;
  description?: string;
}

// ApiKey types
export interface ApiKeyResponse {
  id: number;
  applicationId: number;
  name?: string;
  key?: string;
  createdAt?: string;
  disabledAt?: string;
  expiresAt?: string;
  lastUsed?: string;
}

export interface GenerateApiKeyRequest {
  apiKeyId?: number;
  applicationId?: number;
  newApplication?: {
    name: string;
    description?: string;
  };
  name?: string;
  expiresAt?: string;
}

export interface DisableApiKeyRequest {
  apiKeyId: number;
}

// Worker runtime types
export type WorkerState =
  | 'starting'
  | 'ready'
  | 'degraded'
  | 'draining'
  | 'stopped'
  | 'error'
  | 'offline';

export interface WorkerStatusResponse {
  id: string;
  applicationId: number;
  applicationName: string;
  appId: string;
  workerName: string;
  instanceId: string;
  workerVersion?: string;
  sdkVersion?: string;
  environment?: string;
  hostName?: string;
  pid?: number;
  state: WorkerState;
  effectiveState: WorkerState;
  statusReason?: string;
  brokerType?: string;
  brokerConnected: boolean;
  inFlightJobs: number;
  jobsProcessed: number;
  jobsFailed: number;
  queueLag?: number;
  cpuPercent?: number;
  memoryMb?: number;
  lastError?: string;
  startedAt: string;
  lastSeenAt: string;
  stoppedAt?: string;
  updatedAt: string;
  supportedHandlers?: string[];
  capabilities?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface WorkerStatusListResponse {
  items: WorkerStatusResponse[];
  totalCount: number;
  onlineCount: number;
  offlineCount: number;
  degradedCount: number;
  offlineAfterSec: number;
}

export interface WorkerEventResponse {
  id: number;
  workerId: string;
  workerName: string;
  applicationId: number;
  applicationName: string;
  ts: string;
  level: string;
  eventType: string;
  message: string;
  details?: Record<string, unknown>;
}

// Status types
export type PipelineStatus = 'NotStarted' | 'Pending' | 'Running' | 'Paused' | 'Completed' | 'Failed' | 'Cancelled';
export type StageStatus =
  | 'NotStarted'
  | 'Running'
  | 'Pending'
  | 'RetryScheduled'
  | 'Throttled'
  | 'WaitingForApproval'
  | 'Completed'
  | 'Failed'
  | 'Skipped';

// UI status mapping (map backend status to UI status)
export type UIStatus = 'success' | 'error' | 'running' | 'waiting' | 'rescheduled' | 'throttled' | 'paused' | 'queued' | 'skipped';

export function mapPipelineStatusToUI(status: PipelineStatus): UIStatus {
  switch (status) {
    case 'Completed':
      return 'success';
    case 'Failed':
      return 'error';
    case 'Paused':
    case 'Cancelled':
      return 'paused';
    case 'Running':
      return 'running';
    case 'Pending':
      return 'waiting';
    case 'NotStarted':
      return 'queued';
    default:
      return 'waiting';
  }
}

export function mapStageStatusToUI(status?: StageStatus): UIStatus {
  if (!status) return 'queued';
  switch (status) {
    case 'Completed':
      return 'success';
    case 'Failed':
      return 'error';
    case 'Running':
      return 'running';
    case 'Pending':
    case 'WaitingForApproval':
      return 'waiting';
    case 'RetryScheduled':
      return 'rescheduled';
    case 'Throttled':
      return 'throttled';
    case 'Skipped':
      return 'skipped';
    case 'NotStarted':
      return 'queued';
    default:
      return 'waiting';
  }
}
