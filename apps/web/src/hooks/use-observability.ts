import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { observabilityApi } from '@/api/client';
import type {
  ObservabilityConfig,
  ObservabilityStatus,
  ObservabilityInsights,
  TraceEntry,
  TestConnectionResult,
  SaveIntegrationConfigRequest,
  TimeRange,
  IntegrationType,
  Integration,
} from '@/types/observability';

// ── Static display metadata (name, icon, description) per integration type ──
// The backend DTO does not return these presentation fields, so we define them here
// and merge them with the dynamic data from the API.

const INTEGRATION_META: Record<string, Pick<Integration, 'name' | 'description' | 'icon'>> = {
  opentelemetry: { name: 'OpenTelemetry',  description: 'Export traces and spans via OTLP protocol',    icon: '🔭' },
  alerting:      { name: 'Alerts',          description: 'Route notifications to chat, webhook, and on-call tools', icon: '🚨' },
  grafana:       { name: 'Grafana',         description: 'Visualize metrics and dashboards',              icon: '📈' },
  sentry:        { name: 'Sentry',          description: 'Error tracking and performance monitoring',     icon: '🐛' },
  datadog:       { name: 'Datadog',         description: 'APM and infrastructure monitoring',             icon: '🐕' },
  graylog:       { name: 'Logs',  description: 'Centralized log management and search',        icon: '📋' },
};

// ── Mock data (used when backend endpoints are not yet available) ──

const MOCK_INTEGRATIONS: Integration[] = [
  { type: 'opentelemetry', ...INTEGRATION_META['opentelemetry'], status: 'not_configured' },
  { type: 'alerting',      ...INTEGRATION_META['alerting'],      status: 'not_configured' },
  { type: 'grafana',       ...INTEGRATION_META['grafana'],       status: 'not_configured' },
  { type: 'sentry',        ...INTEGRATION_META['sentry'],        status: 'not_configured' },
  { type: 'datadog',       ...INTEGRATION_META['datadog'],       status: 'not_configured' },
  { type: 'graylog',       ...INTEGRATION_META['graylog'],       status: 'not_configured' },
];

const MOCK_STATUS: ObservabilityStatus = {
  otel: {
    configured: false,
    connected: false,
    exportRatePerMin: 0,
    dropRate: 0,
  },
  prometheus: {
    configured: false,
    connected: false,
  },
  logs: {
    configured: false,
    linkTemplateConfigured: false,
  },
  alerting: {
    configured: false,
    channels: [],
    events: [],
  },
};

const MOCK_TRACES: TraceEntry[] = [
  { traceId: 'a1b2c3d4e5f6a7b8c9d0e1f2', executionId: 1, pipelineName: 'send referral reply', status: 'success', durationMs: 1240, spansCount: 7, timestamp: new Date(Date.now() - 2 * 60000).toISOString() },
  { traceId: 'b2c3d4e5f6a7b8c9d0e1f2a3', executionId: 2, pipelineName: 'user-onboarding', status: 'success', durationMs: 4560, spansCount: 5, timestamp: new Date(Date.now() - 5 * 60000).toISOString() },
  { traceId: 'c3d4e5f6a7b8c9d0e1f2a3b4', pipelineName: 'kyc-verification', status: 'running', durationMs: 890, spansCount: 3, timestamp: new Date(Date.now() - 8 * 60000).toISOString() },
  { traceId: 'd4e5f6a7b8c9d0e1f2a3b4c5', executionId: 4, pipelineName: 'betting-settlement', status: 'error', durationMs: 12340, spansCount: 4, timestamp: new Date(Date.now() - 15 * 60000).toISOString() },
  { traceId: 'e5f6a7b8c9d0e1f2a3b4c5d6', executionId: 5, pipelineName: 'payment-processing', status: 'success', durationMs: 2100, spansCount: 6, timestamp: new Date(Date.now() - 22 * 60000).toISOString() },
  { traceId: 'f6a7b8c9d0e1f2a3b4c5d6e7', executionId: 6, pipelineName: 'notification-dispatch', status: 'success', durationMs: 430, spansCount: 2, timestamp: new Date(Date.now() - 30 * 60000).toISOString() },
  { traceId: 'a7b8c9d0e1f2a3b4c5d6e7f8', pipelineName: 'data-sync', status: 'error', durationMs: 8900, spansCount: 8, timestamp: new Date(Date.now() - 45 * 60000).toISOString() },
  { traceId: 'b8c9d0e1f2a3b4c5d6e7f8a9', executionId: 8, pipelineName: 'report-generation', status: 'success', durationMs: 15600, spansCount: 12, timestamp: new Date(Date.now() - 55 * 60000).toISOString() },
];

const MOCK_INSIGHTS: ObservabilityInsights = {
  slowestStages: [
    { pipelineName: 'betting-settlement', stageName: 'calculate-odds', p95Ms: 8450 },
    { pipelineName: 'report-generation', stageName: 'aggregate-data', p95Ms: 6200 },
    { pipelineName: 'kyc-verification', stageName: 'document-ocr', p95Ms: 4800 },
    { pipelineName: 'payment-processing', stageName: 'fraud-check', p95Ms: 3100 },
    { pipelineName: 'data-sync', stageName: 'transform-records', p95Ms: 2700 },
  ],
  errorHotspots: [
    { pipelineName: 'kyc-verification', stageName: 'id-validation', failureRate: 12.5, avgRetries: 2.1 },
    { pipelineName: 'data-sync', stageName: 'api-fetch', failureRate: 8.3, avgRetries: 3.0 },
    { pipelineName: 'betting-settlement', stageName: 'payout-transfer', failureRate: 4.7, avgRetries: 1.5 },
    { pipelineName: 'notification-dispatch', stageName: 'sms-send', failureRate: 3.2, avgRetries: 1.2 },
  ],
  summary: {
    executionsPerMin: 24.5,
    failuresPerMin: 1.8,
    successRate: 92.7,
    avgStageMs: 1340,
  },
};

// In-memory config store for mock mode (persists across re-renders within session)
let mockConfigStore: ObservabilityConfig = {
  integrations: [...MOCK_INTEGRATIONS],
};

// ── Helpers ──

async function withMockFallback<T>(apiFn: () => Promise<T>, mockData: T): Promise<T> {
  try {
    return await apiFn();
  } catch {
    // TODO: remove mock fallback once backend endpoints are deployed
    return mockData;
  }
}

// ── Hooks ──

export function useObservabilityConfig() {
  return useQuery({
    queryKey: ['observability', 'config'],
    queryFn: () => withMockFallback(
      async () => {
        const config = await observabilityApi.getConfig();
        // Backend DTO omits name/description/icon — merge static metadata in
        return {
          ...config,
          integrations: config.integrations.map(i => ({
            ...INTEGRATION_META[i.type],
            ...i,
          })),
        };
      },
      mockConfigStore,
    ),
  });
}

export function useObservabilityStatus() {
  return useQuery({
    queryKey: ['observability', 'status'],
    queryFn: () => withMockFallback(
      () => observabilityApi.getStatus(),
      MOCK_STATUS,
    ),
    refetchInterval: 30_000,
  });
}

export function useObservabilityTraces(params?: { search?: string; status?: string; timeRange?: TimeRange }) {
  return useQuery({
    queryKey: ['observability', 'traces', params],
    queryFn: () => withMockFallback(
      () => observabilityApi.getTraces(params),
      filterMockTraces(params),
    ),
  });
}

export function useObservabilityInsights(timeRange?: TimeRange) {
  return useQuery({
    queryKey: ['observability', 'insights', timeRange],
    queryFn: () => withMockFallback(
      () => observabilityApi.getInsights(timeRange),
      MOCK_INSIGHTS,
    ),
  });
}

export function useSaveIntegrationConfig() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: SaveIntegrationConfigRequest) => observabilityApi.saveConfig(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['observability', 'config'] });
      queryClient.invalidateQueries({ queryKey: ['observability', 'status'] });
    },
  });
}

export function useTestConnection() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (type: IntegrationType): Promise<TestConnectionResult> =>
      observabilityApi.testConnection(type),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['observability', 'config'] });
      queryClient.invalidateQueries({ queryKey: ['observability', 'status'] });
    },
  });
}

// ── Mock helpers ──

function filterMockTraces(params?: { search?: string; status?: string; timeRange?: TimeRange }): TraceEntry[] {
  let traces = [...MOCK_TRACES];

  if (params?.search) {
    const q = params.search.toLowerCase();
    traces = traces.filter(t =>
      t.traceId.toLowerCase().includes(q) ||
      t.pipelineName.toLowerCase().includes(q) ||
      (t.executionId && String(t.executionId).includes(q))
    );
  }

  if (params?.status && params.status !== 'all') {
    traces = traces.filter(t => t.status === params.status);
  }

  return traces;
}

// Build trace link from template
export function buildTraceLink(template: string | undefined, traceId: string): string | null {
  if (!template) return null;
  return template.replace(/\$\{traceId\}/g, traceId);
}

// Build logs link from template
export function buildLogsLink(
  template: string | undefined,
  params: { traceId?: string; executionId?: string; stageId?: string },
): string | null {
  if (!template) return null;
  let url = template;
  url = url.replace(/\$\{traceId\}/g, params.traceId || '');
  url = url.replace(/\$\{executionId\}/g, params.executionId || '');
  url = url.replace(/\$\{stageId\}/g, params.stageId || '');
  return url;
}
