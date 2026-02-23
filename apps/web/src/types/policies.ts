export type PolicyType = 'rate_limit' | 'retry' | 'timeout' | 'circuit_breaker';

export type PolicyStatus = 'active' | 'paused' | 'disabled';

export type PolicyEnvironment = 'prod' | 'staging' | 'dev' | 'all';

export interface PolicyTargeting {
  pipelines: string[];
  stages: string[];
  handlers: string[];
  tagsInclude: string[];
  tagsExclude: string[];
}

export interface RateLimitRule {
  limit: number;
  windowSeconds: number;
  keyBy: 'global' | 'tenant' | 'user' | 'custom';
  burst?: number;
}

export interface RetryRule {
  maxAttempts: number;
  backoff: 'fixed' | 'exponential';
  baseDelayMs: number;
  maxDelayMs?: number;
  jitter?: boolean;
  retryOn?: {
    httpStatus?: number[];
    errorCodes?: string[];
  };
}

export interface TimeoutRule {
  timeoutMs: number;
  appliesTo: 'step' | 'external_call';
}

export interface CircuitBreakerRule {
  failureThreshold: number;
  windowSeconds: number;
  openSeconds: number;
  halfOpenMaxCalls: number;
}

interface BasePolicy {
  id: string;
  name: string;
  description?: string;
  status: PolicyStatus;
  environment: PolicyEnvironment;
  targeting: PolicyTargeting;
  createdAt: string;
  createdBy: string;
  updatedAt: string;
  updatedBy: string;
  version: number;
}

export type Policy =
  | (BasePolicy & { type: 'rate_limit'; rule: RateLimitRule })
  | (BasePolicy & { type: 'retry'; rule: RetryRule })
  | (BasePolicy & { type: 'timeout'; rule: TimeoutRule })
  | (BasePolicy & { type: 'circuit_breaker'; rule: CircuitBreakerRule });

export type PolicyRule = Policy['rule'];

export type PolicyEventType =
  | 'created'
  | 'updated'
  | 'enabled'
  | 'disabled'
  | 'paused'
  | 'resumed'
  | 'deleted'
  | 'triggered';

export interface PolicyEvent {
  id: string;
  policyId: string;
  ts: string;
  actor: string;
  type: PolicyEventType;
  details?: Record<string, unknown>;
}

export type PolicyListItem = Policy & {
  lastTriggeredAt?: string;
  triggerCountInRange: number;
};

export interface PolicyListResponse {
  items: PolicyListItem[];
  totalCount: number;
}

export type PolicyDetailResponse = Policy & {
  lastTriggeredAt?: string;
  triggerCountInRange: number;
};

export interface PolicyAuditResponse {
  policyId: string;
  events: PolicyEvent[];
}

export interface PolicyInsightsTopPolicy {
  id: string;
  name: string;
  triggers: number;
}

export interface PolicyInsightsResponse {
  activePoliciesCount: number;
  policiesTriggered: number;
  actionsBlockedThrottled: number;
  topPolicy?: PolicyInsightsTopPolicy;
}

export interface PolicyTargetOption {
  id: string;
  name: string;
}

export interface PolicyTargetOptionsResponse {
  environments: PolicyEnvironment[];
  pipelines: PolicyTargetOption[];
  stages: string[];
  handlers: string[];
  tags: string[];
}

export interface PolicyPreviewRequest {
  environment: PolicyEnvironment;
  targeting: PolicyTargeting;
}

export interface PolicyPreviewResponse {
  pipelines: number;
  stages: number;
  handlers: number;
}

export type PolicyRange = '15m' | '1h' | '24h' | '7d';

export interface ListPoliciesParams {
  search?: string;
  type?: PolicyType | 'all';
  status?: PolicyStatus | 'all';
  env?: PolicyEnvironment | 'all';
  pipelineId?: string | 'all';
  range?: PolicyRange;
  sortBy?: 'triggers' | 'lastTriggered' | 'updatedAt';
  sortDir?: 'asc' | 'desc';
}

export type CreatePolicyRequest = Omit<Policy, 'id' | 'createdAt' | 'createdBy' | 'updatedAt' | 'updatedBy' | 'version'> & {
  status?: PolicyStatus;
};

export type UpdatePolicyRequest = Omit<Policy, 'id' | 'createdAt' | 'createdBy' | 'updatedAt' | 'updatedBy' | 'version'> & {
  status?: PolicyStatus;
};
