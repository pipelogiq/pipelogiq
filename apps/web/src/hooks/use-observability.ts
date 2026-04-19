import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { observabilityApi } from '@/api/client';
import type {
  ObservabilityConfig,
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
  graylog:       { name: 'Logs',  description: 'Centralized log management and search',        icon: '📋' },
};

const SUPPORTED_INTEGRATION_TYPES = new Set(Object.keys(INTEGRATION_META));

// ── Hooks ──

export function useObservabilityConfig() {
  return useQuery({
    queryKey: ['observability', 'config'],
    queryFn: async (): Promise<ObservabilityConfig> => {
      const config = await observabilityApi.getConfig();
      // Backend DTO omits name/description/icon — merge static metadata in
      return {
        ...config,
        integrations: config.integrations
          .filter(i => SUPPORTED_INTEGRATION_TYPES.has(i.type))
          .map(i => ({
            ...INTEGRATION_META[i.type],
            ...i,
          })),
      };
    },
  });
}

export function useObservabilityStatus() {
  return useQuery({
    queryKey: ['observability', 'status'],
    queryFn: () => observabilityApi.getStatus(),
    refetchInterval: 30_000,
  });
}

export function useObservabilityTraces(params?: { search?: string; status?: string; timeRange?: TimeRange }) {
  return useQuery({
    queryKey: ['observability', 'traces', params],
    queryFn: () => observabilityApi.getTraces(params),
  });
}

export function useObservabilityInsights(timeRange?: TimeRange) {
  return useQuery({
    queryKey: ['observability', 'insights', timeRange],
    queryFn: () => observabilityApi.getInsights(timeRange),
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
