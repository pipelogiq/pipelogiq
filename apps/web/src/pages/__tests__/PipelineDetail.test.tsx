import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import PipelineDetail from '@/pages/PipelineDetail';
import type { PipelineResponse } from '@/types/api';
import { selectOption } from '@/test/select';

const mocks = vi.hoisted(() => ({
  canOperate: false,
  query: {
    data: undefined as PipelineResponse | undefined,
    isPending: false,
    isError: false,
    isFetching: false,
    error: null as Error | null,
    refetch: vi.fn(),
  },
  detail: vi.fn(),
  pause: vi.fn(),
  resume: vi.fn(),
  rerun: vi.fn(),
  skip: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('@/hooks/use-pipeline-detail', () => ({
  usePipelineDetail: (id: number) => {
    mocks.detail(id);
    return mocks.query;
  },
}));
vi.mock('@/hooks/use-pipelines', () => ({
  usePausePipeline: () => ({ mutateAsync: mocks.pause, isPending: false }),
  useResumePipeline: () => ({ mutateAsync: mocks.resume, isPending: false }),
  useRerunStage: () => ({ mutateAsync: mocks.rerun, isPending: false }),
  useSkipStage: () => ({ mutateAsync: mocks.skip, isPending: false }),
}));
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ canOperate: mocks.canOperate }) }));
vi.mock('@/hooks/use-toast', () => ({ useToast: () => ({ toast: mocks.toast }), toast: mocks.toast }));
vi.mock('@/components/layout/AppHeader', () => ({
  AppHeader: ({ title, subtitle }: { title: string; subtitle?: string }) => (
    <header>
      <h1>{title}</h1>
      <p>{subtitle}</p>
    </header>
  ),
}));

const pipeline: PipelineResponse = {
  id: 17,
  name: 'Invoice processing',
  applicationId: 5,
  status: 'Paused',
  isTerminal: false,
  createdAt: '2026-09-17T10:00:00Z',
  traceId: 'trace-invoice-17',
  idempotencyKey: 'invoice-991',
  pipelineContextItems: [
    { key: 'currency', value: '"EUR"' },
    { key: 'customer', value: '{"name":"Ada","account":"C-71"}' },
  ],
  stages: [
    {
      id: 31,
      pipelineId: 17,
      name: 'Charge payment',
      status: 'Failed',
      stageHandlerName: 'ChargePayment',
      createdAt: '2026-09-17T10:00:00Z',
      startedAt: '2026-09-17T10:01:00Z',
      finishedAt: '2026-09-17T10:02:00Z',
      input: 'false',
      output: 'null',
      attempt: 2,
      retryAttempt: 1,
      logs: [
        {
          id: 81,
          stageId: 31,
          message: 'Payment authorization failed',
          logLevel: 'ERROR',
          created: '2026-09-17T10:02:00Z',
        },
      ],
    },
    {
      id: 32,
      pipelineId: 17,
      name: 'Store invoice',
      status: 'Completed',
      stageHandlerName: 'StoreInvoice',
      createdAt: '2026-09-17T10:00:00Z',
      startedAt: '2026-09-17T10:03:00Z',
      finishedAt: '2026-09-17T10:04:00Z',
      input: '{"invoiceId":"INV-991"}',
      output: '{"stored":true}',
      logs: [
        {
          id: 82,
          stageId: 32,
          message: 'Invoice stored successfully',
          logLevel: 'INFO',
          created: '2026-09-17T10:04:00Z',
        },
      ],
    },
  ],
};

function Location() {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.pathname}
      {location.search}
    </output>
  );
}

function setup(path = '/pipelines/17') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/pipelines/:id" element={<PipelineDetail />} />
      </Routes>
      <Location />
    </MemoryRouter>,
  );
}

function selectTab(name: RegExp | string) {
  fireEvent.mouseDown(screen.getByRole('tab', { name }), { button: 0, ctrlKey: false });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.canOperate = false;
  mocks.query.data = structuredClone(pipeline);
  mocks.query.isPending = false;
  mocks.query.isError = false;
  mocks.query.isFetching = false;
  mocks.query.error = null;
  for (const operation of [mocks.pause, mocks.resume, mocks.rerun, mocks.skip])
    operation.mockReset().mockResolvedValue(undefined);
});
afterEach(cleanup);

describe('PipelineDetail', () => {
  it('opens a deep-linked logs tab filtered to its requested stage', () => {
    setup('/pipelines/17?tab=logs&stage=32');
    expect(mocks.detail).toHaveBeenCalledWith(17);
    expect(screen.getByRole('tab', { name: 'Logs (2)' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByLabelText('Filter logs by stage').textContent).toBe('2. Store invoice');
    expect(screen.getByText('Invoice stored successfully')).toBeTruthy();
    expect(screen.queryByText('Payment authorization failed')).toBeNull();
    selectOption(screen.getByLabelText('Filter logs by stage'), 'All stages');
    expect(screen.getByTestId('location').textContent).toBe('/pipelines/17?tab=logs');
    expect(screen.getByText('Payment authorization failed')).toBeTruthy();
  });

  it('updates the URL when a stage is selected and when its logs are opened', () => {
    setup('/pipelines/17?tab=stages&view=shared');
    const stageNavigation = screen.getByRole('table', { name: 'Pipeline stages' });
    fireEvent.click(within(stageNavigation).getByRole('button', { name: /Store invoice/ }));
    expect(screen.getByTestId('location').textContent).toBe('/pipelines/17?tab=stages&view=shared&stage=32');
    expect(
      within(screen.getByRole('region', { name: 'Stage details' })).getByRole('heading', { name: 'Store invoice' }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'View stage logs' }));
    expect(screen.getByTestId('location').textContent).toBe('/pipelines/17?tab=logs&view=shared&stage=32');
    expect(screen.getByText('Invoice stored successfully')).toBeTruthy();
    expect(screen.queryByText('Payment authorization failed')).toBeNull();
  });

  it('keeps the table collapsed from a shared URL and allows opening and closing a stage', () => {
    setup('/pipelines/17?tab=stages&stage=none');
    expect(screen.queryByRole('region', { name: 'Stage details' })).toBeNull();
    const stage = within(screen.getByRole('table', { name: 'Pipeline stages' })).getByRole('button', { name: /Charge payment/ });
    fireEvent.click(stage);
    expect(stage.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByTestId('location').textContent).toBe('/pipelines/17?tab=stages&stage=31');
    expect(screen.getByLabelText('Input payload')).toBeTruthy();
    fireEvent.click(stage);
    expect(stage.getAttribute('aria-expanded')).toBe('false');
    expect(screen.getByTestId('location').textContent).toBe('/pipelines/17?tab=stages&stage=none');
    expect(screen.queryByLabelText('Input payload')).toBeNull();
  });

  it('keeps literal JSON false and null visible as valid payloads', () => {
    setup('/pipelines/17?stage=31');
    expect(screen.getByLabelText('Input payload').textContent).toBe('false');
    expect(screen.getByLabelText('Output payload').textContent).toBe('null');
    expect(within(screen.getByRole('region', { name: 'Input' })).getByText('JSON')).toBeTruthy();
    expect(within(screen.getByRole('region', { name: 'Output' })).getByText('JSON')).toBeTruthy();
    expect(screen.queryByText('No payload recorded.')).toBeNull();
  });

  it('searches context values and exposes the complete run data', () => {
    setup('/pipelines/17?tab=context');
    fireEvent.change(screen.getByLabelText('Search context'), { target: { value: 'c-71' } });
    const contextTable = screen.getByRole('table', { name: 'Pipeline context items' });
    expect(within(contextTable).getByText('customer')).toBeTruthy();
    expect(within(contextTable).queryByText('currency')).toBeNull();
    fireEvent.click(within(contextTable).getByRole('button', { name: 'Expand customer' }));
    expect(screen.getByLabelText('customer payload').textContent).toContain('C-71');
    selectTab('All data');
    expect(screen.getByTestId('location').textContent).toBe('/pipelines/17?tab=data');
    const recorded = JSON.parse(screen.getByLabelText('Run data payload').textContent!);
    expect(recorded).toEqual(pipeline);
  });

  it.each(['not-a-number', '0', '-17'])('does not show cached data for invalid run ID %s', (id) => {
    setup(`/pipelines/${id}`);
    expect(screen.getByRole('alert').textContent).toContain('Pipeline unavailable');
    expect(screen.queryByRole('heading', { name: 'Invoice processing' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Run summary' })).toBeNull();
    expect(mocks.detail).toHaveBeenCalledWith(0);
  });

  it.each([403, 404])('shows an unavailable run on HTTP %s without revealing stale run data', (status) => {
    mocks.query.isError = true;
    mocks.query.error = new ApiError(status, 'Private invoice record');
    setup();
    expect(screen.getByRole('alert').textContent).toContain('Pipeline unavailable');
    expect(screen.queryByText('Private invoice record')).toBeNull();
    expect(screen.queryByRole('region', { name: 'Run summary' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(mocks.query.refetch).toHaveBeenCalledOnce();
  });

  it('lets viewers inspect stages while hiding every mutation control', () => {
    setup('/pipelines/17?stage=31');
    expect(screen.getByText(/Read-only access/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'View stage logs' })).toBeTruthy();
    for (const name of ['Resume pipeline', 'Pause pipeline', 'Rerun stage', 'Skip stage']) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
    expect(mocks.resume).not.toHaveBeenCalled();
  });

  it('lets an operator resume a paused run and rerun or skip the selected failed stage', async () => {
    mocks.canOperate = true;
    setup('/pipelines/17?stage=31');
    fireEvent.click(screen.getByRole('button', { name: 'Resume pipeline' }));
    fireEvent.click(screen.getByRole('button', { name: 'Rerun stage' }));
    fireEvent.click(screen.getByRole('button', { name: 'Skip stage' }));
    expect(mocks.resume).toHaveBeenCalledWith(17);
    expect(mocks.rerun).toHaveBeenCalledWith(31);
    expect(mocks.skip).toHaveBeenCalledWith(31);
    await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith({ title: 'Stage skipped' }));
  });

  it('lets an operator pause an active run without offering resume', () => {
    mocks.canOperate = true;
    mocks.query.data!.status = 'Running';
    setup();
    fireEvent.click(screen.getByRole('button', { name: 'Pause pipeline' }));
    expect(mocks.pause).toHaveBeenCalledWith(17);
    expect(screen.queryByRole('button', { name: 'Resume pipeline' })).toBeNull();
  });

  it('does not offer resume, rerun or skip for a cancelled terminal run', () => {
    mocks.canOperate = true;
    mocks.query.data!.status = 'Cancelled';
    mocks.query.data!.isTerminal = true;
    setup('/pipelines/17?stage=31');
    expect(within(screen.getByRole('region', { name: 'Run summary' })).getByText('Cancelled')).toBeTruthy();
    for (const name of ['Resume pipeline', 'Pause pipeline', 'Rerun stage', 'Skip stage']) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
    expect(screen.getByRole('button', { name: 'View stage logs' })).toBeTruthy();
  });

  it('does not silently substitute another stage for a stale stage link', () => {
    setup('/pipelines/17?tab=stages&stage=999');
    expect(screen.getByText('This stage is not part of this run.')).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Stage details' })).toBeNull();
    expect(screen.queryByLabelText('Input payload')).toBeNull();
    expect(screen.queryByRole('button', { name: 'View stage logs' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Select a stage' }));
    expect(screen.getByTestId('location').textContent).toBe('/pipelines/17?tab=stages');
    expect(
      within(screen.getByRole('region', { name: 'Stage details' })).getByRole('heading', { name: 'Charge payment' }),
    ).toBeTruthy();
  });

  it('reports a rejected operation and does not claim it succeeded', async () => {
    mocks.canOperate = true;
    mocks.rerun.mockRejectedValueOnce(new Error('Stage can no longer be rerun'));
    setup('/pipelines/17?stage=31');
    fireEvent.click(screen.getByRole('button', { name: 'Rerun stage' }));
    await waitFor(() =>
      expect(mocks.toast).toHaveBeenCalledWith({
        title: 'Operation failed',
        description: 'Stage can no longer be rerun',
        variant: 'destructive',
      }),
    );
    expect(mocks.toast).not.toHaveBeenCalledWith({ title: 'Stage queued for rerun' });
  });
});
