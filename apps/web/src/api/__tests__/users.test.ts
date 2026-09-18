import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usersApi } from '@/api/client';
import type { CreateUserRequest } from '@/types/users';

describe('user management API', () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset().mockImplementation(() => Promise.resolve(new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } })));
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it('loads membership options separately from application API keys', async () => {
    await usersApi.getApplications();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/users/applications');
  });

  it('sends role, status, and application assignments when creating an account', async () => {
    const data: CreateUserRequest = {
      firstName: 'Alex', email: 'alex@example.com', role: 'viewer',
      password: 'long-password-123', isActive: true, applicationIds: [5, 8],
    };
    await usersApi.create(data);
    expect(fetchMock).toHaveBeenCalledWith('/api/users', expect.objectContaining({ method: 'POST', credentials: 'include', body: JSON.stringify(data) }));
  });

  it('uses the password reset endpoint and accepts an empty response', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    await expect(usersApi.resetPassword(42, 'new-password-123')).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith('/api/users/42/password', expect.objectContaining({ method: 'POST', body: JSON.stringify({ password: 'new-password-123' }) }));
  });
});
