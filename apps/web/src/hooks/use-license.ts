import { useQuery } from '@tanstack/react-query';
import { licenseApi } from '@/api/client';

/**
 * useLicense reads the resolved licence. The token is an environment variable,
 * so it cannot change without a restart — hence the long stale time.
 */
export function useLicense() {
  return useQuery({
    queryKey: ['license'],
    queryFn: licenseApi.get,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
}
