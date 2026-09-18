import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { policiesApi } from '@/api/client';
import { useDeletePolicy, usePolicyStatusAction, usePromotePolicy } from '@/hooks/use-policies';
import type { Policy, PolicyListResponse } from '@/types/policies';

vi.mock('@/api/client', () => ({
  policiesApi: { delete: vi.fn(), pause: vi.fn(), promote: vi.fn() },
}));

const policy: Policy = {
  id: 'policy-1', name: 'Original account policy', type: 'timeout', rule: { timeoutMs: 1000, appliesTo: 'step' },
  source: 'system', status: 'active', environment: 'all',
  targeting: { pipelines: [], stages: [], handlers: [], tagsInclude: [], tagsExclude: [] },
  createdAt: '2026-01-01', updatedAt: '2026-01-01', createdBy: 'admin', updatedBy: 'admin', version: 1,
};
const list: PolicyListResponse = { items: [{ ...policy, triggerCountInRange: 0 }], totalCount: 1 };
const otherList: PolicyListResponse = { items: [{ ...policy, name: 'Another account policy', triggerCountInRange: 0 }], totalCount: 1 };
const key = ['policies', {}];

function setup() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  queryClient.setQueryData(key, list);
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  const hook = renderHook(() => ({ remove: useDeletePolicy(), status: usePolicyStatusAction(), promote: usePromotePolicy() }), { wrapper });
  return { ...hook, queryClient };
}

beforeEach(() => vi.resetAllMocks());
afterEach(cleanup);

describe('policy mutations across account changes', () => {
  it.each(['delete', 'status'] as const)('does not restore %s rollback data after logout or replace another account’s cache', async action => {
    let rejectRequest!: (reason: Error) => void;
    const request = new Promise<never>((_resolve, reject) => { rejectRequest = reject; });
    const apiMock = action === 'delete' ? vi.mocked(policiesApi.delete) : vi.mocked(policiesApi.pause);
    apiMock.mockReturnValue(request);
    const { result, queryClient } = setup();
    let pending!: Promise<unknown>;
    act(() => {
      pending = (action === 'delete' ? result.current.remove.mutateAsync(policy.id) : result.current.status.mutateAsync({ id: policy.id, action: 'pause' })).catch(error => error);
    });
    await waitFor(() => expect(apiMock).toHaveBeenCalledOnce());
    queryClient.clear();
    if (action === 'status') queryClient.setQueryData(key, otherList);
    await act(async () => { rejectRequest(new Error('Session expired')); await pending; });
    expect(queryClient.getQueryData(key)).toEqual(action === 'status' ? otherList : undefined);
  });

  it.each(['promote', 'status'] as const)('does not merge an old %s response into a new account’s cache', async action => {
    let resolveRequest!: (result: Policy) => void;
    const request = new Promise<Policy>(resolve => { resolveRequest = resolve; });
    const apiMock = action === 'promote' ? vi.mocked(policiesApi.promote) : vi.mocked(policiesApi.pause);
    apiMock.mockReturnValue(request);
    const { result, queryClient } = setup();
    let pending!: Promise<unknown>;
    act(() => {
      pending = action === 'promote' ? result.current.promote.mutateAsync(policy.id) : result.current.status.mutateAsync({ id: policy.id, action: 'pause' });
    });
    await waitFor(() => expect(apiMock).toHaveBeenCalledOnce());
    queryClient.clear();
    queryClient.setQueryData(key, otherList);
    await act(async () => { resolveRequest({ ...policy, status: 'paused' }); await pending; });
    expect(queryClient.getQueryData(key)).toEqual(otherList);
  });

  it('still restores an optimistic change when the same account’s request fails', async () => {
    vi.mocked(policiesApi.delete).mockRejectedValue(new Error('Request failed'));
    const { result, queryClient } = setup();
    await act(async () => { await expect(result.current.remove.mutateAsync(policy.id)).rejects.toThrow('Request failed'); });
    expect(queryClient.getQueryData(key)).toEqual(list);
  });
});
