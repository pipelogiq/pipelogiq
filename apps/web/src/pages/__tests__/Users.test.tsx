import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, usersApi } from '@/api/client';
import { ConfirmDialogProvider } from '@/components/ui/confirm-dialog';
import Users from '@/pages/Users';
import { selectOption } from '@/test/select';
import type { ManagedUser } from '@/types/users';

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ user: { id: 1 }, isAdmin: true, checkAuth: vi.fn() }),
}));
vi.mock('@/components/layout/AppHeader', () => ({ AppHeader: () => <h1>Users</h1> }));
vi.mock('@/hooks/use-toast', () => ({ useToast: () => ({ toast: vi.fn() }) }));
vi.mock('@/api/client', async importOriginal => {
  const { ApiError } = await importOriginal<typeof import('@/api/client')>();
  return {
    ApiError,
    usersApi: { getAll: vi.fn(), getApplications: vi.fn(), getRoles: vi.fn(), create: vi.fn(), update: vi.fn(), resetPassword: vi.fn() },
  };
});

const users: ManagedUser[] = [
  { id: 1, firstName: 'Admin', email: 'admin@example.com', role: 'admin', isActive: true, createdAt: '2026-01-01', applicationIds: [7] },
  { id: 2, firstName: 'Alice', email: 'alice@example.com', role: 'viewer', isActive: true, createdAt: '2026-01-01', applicationIds: [7] },
  { id: 3, firstName: 'Bob', email: 'bob@example.com', role: 'operator', isActive: false, createdAt: '2026-01-01', applicationIds: [] },
];

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}><MemoryRouter><ConfirmDialogProvider><Users /></ConfirmDialogProvider></MemoryRouter></QueryClientProvider>);
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal('ResizeObserver', class { observe = vi.fn(); unobserve = vi.fn(); disconnect = vi.fn(); });
  vi.mocked(usersApi.getAll).mockResolvedValue(users);
  vi.mocked(usersApi.getApplications).mockResolvedValue([{ id: 7, name: 'Payments' }]);
  vi.mocked(usersApi.getRoles).mockResolvedValue([
    { id: 'admin', name: 'Admin', description: 'Manage all users and settings.', permissions: ['users:manage'] },
    { id: 'operator', name: 'Operator', description: 'Operate assigned applications.', permissions: ['pipelines:operate'] },
    { id: 'viewer', name: 'Viewer', description: 'Read assigned applications.', permissions: ['pipelines:read'] },
  ]);
  vi.mocked(usersApi.create).mockImplementation(async data => ({ ...data, id: 4, createdAt: '2026-01-01' }));
  vi.mocked(usersApi.update).mockImplementation(async (id, data) => ({ ...data, id, createdAt: '2026-01-01' }));
  vi.mocked(usersApi.resetPassword).mockResolvedValue(undefined);
});

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe('Users', () => {
  it('searches accounts and combines role and status filters', async () => {
    setup();
    await screen.findByText('alice@example.com');
    selectOption(screen.getByLabelText('Filter by role'), 'Viewer');
    expect(screen.queryByText('admin@example.com')).toBeNull();
    expect(screen.getByText('alice@example.com')).toBeTruthy();
    selectOption(screen.getByLabelText('Filter by status'), 'Inactive');
    expect(screen.getByText('No matching users')).toBeTruthy();
    selectOption(screen.getByLabelText('Filter by role'), 'All roles');
    selectOption(screen.getByLabelText('Filter by status'), 'All statuses');
    fireEvent.change(screen.getByLabelText('Search users'), { target: { value: 'BOB@' } });
    expect(screen.getByText('bob@example.com')).toBeTruthy();
    expect(screen.queryByText('alice@example.com')).toBeNull();
  });

  it('protects the current account and retains admin application assignments on save', async () => {
    setup();
    const edit = await screen.findByLabelText('Edit admin@example.com');
    await waitFor(() => expect((edit as HTMLButtonElement).disabled).toBe(false));
    expect((screen.getByLabelText('Deactivate admin@example.com') as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByLabelText('Change your password').getAttribute('href')).toBe('/settings/security');
    fireEvent.click(edit);
    const dialog = screen.getByRole('dialog');
    expect((within(dialog).getByLabelText('Role') as HTMLButtonElement).disabled).toBe(true);
    expect((within(dialog).getByLabelText('Account is active') as HTMLButtonElement).disabled).toBe(true);
    expect(within(dialog).getByLabelText('Payments').getAttribute('data-state')).toBe('checked');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(usersApi.update).toHaveBeenCalledWith(1, expect.objectContaining({ role: 'admin', isActive: true, applicationIds: [7] })));
  });

  it('creates an account with a chosen role and assigned applications', async () => {
    setup();
    await waitFor(() => expect((screen.getByRole('button', { name: 'Add user' }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole('button', { name: 'Add user' }));
    const dialog = screen.getByRole('dialog');
    fireEvent.change(within(dialog).getByLabelText('First name'), { target: { value: ' Casey ' } });
    fireEvent.change(within(dialog).getByLabelText('Email'), { target: { value: 'casey@example.com' } });
    fireEvent.change(within(dialog).getByLabelText('Initial password'), { target: { value: 'new-password-123' } });
    expect(within(dialog).getByLabelText('Role').textContent).toBe('Viewer');
    selectOption(within(dialog).getByLabelText('Role'), 'Operator');
    expect(within(dialog).getByLabelText('Role').textContent).toBe('Operator');
    fireEvent.click(within(dialog).getByLabelText('Payments'));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create user' }));
    await waitFor(() => expect(vi.mocked(usersApi.create).mock.calls[0]?.[0]).toEqual(expect.objectContaining({ firstName: 'Casey', role: 'operator', isActive: true, applicationIds: [7] })));
  });

  it('validates multibyte password length before sending a reset', async () => {
    setup();
    fireEvent.click(await screen.findByLabelText('Reset password for alice@example.com'));
    const dialog = screen.getByRole('dialog');
    const tooLong = '🔑'.repeat(20);
    fireEvent.change(within(dialog).getByLabelText('New password'), { target: { value: tooLong } });
    fireEvent.change(within(dialog).getByLabelText('Confirm new password'), { target: { value: tooLong } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reset password' }));
    expect(within(dialog).getByRole('alert').textContent).toContain('too long');
    expect(usersApi.resetPassword).not.toHaveBeenCalled();
    const valid = 'unique-password-2026';
    fireEvent.change(within(dialog).getByLabelText('New password'), { target: { value: valid } });
    fireEvent.change(within(dialog).getByLabelText('Confirm new password'), { target: { value: valid } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reset password' }));
    await waitFor(() => expect(usersApi.resetPassword).toHaveBeenCalledWith(2, valid));
  });

  it('shows a load error instead of claiming there are no users', async () => {
    vi.mocked(usersApi.getAll).mockRejectedValue(new Error('User list unavailable'));
    setup();
    expect(await screen.findByText('User list unavailable')).toBeTruthy();
    expect(screen.queryByText('No users yet')).toBeNull();
  });

  it('explains a missing API once and retries all user management data', async () => {
    const unavailable = new ApiError(404, '404 page not found');
    vi.mocked(usersApi.getAll).mockRejectedValueOnce(unavailable);
    vi.mocked(usersApi.getApplications).mockRejectedValueOnce(unavailable);
    vi.mocked(usersApi.getRoles).mockRejectedValueOnce(unavailable);
    setup();
    expect(await screen.findByText('User management is unavailable')).toBeTruthy();
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(screen.getByText(/dashboard and API are running the same version/)).toBeTruthy();
    expect(screen.queryByText('404 page not found')).toBeNull();
    expect(screen.queryByRole('region', { name: 'User accounts' })).toBeNull();
    expect((screen.getByRole('button', { name: 'Add user' }) as HTMLButtonElement).disabled).toBe(true);
    await waitFor(() => expect((screen.getByRole('button', { name: 'Try again' }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('alice@example.com')).toBeTruthy();
    expect(usersApi.getAll).toHaveBeenCalledTimes(2);
    expect(usersApi.getApplications).toHaveBeenCalledTimes(2);
    expect(usersApi.getRoles).toHaveBeenCalledTimes(2);
  });

  it('reactivates an account without replacing its role or application assignments', async () => {
    setup();
    fireEvent.click(await screen.findByLabelText('Reactivate bob@example.com'));
    await waitFor(() => expect(usersApi.update).toHaveBeenCalledWith(3, expect.objectContaining({ role: 'operator', isActive: true, applicationIds: [] })));
  });
});
