import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { schedulesApi } from '@/api/client';
import type { Schedule, ScheduleStatus } from '@/types/schedules';

export function useSchedules(status?: ScheduleStatus) {
  return useQuery({
    queryKey: ['schedules', status ?? 'all'],
    queryFn: () => schedulesApi.getAll(status),
    // The scheduler ticks every second; a schedule page that is open should not
    // be more than a few seconds stale even if the WebSocket drops.
    refetchInterval: 15000,
  });
}

export function useSchedule(id: number | undefined) {
  return useQuery({
    queryKey: ['schedule', id],
    queryFn: () => schedulesApi.getById(id as number),
    enabled: typeof id === 'number' && Number.isFinite(id),
  });
}

export function useScheduleRuns(id: number | undefined, limit = 50) {
  return useQuery({
    queryKey: ['schedule-runs', id, limit],
    queryFn: () => schedulesApi.getRuns(id as number, limit),
    enabled: typeof id === 'number' && Number.isFinite(id),
    refetchInterval: 15000,
  });
}

/** invalidateSchedule refreshes every view that can show this schedule. */
function invalidateSchedule(queryClient: ReturnType<typeof useQueryClient>, id: number) {
  void queryClient.invalidateQueries({ queryKey: ['schedules'] });
  void queryClient.invalidateQueries({ queryKey: ['schedule', id] });
  void queryClient.invalidateQueries({ queryKey: ['schedule-runs', id] });
}

export function useSetScheduleStatus() {
  const queryClient = useQueryClient();
  return useMutation<Schedule, Error, { id: number; action: 'pause' | 'resume' | 'archive' }>({
    mutationFn: ({ id, action }) => {
      if (action === 'pause') return schedulesApi.pause(id);
      if (action === 'resume') return schedulesApi.resume(id);
      return schedulesApi.archive(id);
    },
    onSuccess: (_data, variables) => invalidateSchedule(queryClient, variables.id),
  });
}

export function useTriggerSchedule() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input?: unknown }) =>
      schedulesApi.trigger(id, input),
    onSuccess: (_data, variables) => {
      invalidateSchedule(queryClient, variables.id);
      void queryClient.invalidateQueries({ queryKey: ['pipelines'] });
    },
  });
}
