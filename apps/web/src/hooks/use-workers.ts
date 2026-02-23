import { useQuery } from '@tanstack/react-query';
import { workersApi } from '@/api/client';

export function useWorkerStatus(params?: { state?: string; applicationId?: number; search?: string; limit?: number }) {
  return useQuery({
    queryKey: ['workers', 'status', params],
    queryFn: () => workersApi.getStatus(params),
    refetchInterval: 5000,
  });
}

export function useWorkerEvents(params?: { workerId?: string; applicationId?: number; limit?: number }) {
  return useQuery({
    queryKey: ['workers', 'events', params],
    queryFn: () => workersApi.getEvents(params),
    refetchInterval: 5000,
  });
}
