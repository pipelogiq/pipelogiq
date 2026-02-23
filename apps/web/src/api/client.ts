import type {
  User,
  LoginRequest,
  PipelineResponse,
  StageResponse,
  ContextItem,
  PagedResult,
  GetPipelinesParams,
  RerunStageRequest,
  SkipStageRequest,
  ApplicationResponse,
  SaveApplicationRequest,
  ApiKeyResponse,
  GenerateApiKeyRequest,
  DisableApiKeyRequest,
  StageLog,
  WorkerStatusListResponse,
  WorkerEventResponse,
} from '@/types/api';
import type {
  ObservabilityConfig,
  ObservabilityStatus,
  ObservabilityInsights,
  TraceEntry,
  TestConnectionResult,
  SaveIntegrationConfigRequest,
  TimeRange,
  IntegrationType,
} from '@/types/observability';
import type {
  PolicyListResponse,
  PolicyDetailResponse,
  CreatePolicyRequest,
  UpdatePolicyRequest,
  PolicyAuditResponse,
  PolicyInsightsResponse,
  PolicyTargetOptionsResponse,
  PolicyPreviewRequest,
  PolicyPreviewResponse,
  ListPoliciesParams,
  PolicyRange,
  Policy,
} from '@/types/policies';

const API_BASE = import.meta.env.VITE_API_BASE_URL || '/api';
console.log('[API] Base URL:', API_BASE);

class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

async function request<T>(
  endpoint: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(`${API_BASE}${endpoint}`, {
    ...options,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
  });

  if (!response.ok) {
    const message = await response.text();
    throw new ApiError(response.status, message || `HTTP ${response.status}`);
  }

  const contentLength = response.headers.get('content-length');
  const contentType = response.headers.get('content-type') || '';

  if (response.status === 204 || contentLength === '0' || (!contentType && !contentLength)) {
    return undefined as T;
  }

  if (contentType.includes('application/json')) {
    return response.json();
  }

  return undefined as T;
}

// Auth API
export const authApi = {
  login: async (data: LoginRequest): Promise<void> => {
    await request<void>('/auth/login', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  logout: async (): Promise<void> => {
    await request<void>('/auth/logout', {
      method: 'POST',
    });
  },

  getCurrentUser: async (): Promise<User> => {
    return request<User>('/auth/me');
  },
};

// Pipelines API
export const pipelinesApi = {
  getAll: async (params?: GetPipelinesParams): Promise<PagedResult<PipelineResponse>> => {
    const searchParams = new URLSearchParams();

    if (params?.pageNumber) searchParams.set('pageNumber', String(params.pageNumber));
    if (params?.pageSize) searchParams.set('pageSize', String(params.pageSize));
    if (params?.applicationId) searchParams.set('applicationId', String(params.applicationId));
    if (params?.search) searchParams.set('search', params.search);
    if (params?.pipelineStartFrom) searchParams.set('pipelineStartFrom', params.pipelineStartFrom);
    if (params?.pipelineStartTo) searchParams.set('pipelineStartTo', params.pipelineStartTo);
    if (params?.pipelineEndFrom) searchParams.set('pipelineEndFrom', params.pipelineEndFrom);
    if (params?.pipelineEndTo) searchParams.set('pipelineEndTo', params.pipelineEndTo);

    // Handle array params
    params?.keywords?.forEach(k => searchParams.append('keywords', k));
    params?.statuses?.forEach(s => searchParams.append('statuses', s));

    const queryString = searchParams.toString();
    return request<PagedResult<PipelineResponse>>(`/pipelines${queryString ? `?${queryString}` : ''}`);
  },

  getById: async (id: number): Promise<PipelineResponse> => {
    return request<PipelineResponse>(`/pipelines/${id}`);
  },

  getStages: async (pipelineId: number): Promise<StageResponse[]> => {
    return request<StageResponse[]>(`/pipelines/${pipelineId}/stages`);
  },

  getContext: async (pipelineId: number): Promise<ContextItem[]> => {
    return request<ContextItem[]>(`/pipelines/${pipelineId}/context`);
  },

  getLogs: async (pipelineId: number, stageId?: number): Promise<StageLog[]> => {
    const path = stageId
      ? `/pipelines/logs/${pipelineId}/${stageId}`
      : `/pipelines/logs/${pipelineId}`;
    return request<StageLog[]>(path);
  },

  rerunStage: async (data: RerunStageRequest): Promise<void> => {
    await request<void>('/pipelines/rerunStage', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  skipStage: async (data: SkipStageRequest): Promise<void> => {
    await request<void>('/pipelines/skipStage', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },
};

// Applications API
export const applicationsApi = {
  getAll: async (): Promise<ApplicationResponse[]> => {
    return request<ApplicationResponse[]>('/applications');
  },

  save: async (data: SaveApplicationRequest): Promise<ApplicationResponse> => {
    return request<ApplicationResponse>('/applications', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },
};

// API Keys API
export const apiKeysApi = {
  getByApplicationId: async (applicationId: number): Promise<ApiKeyResponse[]> => {
    return request<ApiKeyResponse[]>(`/apiKeys?applicationId=${applicationId}`);
  },

  generate: async (data: GenerateApiKeyRequest): Promise<ApiKeyResponse> => {
    return request<ApiKeyResponse>('/apiKeys', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  disable: async (data: DisableApiKeyRequest): Promise<void> => {
    await request<void>('/apiKeys/disable', {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },
};

// Keywords API
export const keywordsApi = {
  search: async (query: string): Promise<string[]> => {
    return request<string[]>(`/keywords?keySearch=${encodeURIComponent(query)}`);
  },
};

// Workers API
export const workersApi = {
  getStatus: async (params?: { state?: string; applicationId?: number; search?: string; limit?: number }): Promise<WorkerStatusListResponse> => {
    const searchParams = new URLSearchParams();
    if (params?.state) searchParams.set('state', params.state);
    if (params?.applicationId) searchParams.set('applicationId', String(params.applicationId));
    if (params?.search) searchParams.set('search', params.search);
    if (params?.limit) searchParams.set('limit', String(params.limit));
    const qs = searchParams.toString();
    return request<WorkerStatusListResponse>(`/workers${qs ? `?${qs}` : ''}`);
  },

  getEvents: async (params?: { workerId?: string; applicationId?: number; limit?: number }): Promise<WorkerEventResponse[]> => {
    const searchParams = new URLSearchParams();
    if (params?.workerId) searchParams.set('workerId', params.workerId);
    if (params?.applicationId) searchParams.set('applicationId', String(params.applicationId));
    if (params?.limit) searchParams.set('limit', String(params.limit));
    const qs = searchParams.toString();
    return request<WorkerEventResponse[]>(`/workers/events${qs ? `?${qs}` : ''}`);
  },
};

// Observability API
export const observabilityApi = {
  getConfig: async (): Promise<ObservabilityConfig> => {
    return request<ObservabilityConfig>('/observability/config');
  },

  saveConfig: async (data: SaveIntegrationConfigRequest): Promise<ObservabilityConfig> => {
    return request<ObservabilityConfig>('/observability/config', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  getStatus: async (): Promise<ObservabilityStatus> => {
    return request<ObservabilityStatus>('/observability/status');
  },

  testConnection: async (type: IntegrationType): Promise<TestConnectionResult> => {
    return request<TestConnectionResult>('/observability/test', {
      method: 'POST',
      body: JSON.stringify({ type }),
    });
  },

  getTraces: async (params?: { search?: string; status?: string; timeRange?: TimeRange }): Promise<TraceEntry[]> => {
    const searchParams = new URLSearchParams();
    if (params?.search) searchParams.set('search', params.search);
    if (params?.status) searchParams.set('status', params.status);
    if (params?.timeRange) searchParams.set('timeRange', params.timeRange);
    const qs = searchParams.toString();
    return request<TraceEntry[]>(`/observability/traces${qs ? `?${qs}` : ''}`);
  },

  getInsights: async (timeRange?: TimeRange): Promise<ObservabilityInsights> => {
    const qs = timeRange ? `?timeRange=${timeRange}` : '';
    return request<ObservabilityInsights>(`/observability/insights${qs}`);
  },
};

// Policies API
export const policiesApi = {
  getAll: async (params?: ListPoliciesParams): Promise<PolicyListResponse> => {
    const searchParams = new URLSearchParams();

    if (params?.search) searchParams.set('search', params.search);
    if (params?.type && params.type !== 'all') searchParams.set('type', params.type);
    if (params?.status && params.status !== 'all') searchParams.set('status', params.status);
    if (params?.env && params.env !== 'all') searchParams.set('env', params.env);
    if (params?.pipelineId && params.pipelineId !== 'all') searchParams.set('pipelineId', params.pipelineId);
    if (params?.range) searchParams.set('range', params.range);
    if (params?.sortBy) searchParams.set('sortBy', params.sortBy);
    if (params?.sortDir) searchParams.set('sortDir', params.sortDir);

    const queryString = searchParams.toString();
    return request<PolicyListResponse>(`/policies${queryString ? `?${queryString}` : ''}`);
  },

  getById: async (id: string, range?: PolicyRange): Promise<PolicyDetailResponse> => {
    const query = range ? `?range=${range}` : '';
    return request<PolicyDetailResponse>(`/policies/${id}${query}`);
  },

  create: async (data: CreatePolicyRequest): Promise<Policy> => {
    return request<Policy>('/policies', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  update: async (id: string, data: UpdatePolicyRequest): Promise<Policy> => {
    return request<Policy>(`/policies/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  duplicate: async (id: string): Promise<Policy> => {
    return request<Policy>(`/policies/${id}/duplicate`, {
      method: 'POST',
    });
  },

  enable: async (id: string): Promise<Policy> => {
    return request<Policy>(`/policies/${id}/enable`, {
      method: 'POST',
    });
  },

  disable: async (id: string): Promise<Policy> => {
    return request<Policy>(`/policies/${id}/disable`, {
      method: 'POST',
    });
  },

  pause: async (id: string): Promise<Policy> => {
    return request<Policy>(`/policies/${id}/pause`, {
      method: 'POST',
    });
  },

  resume: async (id: string): Promise<Policy> => {
    return request<Policy>(`/policies/${id}/resume`, {
      method: 'POST',
    });
  },

  delete: async (id: string): Promise<void> => {
    await request<void>(`/policies/${id}`, {
      method: 'DELETE',
    });
  },

  getAudit: async (id: string): Promise<PolicyAuditResponse> => {
    return request<PolicyAuditResponse>(`/policies/${id}/audit`);
  },

  getInsights: async (range: PolicyRange = '24h'): Promise<PolicyInsightsResponse> => {
    return request<PolicyInsightsResponse>(`/policies/insights?range=${range}`);
  },

  getTargetOptions: async (): Promise<PolicyTargetOptionsResponse> => {
    return request<PolicyTargetOptionsResponse>('/policies/targets');
  },

  previewTargets: async (payload: PolicyPreviewRequest): Promise<PolicyPreviewResponse> => {
    return request<PolicyPreviewResponse>('/policies/preview', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },
};

export { ApiError };
