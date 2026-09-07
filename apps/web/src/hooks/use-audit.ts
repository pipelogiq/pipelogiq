import { useQuery } from '@tanstack/react-query';
import { auditApi } from '@/api/client';

export function useAuditLog(limit = 200) {
  return useQuery({
    queryKey: ['audit', limit],
    queryFn: () => auditApi.list(limit),
    refetchInterval: 30_000,
  });
}
