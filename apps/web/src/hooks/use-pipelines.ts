import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type { QueryClient } from '@tanstack/react-query';
import { pipelinesApi, observabilityApi } from '@/api/client';
import type { GetPipelinesParams, PipelineResponse, StageResponse } from '@/types/api';
import type { ObservabilityConfig, OtelConfig, LogsConfig } from '@/types/observability';
import type { PipelineExecution, PipelineAction } from '@/components/pipelines/PipelineDetailPanel';
import { mapPipelineStatusToUI, mapStageStatusToUI } from '@/types/api';
import { formatDistanceToNow, format } from 'date-fns';
import { buildTraceLink, buildLogsLink } from './use-observability';

function formatDuration(startDate?: string, endDate?: string): string {
  if (!startDate) return '';

  const start = new Date(startDate);
  const end = endDate ? new Date(endDate) : new Date();
  const diff = end.getTime() - start.getTime();

  if (diff < 1000) return `${diff}ms`;
  if (diff < 60000) return `${(diff / 1000).toFixed(1)}s`;
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ${Math.floor((diff % 60000) / 1000)}s`;
  return `${Math.floor(diff / 3600000)}h ${Math.floor((diff % 3600000) / 60000)}m`;
}

function mapStageToAction(stage: StageResponse): PipelineAction {
  // Combine all logs into a single string
  const logsText = stage.logs
    ?.map(log => `[${log.created}] ${log.logLevel?.toUpperCase() || 'INFO'}  ${log.message}`)
    .join('\n') || '';

  // Parse input/output from JSON strings
  let input: Record<string, unknown> = {};
  let output: Record<string, unknown> = {};

  try {
    if (stage.input) input = JSON.parse(stage.input);
  } catch {
    input = { raw: stage.input };
  }

  try {
    if (stage.output) output = JSON.parse(stage.output);
  } catch {
    output = { raw: stage.output };
  }

  return {
    id: String(stage.id),
    name: stage.name,
    spanId: stage.spanId || undefined,
    status: mapStageStatusToUI(stage.status),
    duration: formatDuration(stage.startedAt, stage.finishedAt),
    startedAt: stage.startedAt ? format(new Date(stage.startedAt), 'HH:mm:ss.SSS') : undefined,
    completedAt: stage.finishedAt ? format(new Date(stage.finishedAt), 'HH:mm:ss.SSS') : undefined,
    logs: logsText,
    input,
    output,
    error: stage.status === 'Failed' ? (stage.logs?.[stage.logs.length - 1]?.message || 'Stage failed') : undefined,
  };
}

export function getTraceLinkTemplateFromConfig(config?: ObservabilityConfig): string | undefined {
  const otel = config?.integrations.find((integration) => integration.type === 'opentelemetry');
  const otelConfig = (otel?.config || {}) as Partial<OtelConfig>;
  const template = otelConfig.traceLinkTemplate;
  if (typeof template !== 'string') return undefined;
  const trimmed = template.trim();
  return trimmed || undefined;
}

export function getLogsLinkTemplateFromConfig(config?: ObservabilityConfig): string | undefined {
  const logs = config?.integrations.find((integration) => {
    if (integration.type === 'graylog') return true;
    const candidate = integration.config as Partial<LogsConfig> | undefined;
    return typeof candidate?.searchUrlTemplate === 'string';
  });
  const logsConfig = (logs?.config || {}) as Partial<LogsConfig>;
  const template = logsConfig.searchUrlTemplate;
  if (typeof template !== 'string') return undefined;
  const trimmed = template.trim();
  return trimmed || undefined;
}

export async function resolveObservabilityConfig(queryClient: QueryClient): Promise<ObservabilityConfig | undefined> {
  const key = ['observability', 'config'] as const;
  const cached = queryClient.getQueryData<ObservabilityConfig>(key);
  if (cached) return cached;

  try {
    const config = await observabilityApi.getConfig();
    queryClient.setQueryData(key, config);
    return config;
  } catch {
    return undefined;
  }
}

export async function resolveTraceLinkTemplate(queryClient: QueryClient): Promise<string | undefined> {
  const config = await resolveObservabilityConfig(queryClient);
  return getTraceLinkTemplateFromConfig(config);
}

export async function resolveLogsLinkTemplate(queryClient: QueryClient): Promise<string | undefined> {
  const config = await resolveObservabilityConfig(queryClient);
  return getLogsLinkTemplateFromConfig(config);
}

type PipelineLinkTemplates = {
  traceLinkTemplate?: string;
  logsLinkTemplate?: string;
};

export function mapPipelineToExecution(
  pipeline: PipelineResponse,
  linkTemplates?: PipelineLinkTemplates,
): PipelineExecution {
  const stages = pipeline.stages || [];

  // Extract keywords for tags
  const tags = pipeline.pipelineKeywords?.map(kw => kw.value) || [];

  // Find owner from context or use default
  const ownerContext = pipeline.pipelineContextItems?.find(ctx => ctx.key.toLowerCase() === 'owner');
  const owner = ownerContext?.value || 'system';

  // Find environment from context or use default
  const envContext = pipeline.pipelineContextItems?.find(ctx =>
    ctx.key.toLowerCase() === 'environment' || ctx.key.toLowerCase() === 'env'
  );
  const environment = envContext?.value || 'production';

  // Find correlation ID from context
  const corrContext = pipeline.pipelineContextItems?.find(ctx =>
    ctx.key.toLowerCase() === 'correlationid' || ctx.key.toLowerCase() === 'correlation_id'
  );
  const correlationId = corrContext?.value || `corr-${pipeline.id}`;

  const traceId = pipeline.traceId || '';
  const traceUrl = traceId ? (buildTraceLink(linkTemplates?.traceLinkTemplate, traceId) || '') : '';
  const logTraceUrl = buildLogsLink(linkTemplates?.logsLinkTemplate, {
    traceId: traceId || undefined,
    executionId: String(pipeline.id),
  }) || '';

  return {
    id: String(pipeline.id),
    pipelineId: String(pipeline.id),
    pipelineName: pipeline.name,
    description: stages[0]?.description || `Pipeline #${pipeline.id}`,
    status: mapPipelineStatusToUI(pipeline.status),
    environment,
    startedAt: formatDistanceToNow(new Date(pipeline.createdAt), { addSuffix: true }),
    completedAt: pipeline.finishedAt
      ? formatDistanceToNow(new Date(pipeline.finishedAt), { addSuffix: true })
      : undefined,
    duration: formatDuration(pipeline.createdAt, pipeline.finishedAt || undefined),
    owner,
    tags,
    correlationId,
    executionNumber: pipeline.id,
    context: pipeline.pipelineContextItems?.map(ctx => ({
      key: ctx.key,
      value: ctx.value,
    })) || [],
    actions: stages.map(mapStageToAction),
    stages: stages.map(s => ({
      name: s.name,
      status: mapStageStatusToUI(s.status),
      startedAt: s.startedAt || undefined,
      finishedAt: s.finishedAt || undefined,
    })),
    traceId,
    traceUrl,
    logTraceUrl,

  };
}

export function usePipelines(params?: GetPipelinesParams) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: ['pipelines', params],
    queryFn: async () => {
      const config = await resolveObservabilityConfig(queryClient);
      const linkTemplates: PipelineLinkTemplates = {
        traceLinkTemplate: getTraceLinkTemplateFromConfig(config),
        logsLinkTemplate: getLogsLinkTemplateFromConfig(config),
      };
      const result = await pipelinesApi.getAll(params);
      return {
        ...result,
        items: result.items.map((pipeline) => mapPipelineToExecution(pipeline, linkTemplates)),
      };
    },
    refetchInterval: (query) => {
      const data = query.state.data as
        | { items: PipelineExecution[] }
        | undefined;
      if (!data?.items?.length) return false;
      const hasActive = data.items.some((pipeline) =>
        pipeline.status === 'running' ||
        pipeline.status === 'waiting' ||
        pipeline.status === 'throttled'
      );
      return hasActive ? 5000 : false;
    },
  });
}

export function usePipeline(id: number) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: ['pipeline', id],
    queryFn: async () => {
      const config = await resolveObservabilityConfig(queryClient);
      const linkTemplates: PipelineLinkTemplates = {
        traceLinkTemplate: getTraceLinkTemplateFromConfig(config),
        logsLinkTemplate: getLogsLinkTemplateFromConfig(config),
      };
      const pipeline = await pipelinesApi.getById(id);
      return mapPipelineToExecution(pipeline, linkTemplates);
    },
    enabled: id > 0,
    refetchInterval: (query) => {
      const data = query.state.data as PipelineExecution | undefined;
      if (!data) return 3000;
      return (
        data.status === 'running' ||
        data.status === 'waiting' ||
        data.status === 'throttled'
      ) ? 3000 : false;
    },
  });
}

export function usePipelineStages(pipelineId: number) {
  return useQuery({
    queryKey: ['pipeline-stages', pipelineId],
    queryFn: () => pipelinesApi.getStages(pipelineId),
    enabled: pipelineId > 0,
  });
}

export function usePipelineContext(pipelineId: number) {
  return useQuery({
    queryKey: ['pipeline-context', pipelineId],
    queryFn: () => pipelinesApi.getContext(pipelineId),
    enabled: pipelineId > 0,
  });
}

export function useRerunStage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (stageId: number) =>
      pipelinesApi.rerunStage({ stageId, rerunAllNextStages: false }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pipelines'] });
      queryClient.invalidateQueries({ queryKey: ['pipeline'] });
      queryClient.invalidateQueries({ queryKey: ['pipeline-stages'] });
    },
  });
}

export function useSkipStage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (stageId: number) =>
      pipelinesApi.skipStage({ stageId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pipelines'] });
      queryClient.invalidateQueries({ queryKey: ['pipeline'] });
      queryClient.invalidateQueries({ queryKey: ['pipeline-stages'] });
    },
  });
}
