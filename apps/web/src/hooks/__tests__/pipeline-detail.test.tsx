import type { ReactNode } from 'react';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import { usePipelineDetail } from '@/hooks/use-pipeline-detail';
import { usePipelineWebSocket } from '@/hooks/use-pipeline-ws';
import type { PipelineResponse } from '@/types/api';

const mocks = vi.hoisted(() => ({ pipeline: vi.fn(), config: vi.fn() }));
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAdmin: false, user: { id: 7 } }),
}));
vi.mock('@/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/client')>();
  return {
    ...actual,
    pipelinesApi: { ...actual.pipelinesApi, getById: mocks.pipeline },
    observabilityApi: { ...actual.observabilityApi, getConfig: mocks.config },
  };
});

const pipeline: PipelineResponse = {
  id: 17, name: 'Actual pipeline', status: 'Completed', isTerminal: true,
  createdAt: '2026-09-17T10:00:00Z', applicationId: 4, idempotencyKey: 'job-17',
  stages: [{
    id: 51, pipelineId: 17, name: 'Payload stage', status: 'Completed',
    createdAt: '2026-09-17T10:00:00Z', input: 'null', output: 'false',
    attempt: 3, retryAttempt: 2, lastErrorCode: 'timeout', failureDisposition: 'retryable',
    failureCount: 2, hasFailureHistory: true,
    logs: [{ id: 9, stageId: 51, created: '2026-09-17T10:01:00Z', message: 'Completed' }],
  }],
  pipelineContextItems: [{ key: 'token', value: '[REDACTED]', isSensitive: true }],
  pipelineKeywords: [{ key: 'region', value: 'EU' }],
};

class TestSocket {
  static instances: TestSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  constructor() { TestSocket.instances.push(this); }
}

let queryClient: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  vi.resetAllMocks();
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  mocks.pipeline.mockResolvedValue(pipeline);
  TestSocket.instances = [];
  vi.stubGlobal('WebSocket', TestSocket);
});

afterEach(() => {
  cleanup();
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('raw pipeline detail', () => {
  it('preserves actual payloads, logs, context and execution metadata without inferred values', async () => {
    const { result } = renderHook(() => usePipelineDetail(17), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual(pipeline);
    expect(result.current.data?.stages?.[0]).toMatchObject({ input: 'null', output: 'false', attempt: 3, retryAttempt: 2 });
    expect(result.current.data).not.toHaveProperty('environment');
    expect(result.current.data).not.toHaveProperty('correlationId');
    expect(mocks.config).not.toHaveBeenCalled();
    expect(mocks.pipeline).toHaveBeenCalledWith(17);
  });

  it.each([0, -1, 1.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])('does not fetch an invalid pipeline ID (%s)', id => {
    const { result } = renderHook(() => usePipelineDetail(id), { wrapper });
    expect(result.current.fetchStatus).toBe('idle');
    expect(mocks.pipeline).not.toHaveBeenCalled();
  });

  it('refetches on websocket invalidation without replacing full detail with the mapped snapshot', async () => {
    const { result } = renderHook(() => {
      usePipelineWebSocket();
      return usePipelineDetail(17);
    }, { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    let resolveRefresh!: (value: PipelineResponse) => void;
    mocks.pipeline.mockImplementationOnce(() => new Promise(resolve => { resolveRefresh = resolve; }));
    const snapshot: PipelineResponse = {
      ...pipeline,
      stages: pipeline.stages?.map(stage => ({ ...stage, logs: undefined, output: undefined })),
      pipelineKeywords: undefined,
    };
    act(() => TestSocket.instances[0].onmessage?.({ data: JSON.stringify(snapshot) }));
    await waitFor(() => expect(mocks.pipeline).toHaveBeenCalledTimes(2));
    expect(result.current.data).toEqual(pipeline);
    expect(queryClient.getQueryData(['pipeline', 17])).toMatchObject({ id: '17', pipelineName: pipeline.name });

    const refreshed = { ...pipeline, name: 'Updated actual pipeline' };
    await act(async () => { resolveRefresh(refreshed); });
    await waitFor(() => expect(result.current.data).toEqual(refreshed));
    expect(result.current.data?.stages?.[0].logs).toHaveLength(1);
  });

  it('does not restore detail after the account query cache has been cleared', async () => {
    let resolveRequest!: (value: PipelineResponse) => void;
    mocks.pipeline.mockImplementationOnce(() => new Promise(resolve => { resolveRequest = resolve; }));
    renderHook(() => usePipelineDetail(17), { wrapper });
    await waitFor(() => expect(mocks.pipeline).toHaveBeenCalledOnce());
    queryClient.clear();
    await act(async () => { resolveRequest(pipeline); });
    expect(queryClient.getQueryData(['pipeline', 17, 'detail'])).toBeUndefined();
  });

  it.each(['NotStarted', 'Pending', 'Running', 'Paused'] as const)('polls %s pipelines while websocket updates are unavailable', async status => {
    vi.useFakeTimers();
    mocks.pipeline.mockResolvedValue({ ...pipeline, status, isTerminal: false });
    renderHook(() => usePipelineDetail(17), { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(1); });
    expect(mocks.pipeline).toHaveBeenCalledOnce();
    await act(async () => { await vi.advanceTimersByTimeAsync(3000); });
    expect(mocks.pipeline).toHaveBeenCalledTimes(2);
  });

  it.each(['Completed', 'Failed', 'Cancelled'] as const)('stops polling terminal %s pipelines', async status => {
    vi.useFakeTimers();
    mocks.pipeline.mockResolvedValue({ ...pipeline, status, isTerminal: undefined });
    renderHook(() => usePipelineDetail(17), { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(9000); });
    expect(mocks.pipeline).toHaveBeenCalledOnce();
  });

  it('continues polling when a stage is still retrying despite a terminal pipeline snapshot', async () => {
    vi.useFakeTimers();
    mocks.pipeline.mockResolvedValue({ ...pipeline, stages: [{ ...pipeline.stages![0], status: 'RetryScheduled' }] });
    renderHook(() => usePipelineDetail(17), { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(3001); });
    expect(mocks.pipeline).toHaveBeenCalledTimes(2);
  });

  it.each([401, 403, 404])('does not poll or retry an HTTP %s response', async status => {
    vi.useFakeTimers();
    mocks.pipeline.mockRejectedValue(new ApiError(status, 'Unavailable'));
    renderHook(() => usePipelineDetail(17), { wrapper });
    await act(async () => { await vi.advanceTimersByTimeAsync(9000); });
    expect(mocks.pipeline).toHaveBeenCalledOnce();
  });
});
