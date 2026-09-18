import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { auditApi } from '@/api/client';

describe('audit API', () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock
      .mockReset()
      .mockImplementation(() =>
        Promise.resolve(new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } })),
      );
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('requests paginated history without a latest-events limit', async () => {
    await auditApi.list();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/audit?page=1&pageSize=50');
  });

  it('encodes exact action, user and outcome filters and passes a cancellation signal', async () => {
    const controller = new AbortController();
    await auditApi.list(
      { action: 'stage.rerun/legacy value', userId: 37, outcome: 'partial', page: 7, pageSize: 100 },
      controller.signal,
    );
    const url = new URL(fetchMock.mock.calls[0][0], 'http://localhost');
    expect(Object.fromEntries(url.searchParams)).toEqual({
      action: 'stage.rerun/legacy value',
      userId: '37',
      outcome: 'partial',
      page: '7',
      pageSize: '100',
    });
    expect(fetchMock.mock.calls[0][1]).toEqual(
      expect.objectContaining({ credentials: 'include', signal: controller.signal }),
    );
  });

  it('keeps the unauthenticated actor filter distinct from all users', async () => {
    await auditApi.list({ userId: 0 });
    expect(fetchMock.mock.calls[0][0]).toContain('userId=0');
  });

  it('loads options independently of the selected audit page', async () => {
    await auditApi.options();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/audit/options');
  });
});
