import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { systemApi, isForbidden, ApiError, SESSION_EXPIRED_EVENT } from '@/api/client';

function respond(status: number, body: string, contentType = 'text/plain') {
  return Promise.resolve(
    new Response(body, { status, headers: { 'content-type': contentType } }),
  );
}

describe('api client error handling', () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('raises a session-expired event on 401 for an authenticated call', async () => {
    const heard = vi.fn();
    window.addEventListener(SESSION_EXPIRED_EVENT, heard);
    fetchMock.mockReturnValue(respond(401, 'unauthorized'));

    await expect(systemApi.getVersion()).rejects.toBeInstanceOf(ApiError);
    expect(heard).toHaveBeenCalledOnce();
    window.removeEventListener(SESSION_EXPIRED_EVENT, heard);
  });

  it('classifies 403 as forbidden', async () => {
    fetchMock.mockReturnValue(respond(403, 'not found'));
    const error = await systemApi.getVersion().catch((e) => e);
    expect(isForbidden(error)).toBe(true);
    expect(isForbidden(new Error('x'))).toBe(false);
  });
});
