import { useQuery } from '@tanstack/react-query';
import { auditApi } from '@/api/client';
import type { AuditListParams } from '@/types/api';

export function useAuditLog(params: AuditListParams = {}) {
  return useQuery({
    queryKey: ['audit', 'list', params],
    queryFn: ({ signal }) => auditApi.list(params, signal),
    refetchInterval: 30_000,
  });
}

export function useAuditOptions() {
  return useQuery({
    queryKey: ['audit', 'options'],
    queryFn: ({ signal }) => auditApi.options(signal),
    refetchInterval: 30_000,
  });
}
