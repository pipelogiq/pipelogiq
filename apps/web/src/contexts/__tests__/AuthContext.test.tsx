import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { authApi, SESSION_EXPIRED_EVENT } from '@/api/client';
import { AuthProvider, useAuth } from '@/contexts/AuthContext';
import type { User } from '@/types/api';

vi.mock('@/api/client', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/client')>(),
  authApi: { getCurrentUser: vi.fn(), login: vi.fn(), logout: vi.fn() },
}));

const admin: User = { id: 1, firstName: 'Admin', email: 'admin@example.com', role: 'admin', createdAt: '2026-01-01' };

function setup() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}><AuthProvider>{children}</AuthProvider></QueryClientProvider>
  );
  return { ...renderHook(useAuth, { wrapper }), queryClient };
}

beforeEach(() => {
  vi.mocked(authApi.getCurrentUser).mockReset().mockResolvedValue(admin);
  vi.mocked(authApi.login).mockReset().mockResolvedValue(undefined);
  vi.mocked(authApi.logout).mockReset().mockResolvedValue(undefined);
});
afterEach(cleanup);

describe('role capabilities', () => {
  it.each([
    [' admin ', true, true], ['Administrator', true, true], ['SUPERADMIN', true, true],
    [' Operator ', false, true], ['User', false, true], ['viewer', false, false], ['unknown', false, false],
  ])('normalizes %s without granting unknown roles access', async (role, isAdmin, canOperate) => {
    vi.mocked(authApi.getCurrentUser).mockResolvedValue({ ...admin, role });
    const { result } = setup();
    await waitFor(() => expect(result.current.isAuthenticated).toBe(true));
    expect(result.current.isAdmin).toBe(isAdmin);
    expect(result.current.canOperate).toBe(canOperate);
  });
});

describe('account cache isolation', () => {
  it('does not restore an initial session check after an expiration event', async () => {
    let finishInitialCheck!: (user: User) => void;
    vi.mocked(authApi.getCurrentUser).mockReturnValue(new Promise(resolve => { finishInitialCheck = resolve; }));
    const { result, queryClient } = setup();
    expect(result.current.isLoading).toBe(true);
    expect(result.current.user).toBeNull();
    queryClient.setQueryData(['pipelines'], ['private']);

    act(() => window.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT)));
    expect(result.current.isLoading).toBe(false);
    await act(async () => { finishInitialCheck(admin); });

    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.isLoading).toBe(false);
    expect(queryClient.getQueryData(['pipelines'])).toBeUndefined();
  });

  it('clears cached data when the session expires', async () => {
    const { result, queryClient } = setup();
    await waitFor(() => expect(result.current.isAuthenticated).toBe(true));
    queryClient.setQueryData(['pipelines'], ['private']);
    act(() => window.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT)));
    expect(result.current.isAuthenticated).toBe(false);
    expect(queryClient.getQueryData(['pipelines'])).toBeUndefined();
  });

  it('clears cached data immediately even when the logout request fails', async () => {
    const { result, queryClient } = setup();
    await waitFor(() => expect(result.current.isAuthenticated).toBe(true));
    queryClient.setQueryData(['pipelines'], ['private']);
    vi.mocked(authApi.logout).mockRejectedValue(new Error('offline'));
    await act(async () => { await expect(result.current.logout()).rejects.toThrow('offline'); });
    expect(result.current.isAuthenticated).toBe(false);
    expect(queryClient.getQueryData(['pipelines'])).toBeUndefined();
  });

  it('clears another account’s data when authentication changes', async () => {
    const { result, queryClient } = setup();
    await waitFor(() => expect(result.current.isAuthenticated).toBe(true));
    queryClient.setQueryData(['pipelines'], ['private']);
    vi.mocked(authApi.getCurrentUser).mockResolvedValue({ ...admin, id: 2, role: 'viewer' });
    await act(async () => { await result.current.checkAuth(); });
    expect(result.current.user?.id).toBe(2);
    expect(result.current.canOperate).toBe(false);
    expect(queryClient.getQueryData(['pipelines'])).toBeUndefined();
  });

  it('does not restore a session from an auth check finishing after logout', async () => {
    const { result } = setup();
    await waitFor(() => expect(result.current.isAuthenticated).toBe(true));
    let resolveAuth!: (user: User) => void;
    vi.mocked(authApi.getCurrentUser).mockReturnValue(new Promise(resolve => { resolveAuth = resolve; }));
    let pending!: Promise<void>;
    act(() => { pending = result.current.checkAuth(); });
    await act(async () => { await result.current.logout(); });
    await act(async () => { resolveAuth(admin); await pending; });
    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.isLoading).toBe(false);
  });

  it('does not restore a session when a pending login finishes after logout', async () => {
    const { result, queryClient } = setup();
    await waitFor(() => expect(result.current.isAuthenticated).toBe(true));
    let finishLogin!: () => void;
    vi.mocked(authApi.login).mockReturnValue(new Promise(resolve => { finishLogin = resolve; }));
    let pending!: Promise<void>;
    act(() => { pending = result.current.login({ email: admin.email, password: 'test-password' }); });
    await act(async () => { await result.current.logout(); });
    await act(async () => { finishLogin(); await pending; });
    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.isLoading).toBe(false);
    expect(authApi.getCurrentUser).toHaveBeenCalledOnce();
    expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
  });

  it('does not overwrite a newer role check with an older privileged response', async () => {
    const { result, queryClient } = setup();
    await waitFor(() => expect(result.current.isAuthenticated).toBe(true));
    queryClient.setQueryData(['users'], ['administrator-only']);
    let finishOldCheck!: (user: User) => void;
    vi.mocked(authApi.getCurrentUser).mockReturnValueOnce(new Promise(resolve => { finishOldCheck = resolve; }));
    let oldCheck!: Promise<void>;
    act(() => { oldCheck = result.current.checkAuth(); });
    vi.mocked(authApi.getCurrentUser).mockResolvedValue({ ...admin, role: 'viewer' });
    await act(async () => { await result.current.checkAuth(); });
    await act(async () => { finishOldCheck(admin); await oldCheck; });
    expect(result.current.user?.role).toBe('viewer');
    expect(result.current.canOperate).toBe(false);
    expect(queryClient.getQueryData(['users'])).toBeUndefined();
  });
});
