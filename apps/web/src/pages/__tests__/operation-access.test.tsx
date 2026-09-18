import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Pipelines from '@/pages/Pipelines';
import Schedules from '@/pages/Schedules';
import ScheduleDetail from '@/pages/ScheduleDetail';
import { PipelineSidePanel } from '@/components/pipelines/PipelineSidePanel';
import type { PipelineExecution } from '@/components/pipelines/PipelineDetailPanel';

const mocks = vi.hoisted(() => ({
  canOperate: false,
  bulk: vi.fn(),
  pause: vi.fn(),
  resume: vi.fn(),
  rerun: vi.fn(),
  skip: vi.fn(),
  setStatus: vi.fn(),
  trigger: vi.fn(),
  updatePolicies: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ canOperate: mocks.canOperate }) }));
vi.mock('@/components/layout/AppHeader', () => ({ AppHeader: ({ title }: { title: string }) => <h1>{title}</h1> }));
vi.mock('@/components/pipelines/PipelineSummaryBar', () => ({ PipelineSummaryBar: () => null }));
vi.mock('@/components/pipelines/PipelineSearchBar', () => ({ PipelineSearchBar: () => <input aria-label="Search pipelines" /> }));
vi.mock('@/components/ui/scroll-area', () => ({ ScrollArea: ({ children }: { children: ReactNode }) => <div>{children}</div> }));
vi.mock('@/hooks/use-toast', () => ({ toast: mocks.toast, useToast: () => ({ toast: mocks.toast }) }));

const pipeline: PipelineExecution = {
  id: '17', pipelineId: '17', pipelineName: 'Invoice processing', description: '',
  status: 'paused', environment: 'production', startedAt: '1 minute ago', startedAtExact: '2026-09-17 12:00:00',
  owner: 'Billing', tags: [], correlationId: '', context: [], executionNumber: 17,
  traceId: '', traceUrl: '', logTraceUrl: '',
  stages: [{ id: '31', name: 'Charge payment', status: 'error' }],
  actions: [{ id: '31', name: 'Charge payment', status: 'error', logs: '', input: { invoice: 'INV-17' }, output: {}, error: 'Payment failed' }],
};

const schedule = {
  id: 8, name: 'Daily invoices', status: 'Active', kind: 'Cron', cronExpression: '0 0 * * *',
  timeZone: 'UTC', overlapPolicy: 'Skip', catchupPolicy: 'None', jitterSeconds: 0, definitionVersion: 1,
  definition: { name: 'Create invoices', stages: [{ stageName: 'Build invoice', stageHandlerName: 'invoice' }] },
};

vi.mock('@/hooks/use-pipelines', () => ({
  usePipelines: () => ({ data: { items: [pipeline], totalCount: 1 }, isLoading: false }),
  usePipeline: () => ({ data: pipeline, isLoading: false }),
  useBulkPipelineAction: () => ({ mutateAsync: mocks.bulk, isPending: false }),
  usePausePipeline: () => ({ mutate: mocks.pause, isPending: false }),
  useResumePipeline: () => ({ mutate: mocks.resume, isPending: false }),
  useRerunStage: () => ({ mutate: mocks.rerun, isPending: false }),
  useSkipStage: () => ({ mutate: mocks.skip, isPending: false }),
}));
vi.mock('@/hooks/use-schedules', () => ({
  useSchedules: () => ({ data: { schedules: [schedule] }, isLoading: false }),
  useSchedule: () => ({ data: schedule, isLoading: false }),
  useScheduleRuns: () => ({ data: { runs: [] }, isLoading: false }),
  useSetScheduleStatus: () => ({ mutate: mocks.setStatus, isPending: false }),
  useTriggerSchedule: () => ({ mutate: mocks.trigger, isPending: false }),
  useUpdateSchedulePolicies: () => ({ mutate: mocks.updatePolicies, isPending: false }),
}));

function wrap(children: ReactNode) {
  return <MemoryRouter>{children}</MemoryRouter>;
}

beforeEach(() => {
  mocks.canOperate = false;
  vi.clearAllMocks();
  mocks.bulk.mockResolvedValue({ requested: 1, succeeded: 1, failed: 0 });
});
afterEach(cleanup);

describe('pipeline operation access', () => {
  it('lets viewers open pipeline and stage details without selection or operation controls', () => {
    render(wrap(<Pipelines />));
    expect(screen.getByText(/Read-only access/)).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Search pipelines' })).toBeTruthy();
    expect(screen.queryByRole('checkbox')).toBeNull();
    fireEvent.click(screen.getByText('Invoice processing'));
    fireEvent.click(screen.getByRole('button', { name: /Charge payment/ }));
    expect(screen.getByText(/INV-17/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Resume' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Rerun Stage' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Skip Stage' })).toBeNull();
    expect(mocks.resume).not.toHaveBeenCalled();
    expect(mocks.rerun).not.toHaveBeenCalled();
  });

  it('allows operator bulk actions and hides previously selected actions after access is revoked', async () => {
    mocks.canOperate = true;
    const view = render(wrap(<Pipelines />));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select pipeline 17' }));
    fireEvent.click(screen.getByRole('button', { name: 'Pause' }));
    await waitFor(() => expect(mocks.bulk).toHaveBeenCalledWith({ action: 'pause', pipelineIds: [17], stageIds: undefined }));
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull());
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select pipeline 17' }));
    expect(screen.getByRole('button', { name: 'Pause' })).toBeTruthy();
    mocks.canOperate = false;
    view.rerender(wrap(<Pipelines />));
    expect(screen.queryByRole('checkbox')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull();
  });

  it('allows operators to resume pipelines and rerun or skip failed stages', () => {
    mocks.canOperate = true;
    render(wrap(<PipelineSidePanel pipelineId={17} onClose={vi.fn()} />));
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }));
    fireEvent.click(screen.getByRole('button', { name: /Charge payment/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Rerun Stage' }));
    fireEvent.click(screen.getByRole('button', { name: 'Skip Stage' }));
    expect(mocks.resume).toHaveBeenCalledWith(17);
    expect(mocks.rerun).toHaveBeenCalledWith(31);
    expect(mocks.skip).toHaveBeenCalledWith(31);
  });
});

describe('schedule operation access', () => {
  it('keeps schedule links readable and disables toggles for viewers', () => {
    render(wrap(<Schedules />));
    expect(screen.getByRole('link', { name: 'Daily invoices' }).getAttribute('href')).toBe('/schedules/8');
    const toggle = screen.getByRole('switch', { name: 'Toggle Daily invoices' }) as HTMLButtonElement;
    expect(toggle.disabled).toBe(true);
    fireEvent.click(toggle);
    expect(mocks.setStatus).not.toHaveBeenCalled();
  });

  it('keeps definitions and history visible while viewers cannot mutate schedule policies or status', () => {
    render(wrap(<ScheduleDetail />));
    expect(screen.getByText('Build invoice')).toBeTruthy();
    expect(screen.getByText('Run history')).toBeTruthy();
    expect(screen.getAllByRole('combobox').every(control => (control as HTMLButtonElement).disabled)).toBe(true);
    for (const name of ['Run now', 'Pause', 'Resume', 'Archive']) {
      expect(screen.queryByRole('button', { name })).toBeNull();
    }
  });

  it('allows operator actions and closes an open run dialog if access is revoked', () => {
    mocks.canOperate = true;
    const view = render(wrap(<ScheduleDetail />));
    fireEvent.click(screen.getByRole('button', { name: 'Pause' }));
    expect(mocks.setStatus).toHaveBeenCalledWith({ id: 8, action: 'pause' }, expect.any(Object));
    fireEvent.click(screen.getByRole('button', { name: 'Run now' }));
    expect(screen.getByRole('dialog')).toBeTruthy();
    mocks.canOperate = false;
    view.rerender(wrap(<ScheduleDetail />));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(mocks.trigger).not.toHaveBeenCalled();
  });
});
