import { useQuery } from '@tanstack/react-query';
import { ApiError, pipelinesApi } from '@/api/client';
import type { PipelineStatus, StageStatus } from '@/types/api';

const terminalStatuses = new Set<PipelineStatus>(['Completed', 'Failed', 'Cancelled']);
const activeStageStatuses = new Set<StageStatus>([
  'Running', 'Pending', 'RetryScheduled', 'Throttled', 'WaitingForApproval',
]);

// This child key receives the existing websocket/mutation invalidations while
// remaining separate from the mapped sidebar data written to ['pipeline', id].
export function usePipelineDetail(id: number) {
  return useQuery({
    queryKey: ['pipeline', id, 'detail'],
    queryFn: () => pipelinesApi.getById(id),
    enabled: Number.isSafeInteger(id) && id > 0,
    retry: (failureCount, error) => failureCount < 1 &&
      !(error instanceof ApiError && error.status >= 400 && error.status < 500),
    refetchOnWindowFocus: true,
    refetchInterval: (query) => {
      const pipeline = query.state.data;
      if (!pipeline || query.state.status === 'error') return false;
      const isTerminal = pipeline.isTerminal ?? terminalStatuses.has(pipeline.status);
      const hasActiveStage = pipeline.stages?.some(stage =>
        stage.status !== undefined && activeStageStatuses.has(stage.status));
      return !isTerminal || hasActiveStage ? 3000 : false;
    },
  });
}
