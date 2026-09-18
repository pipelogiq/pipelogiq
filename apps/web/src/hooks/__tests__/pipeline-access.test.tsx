import type { ReactNode } from 'react';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resolveObservabilityConfig, usePipeline, usePipelines } from '@/hooks/use-pipelines';
import { usePipelineWebSocket } from '@/hooks/use-pipeline-ws';

const mocks = vi.hoisted(() => ({
  isAdmin: false, userId: 2,
  config: vi.fn(), pipelines: vi.fn(), pipeline: vi.fn(),
}));
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ isAdmin: mocks.isAdmin, user: { id: mocks.userId } }) }));
vi.mock('@/api/client', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/api/client')>(),
  observabilityApi: { getConfig: mocks.config },
  pipelinesApi: { getAll: mocks.pipelines, getById: mocks.pipeline },
}));

const pipeline = { id: 17, name: 'Invoice', status: 'Completed', createdAt: '2026-09-17T10:00:00Z', traceId: 'trace-17', stages: [] };
const config = { integrations: [{ type: 'opentelemetry', config: { traceLinkTemplate: 'https://admin.example/${traceId}' } }] };
let queryClient: QueryClient;

class TestSocket {
  static instances: TestSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  constructor() { TestSocket.instances.push(this); }
  sendPipeline() { this.onmessage?.({ data: JSON.stringify(pipeline) }); }
}

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.isAdmin = false;
  mocks.userId = 2;
  mocks.config.mockResolvedValue(config);
  mocks.pipelines.mockResolvedValue({ items: [pipeline], totalCount: 1 });
  mocks.pipeline.mockResolvedValue(pipeline);
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  TestSocket.instances = [];
  vi.stubGlobal('WebSocket', TestSocket);
});
afterEach(() => { cleanup(); queryClient.clear(); vi.unstubAllGlobals(); });

describe('admin observability access from pipeline views', () => {
  it('does not fetch or reuse admin configuration for viewer list and detail requests', async () => {
    queryClient.setQueryData(['observability', 'config'], config);
    const { result } = renderHook(() => ({ list: usePipelines(), detail: usePipeline(17) }), { wrapper });
    await waitFor(() => expect(result.current.list.isSuccess && result.current.detail.isSuccess).toBe(true));
    expect(mocks.config).not.toHaveBeenCalled();
    expect(result.current.list.data?.items[0].traceUrl).toBe('');
    expect(result.current.detail.data?.traceUrl).toBe('');
  });

  it('shares one cancellable configuration request for concurrent admin views', async () => {
    mocks.isAdmin = true;
    const { result } = renderHook(() => ({ list: usePipelines(), detail: usePipeline(17) }), { wrapper });
    await waitFor(() => expect(result.current.list.isSuccess && result.current.detail.isSuccess).toBe(true));
    expect(mocks.config).toHaveBeenCalledOnce();
    expect(mocks.config.mock.calls[0][0]).toBeInstanceOf(AbortSignal);
    expect(result.current.detail.data?.traceUrl).toBe('https://admin.example/trace-17');
  });

  it('does not restore old configuration if a request finishes after the session cache is cleared', async () => {
    let finish!: (value: typeof config) => void;
    mocks.config.mockImplementation(() => new Promise(resolve => { finish = resolve; }));
    const request = resolveObservabilityConfig(queryClient);
    const signal = mocks.config.mock.calls[0][0] as AbortSignal;
    queryClient.clear();
    expect(signal.aborted).toBe(true);
    finish(config);
    await expect(request).resolves.toBeUndefined();
    expect(queryClient.getQueryData(['observability', 'config'])).toBeUndefined();
  });

  it('does not preserve cached admin trace links when viewer websocket updates arrive', () => {
    queryClient.setQueryData(['observability', 'config'], config);
    queryClient.setQueryData(['pipeline', 17], { traceUrl: 'https://admin.example/trace-17', logTraceUrl: 'https://admin.example/logs' });
    queryClient.setQueryData(['pipelines'], { items: [{ id: '17', traceUrl: 'https://admin.example/trace-17', logTraceUrl: 'https://admin.example/logs' }] });
    renderHook(() => usePipelineWebSocket(), { wrapper });
    act(() => TestSocket.instances[0].sendPipeline());
    expect(mocks.config).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(['pipeline', 17])).toEqual(expect.objectContaining({ traceUrl: '', logTraceUrl: '' }));
    expect(queryClient.getQueryData(['pipelines'])).toEqual(expect.objectContaining({ items: [expect.objectContaining({ traceUrl: '', logTraceUrl: '' })] }));
  });

  it('reconnects when account identity changes and ignores queued messages from the previous session', () => {
    const view = renderHook(() => usePipelineWebSocket(), { wrapper });
    const previousSocket = TestSocket.instances[0];
    mocks.userId = 3;
    queryClient.clear();
    view.rerender();
    expect(previousSocket.close).toHaveBeenCalledOnce();
    expect(TestSocket.instances).toHaveLength(2);
    act(() => previousSocket.sendPipeline());
    expect(queryClient.getQueryData(['pipeline', 17])).toBeUndefined();
    act(() => TestSocket.instances[1].sendPipeline());
    expect(queryClient.getQueryData(['pipeline', 17])).toEqual(expect.objectContaining({ id: '17' }));
  });
});
