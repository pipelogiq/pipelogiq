import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import Account from '@/pages/Account';

const mocks = vi.hoisted(() => ({ changePassword: vi.fn(), toast: vi.fn() }));
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ user: { firstName: 'Ada', lastName: 'Lovelace', email: 'ada@example.com', role: 'Viewer' }, canOperate: false, isAdmin: false }),
}));
vi.mock('@/components/layout/AppHeader', () => ({ AppHeader: ({ title }: { title: string }) => <h1>{title}</h1> }));
vi.mock('@/api/client', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/api/client')>(),
  authApi: { changePassword: mocks.changePassword },
}));
vi.mock('@/hooks/use-toast', () => ({ useToast: () => ({ toast: mocks.toast }) }));

beforeEach(() => { vi.clearAllMocks(); mocks.changePassword.mockResolvedValue(undefined); });
afterEach(cleanup);

describe('self-service account access', () => {
  it('lets viewers change their own password and clears the form after success', async () => {
    render(<Account />);
    expect(screen.getByText('Viewer')).toBeTruthy();
    expect(screen.getByText('ada@example.com')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'current-password' } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password-123' } });
    fireEvent.change(screen.getByLabelText('Repeat new password'), { target: { value: 'new-password-123' } });
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }));
    await waitFor(() => expect(mocks.changePassword).toHaveBeenCalledWith({ currentPassword: 'current-password', newPassword: 'new-password-123' }));
    await waitFor(() => expect((screen.getByLabelText('Current password') as HTMLInputElement).value).toBe(''));
    expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Password changed' }));
  });

  it('rejects mismatched new passwords before calling the API', () => {
    render(<Account />);
    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'current-password' } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password-123' } });
    fireEvent.change(screen.getByLabelText('Repeat new password'), { target: { value: 'different-password' } });
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }));
    expect(mocks.changePassword).not.toHaveBeenCalled();
    expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'New passwords do not match' }));
  });

  it('rejects a multibyte password exceeding the server byte limit', () => {
    render(<Account />);
    const password = '界'.repeat(25);
    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'current-password' } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: password } });
    fireEvent.change(screen.getByLabelText('Repeat new password'), { target: { value: password } });
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }));
    expect(mocks.changePassword).not.toHaveBeenCalled();
    expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'New password is too long' }));
  });

  it('counts Unicode characters rather than UTF-16 units for the password minimum', () => {
    render(<Account />);
    const password = '😀'.repeat(6);
    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'current-password' } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: password } });
    fireEvent.change(screen.getByLabelText('Repeat new password'), { target: { value: password } });
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }));
    expect(mocks.changePassword).not.toHaveBeenCalled();
    expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Use at least 12 characters for your new password' }));
  });
});
