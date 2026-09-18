import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { policiesApi } from '@/api/client';
import type {
  ListPoliciesParams,
  Policy,
  PolicyListResponse,
  PolicyRange,
  CreatePolicyRequest,
  UpdatePolicyRequest,
  PolicyPreviewRequest,
  PolicyEffectiveResolutionResponse,
} from '@/types/policies';

export type PolicyStatusAction = 'enable' | 'disable' | 'pause' | 'resume';

function snapshotPolicyLists(queryClient: QueryClient) {
  return queryClient.getQueryCache().findAll({ queryKey: ['policies'] }).map(query => ({
    query,
    data: query.state.data as PolicyListResponse | undefined,
  }));
}

type PolicyListSnapshots = ReturnType<typeof snapshotPolicyLists>;

function updatePolicyLists(
  queryClient: QueryClient,
  snapshots: PolicyListSnapshots | undefined,
  update: (old: PolicyListResponse | undefined) => PolicyListResponse | undefined,
) {
  snapshots?.forEach(({ query }) => {
    // Logout removes these query objects. A late mutation must not write data
    // into another account's cache, even when its query keys are identical.
    if (queryClient.getQueryCache().get(query.queryHash) === query) {
      queryClient.setQueryData<PolicyListResponse>(query.queryKey, update);
    }
  });
}

function restorePolicyLists(queryClient: QueryClient, snapshots: PolicyListSnapshots | undefined) {
  snapshots?.forEach(snapshot => updatePolicyLists(queryClient, [snapshot], () => snapshot.data));
}

export function usePolicies(params?: ListPoliciesParams) {
  return useQuery({
    queryKey: ['policies', params],
    queryFn: () => policiesApi.getAll(params),
  });
}

export function usePolicyInsights(range: PolicyRange) {
  return useQuery({
    queryKey: ['policy-insights', range],
    queryFn: () => policiesApi.getInsights(range),
  });
}

export function usePolicy(policyId: string | null, range: PolicyRange) {
  return useQuery({
    queryKey: ['policy', policyId, range],
    queryFn: () => policiesApi.getById(policyId || '', range),
    enabled: !!policyId,
  });
}

export function usePolicyAudit(policyId: string | null) {
  return useQuery({
    queryKey: ['policy-audit', policyId],
    queryFn: () => policiesApi.getAudit(policyId || ''),
    enabled: !!policyId,
  });
}

export function usePolicyTargetOptions() {
  return useQuery({
    queryKey: ['policy-target-options'],
    queryFn: () => policiesApi.getTargetOptions(),
  });
}

export function usePreviewPolicyTargets() {
  return useMutation({
    mutationFn: (payload: PolicyPreviewRequest) => policiesApi.previewTargets(payload),
  });
}

export function useEffectiveStagePolicies(stageId: number | null) {
  return useQuery<PolicyEffectiveResolutionResponse>({
    queryKey: ['policy-effective-stage', stageId],
    queryFn: () => policiesApi.getEffectiveStagePolicies(stageId || 0),
    enabled: !!stageId,
  });
}

export function useCreatePolicy() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: CreatePolicyRequest) => policiesApi.create(payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      queryClient.invalidateQueries({ queryKey: ['policy-insights'] });
    },
  });
}

export function useUpdatePolicy() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: UpdatePolicyRequest }) =>
      policiesApi.update(id, payload),
    onSuccess: (policy) => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      queryClient.invalidateQueries({ queryKey: ['policy-insights'] });
      queryClient.invalidateQueries({ queryKey: ['policy', policy.id] });
      queryClient.invalidateQueries({ queryKey: ['policy-audit', policy.id] });
    },
  });
}

export function useDuplicatePolicy() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => policiesApi.duplicate(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      queryClient.invalidateQueries({ queryKey: ['policy-insights'] });
    },
  });
}

export function usePromotePolicy() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => policiesApi.promote(id),
    onMutate: () => snapshotPolicyLists(queryClient),
    onSuccess: (policy: Policy, _id, snapshots) => {
      updatePolicyLists(queryClient, snapshots, old => {
        if (!old) return old;
        return { ...old, items: old.items.map(item => (item.id === policy.id ? { ...item, ...policy } : item)) };
      });
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      queryClient.invalidateQueries({ queryKey: ['policy-insights'] });
      queryClient.invalidateQueries({ queryKey: ['policy', policy.id] });
      queryClient.invalidateQueries({ queryKey: ['policy-audit', policy.id] });
    },
  });
}

export function useDeletePolicy() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => policiesApi.delete(id),
    onMutate: async (id: string) => {
      const previous = snapshotPolicyLists(queryClient);
      await queryClient.cancelQueries({ queryKey: ['policies'] });

      updatePolicyLists(queryClient, previous, old => {
        if (!old) return old;
        return {
          ...old,
          items: old.items.filter(item => item.id !== id),
          totalCount: Math.max(0, old.totalCount - 1),
        };
      });

      return { previous };
    },
    onError: (_error, _id, context) => {
      restorePolicyLists(queryClient, context?.previous);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      queryClient.invalidateQueries({ queryKey: ['policy-insights'] });
    },
  });
}

export function usePolicyStatusAction() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, action }: { id: string; action: PolicyStatusAction }) => {
      switch (action) {
        case 'enable':
          return policiesApi.enable(id);
        case 'disable':
          return policiesApi.disable(id);
        case 'pause':
          return policiesApi.pause(id);
        case 'resume':
          return policiesApi.resume(id);
        default:
          throw new Error(`Unsupported action: ${action as string}`);
      }
    },
    onMutate: async ({ id, action }) => {
      const previous = snapshotPolicyLists(queryClient);
      await queryClient.cancelQueries({ queryKey: ['policies'] });

      const nextStatus =
        action === 'enable' || action === 'resume'
          ? 'active'
          : action === 'pause'
            ? 'paused'
            : 'disabled';

      updatePolicyLists(queryClient, previous, old => {
        if (!old) return old;

        return {
          ...old,
          items: old.items.map(item =>
            item.id === id
              ? {
                  ...item,
                  status: nextStatus,
                  updatedAt: new Date().toISOString(),
                }
              : item,
          ),
        };
      });

      return { previous };
    },
    onError: (_error, _vars, context) => {
      restorePolicyLists(queryClient, context?.previous);
    },
    onSuccess: (policy: Policy, _vars, context) => {
      updatePolicyLists(queryClient, context?.previous, old => {
        if (!old) return old;

        return {
          ...old,
          items: old.items.map(item => (item.id === policy.id ? { ...item, ...policy } : item)),
        };
      });
      queryClient.invalidateQueries({ queryKey: ['policy', policy.id] });
      queryClient.invalidateQueries({ queryKey: ['policy-audit', policy.id] });
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      queryClient.invalidateQueries({ queryKey: ['policy-insights'] });
    },
  });
}
