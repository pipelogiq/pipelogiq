import type { ReactNode } from 'react';
import { cleanup, render, screen } from '@testing-library/react';
import { Outlet } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '@/App';

const auth = vi.hoisted(() => ({ isAuthenticated: true, isAdmin: false, canOperate: false }));
vi.mock('@/contexts/AuthContext', () => ({
  AuthProvider: ({ children }: { children: ReactNode }) => children,
  useAuth: () => ({ ...auth, user: { id: 2, role: auth.canOperate ? 'operator' : 'viewer' }, isLoading: false }),
}));
vi.mock('next-themes', () => ({ ThemeProvider: ({ children }: { children: ReactNode }) => children }));
vi.mock('@/hooks/use-pipeline-ws', () => ({ usePipelineWebSocket: () => undefined }));
vi.mock('@/components/layout/AppLayout', () => ({ AppLayout: () => <Outlet /> }));
vi.mock('@/pages/Login', () => ({ default: () => <h1>Sign in</h1> }));

beforeEach(() => {
  auth.isAuthenticated = true;
  auth.isAdmin = false;
  auth.canOperate = false;
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Unexpected protected data request')));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); window.history.replaceState({}, '', '/'); });

describe('settings route access', () => {
  it.each([
    '/settings/users', '/settings/audit', '/settings/licence', '/settings/security',
    '/settings/api-keys', '/users', '/audit',
  ])('does not mount or fetch protected data for a viewer opening %s', async (path) => {
    window.history.replaceState({}, '', path);
    render(<App />);
    expect(await screen.findByRole('heading', { name: "You don't have access to settings" })).toBeTruthy();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('does not grant an operator access to user management', async () => {
    auth.canOperate = true;
    window.history.replaceState({}, '', '/settings/users');
    render(<App />);
    expect(await screen.findByRole('heading', { name: "You don't have access to settings" })).toBeTruthy();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('requires a session before opening a pipeline detail link', async () => {
    auth.isAuthenticated = false;
    window.history.replaceState({}, '', '/pipelines/17?tab=context');
    render(<App />);
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeTruthy();
    expect(fetch).not.toHaveBeenCalled();
  });
});
