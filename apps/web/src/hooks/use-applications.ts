import { useQuery } from '@tanstack/react-query';
import { applicationsApi, apiKeysApi } from '@/api/client';

export function useApplications() {
  return useQuery({
    queryKey: ['applications'],
    queryFn: applicationsApi.getAll,
  });
}

export function useApiKeys(applicationId: number) {
  return useQuery({
    queryKey: ['apiKeys', applicationId],
    queryFn: () => apiKeysApi.getByApplicationId(applicationId),
    enabled: applicationId > 0,
  });
}
