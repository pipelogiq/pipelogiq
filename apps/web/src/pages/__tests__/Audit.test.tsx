import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { auditApi } from '@/api/client';
import Audit from '@/pages/Audit';
import { selectOption } from '@/test/select';
import type { AuditListResponse, AuditRecord } from '@/types/api';

vi.mock('@/api/client', () => ({ auditApi: { list: vi.fn(), options: vi.fn() } }));

const record: AuditRecord = {
  id: 311,
  ts: '2026-09-17T10:00:00Z',
  userId: 7,
  email: 'operator@example.com',
  action: 'stage.rerun',
  target: 'stage:42',
  outcome: 'ok',
  remoteAddr: '127.0.0.1',
};
const response: AuditListResponse = { items: [record], totalCount: 311, page: 1, pageSize: 50, totalPages: 7 };
const clients: QueryClient[] = [];

function Location() {
  return <output data-testid="location">{useLocation().search}</output>;
}
function setup(url = '/settings/audit') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[url]}>
        <Audit />
        <Location />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(auditApi.list).mockImplementation(async (params) => ({
    ...response,
    page: params?.page ?? 1,
    pageSize: params?.pageSize ?? 50,
    totalPages: Math.ceil(response.totalCount / (params?.pageSize ?? 50)),
  }));
  vi.mocked(auditApi.options).mockResolvedValue({
    actions: ['stage.rerun', 'stage.skip', 'legacy_operation'],
    users: [
      { id: 7, email: 'operator@example.com' },
      { id: 9, email: 'former-user@example.com' },
    ],
  });
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});

describe('Audit', () => {
  it('restores shared filters and pagination from the URL, including unauthenticated actions', async () => {
    setup('/settings/audit?action=stage.rerun&userId=0&outcome=failed&page=5&pageSize=25');
    await screen.findByText('stage:42');
    expect(auditApi.list).toHaveBeenCalledWith(
      { action: 'stage.rerun', userId: 0, outcome: 'failed', page: 5, pageSize: 25 },
      expect.any(AbortSignal),
    );
    expect(screen.getByLabelText('Filter by action').textContent).toBe('Rerun stage');
    expect(screen.getByLabelText('Filter by user').textContent).toBe('Unauthenticated');
    expect(screen.getByLabelText('Filter by outcome').textContent).toBe('Failed');
    expect(screen.getByLabelText('Events per page').textContent).toBe('25');
    expect(screen.getByText('Page 5 of 13')).toBeTruthy();
  });

  it('combines server-side filters, resets pagination, and preserves unrelated query parameters', async () => {
    setup('/settings/audit?page=6&view=compact');
    await screen.findByText('stage:42');
    selectOption(screen.getByLabelText('Filter by action'), 'Skip stage');
    selectOption(screen.getByLabelText('Filter by user'), 'former-user@example.com · #9');
    selectOption(screen.getByLabelText('Filter by outcome'), 'Access denied');
    await waitFor(() =>
      expect(auditApi.list).toHaveBeenLastCalledWith(
        { action: 'stage.skip', userId: 9, outcome: 'denied', page: 1, pageSize: 50 },
        expect.any(AbortSignal),
      ),
    );
    expect(new URLSearchParams(screen.getByTestId('location').textContent!).get('view')).toBe('compact');
    expect(new URLSearchParams(screen.getByTestId('location').textContent!).has('page')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    await waitFor(() =>
      expect(auditApi.list).toHaveBeenLastCalledWith(
        { action: undefined, userId: undefined, outcome: undefined, page: 1, pageSize: 50 },
        expect.any(AbortSignal),
      ),
    );
    expect(screen.getByTestId('location').textContent).toBe('?view=compact');
  });

  it('loads history past the old 200-event window and resets the page when page size changes', async () => {
    setup();
    await screen.findByText('Page 1 of 7');
    fireEvent.click(screen.getByRole('button', { name: 'Last page' }));
    await screen.findByText('Page 7 of 7');
    expect(auditApi.list).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 7, pageSize: 50 }),
      expect.any(AbortSignal),
    );
    expect((screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    await screen.findByText('Page 6 of 7');
    selectOption(screen.getByLabelText('Events per page'), '100');
    await screen.findByText('Page 1 of 4');
    expect(auditApi.list).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 1, pageSize: 100 }),
      expect.any(AbortSignal),
    );
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('distinguishes no matches from an empty audit history', async () => {
    vi.mocked(auditApi.list).mockResolvedValue({ ...response, items: [], totalCount: 0, totalPages: 0 });
    setup('/settings/audit?userId=9');
    expect(await screen.findByText('No matching events')).toBeTruthy();
    expect(screen.queryByText('Nothing recorded yet')).toBeNull();
    fireEvent.click(screen.getAllByRole('button', { name: 'Clear filters' })[0]);
    expect(await screen.findByText('Nothing recorded yet')).toBeTruthy();
  });

  it('shows and retries list errors without presenting them as an empty history', async () => {
    vi.mocked(auditApi.list).mockRejectedValueOnce(new Error('Unavailable'));
    setup();
    expect(await screen.findByText('Could not load the audit log.')).toBeTruthy();
    expect(screen.queryByText('Nothing recorded yet')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('stage:42')).toBeTruthy();
  });

  it('retains selected historical filters even when filter options fail and allows a retry', async () => {
    vi.mocked(auditApi.options).mockRejectedValueOnce(new Error('Unavailable'));
    setup('/settings/audit?action=deleted_legacy_action&userId=93');
    await screen.findByText(/Could not load filter choices/);
    expect(screen.getByLabelText('Filter by action').textContent).toBe('Deleted legacy action');
    expect(screen.getByLabelText('Filter by user').textContent).toBe('User #93');
    fireEvent.click(screen.getByRole('button', { name: 'Retry filters' }));
    await waitFor(() => expect(screen.queryByText(/Could not load filter choices/)).toBeNull());
    selectOption(screen.getByLabelText('Filter by user'), 'former-user@example.com · #9');
    expect(screen.getByLabelText('Filter by user').textContent).toBe('former-user@example.com · #9');
  });

  it('distinguishes unauthenticated users from all users and clears filters through each dropdown', async () => {
    setup('/settings/audit?action=stage.skip&outcome=denied&page=4&view=compact');
    await screen.findByText('stage:42');
    selectOption(screen.getByLabelText('Filter by user'), 'Unauthenticated');
    await waitFor(() => expect(auditApi.list).toHaveBeenLastCalledWith(
      { action: 'stage.skip', userId: 0, outcome: 'denied', page: 1, pageSize: 50 },
      expect.any(AbortSignal),
    ));
    selectOption(screen.getByLabelText('Filter by action'), 'All actions');
    selectOption(screen.getByLabelText('Filter by user'), 'All users');
    selectOption(screen.getByLabelText('Filter by outcome'), 'All outcomes');
    await waitFor(() => expect(auditApi.list).toHaveBeenLastCalledWith(
      { action: undefined, userId: undefined, outcome: undefined, page: 1, pageSize: 50 },
      expect.any(AbortSignal),
    ));
    expect(screen.getByTestId('location').textContent).toBe('?view=compact');
  });

  it('renders bulk counts and individual outcomes as readable details with sensitive keys excluded', async () => {
    vi.mocked(auditApi.list).mockResolvedValue({
      ...response,
      items: [
        {
          ...record,
          action: 'stage.bulk_rerun',
          outcome: 'partial',
          details: JSON.stringify({
            requested: 2,
            succeeded: 1,
            failed: 1,
            results: [
              { id: 42, scope: 'stage', success: true },
              { id: 43, scope: 'stage', success: false },
            ],
            token: 'hidden-token',
            config: { password: 'hidden-password' },
          }),
        },
      ],
    });
    setup();
    const detailsButton = await screen.findByRole('button', { name: 'Show details for event 311' });
    expect(screen.queryByText('Requested')).toBeNull();
    fireEvent.click(detailsButton);
    const details = screen.getByText('Event details').parentElement!;
    expect(within(details).getByText('Requested')).toBeTruthy();
    expect(within(details).getByText('Succeeded')).toBeTruthy();
    expect(within(details).getByText('42')).toBeTruthy();
    expect(within(details).getByText('43')).toBeTruthy();
    expect(within(details).getByText('Success')).toBeTruthy();
    expect(within(details).getAllByText('Failed')).toHaveLength(2);
    expect(screen.queryByText('hidden-token')).toBeNull();
    expect(screen.queryByText('hidden-password')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Hide details for event 311' }));
    expect(screen.queryByText('Requested')).toBeNull();
  });

  it.each(['abc', '10000001'])('uses safe defaults for invalid page %s and unsupported URL filters', async (page) => {
    setup(`/settings/audit?userId=-1&page=${page}&pageSize=999&outcome=anything`);
    await screen.findByText('stage:42');
    expect(auditApi.list).toHaveBeenCalledWith(
      { action: undefined, userId: undefined, outcome: undefined, page: 1, pageSize: 50 },
      expect.any(AbortSignal),
    );
  });

  it('refreshes records and filter choices together', async () => {
    setup();
    await screen.findByText('stage:42');
    await waitFor(() =>
      expect((screen.getByRole('button', { name: 'Refresh' }) as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(auditApi.list).toHaveBeenCalledTimes(2));
    expect(auditApi.options).toHaveBeenCalledTimes(2);
  });
});
