import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import type { PipelineExecution } from '@/components/pipelines/PipelineDetailPanel';
import { PipelineTable } from '@/components/pipelines/PipelineTable';
import { PipelineSidePanel } from '@/components/pipelines/PipelineSidePanel';
import { AppSidebar } from '@/components/layout/AppSidebar';
import { TooltipProvider } from '@/components/ui/tooltip';
import ExecutionsRedirect from '@/pages/Executions';

vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ isAdmin: true, canOperate: false }) }));
vi.mock('@/hooks/use-pipelines', () => ({
  usePipeline: () => ({ data: pipeline, isLoading: false }),
  usePausePipeline: () => ({ mutate: vi.fn(), isPending: false }),
  useResumePipeline: () => ({ mutate: vi.fn(), isPending: false }),
  useRerunStage: () => ({ mutate: vi.fn(), isPending: false }),
  useSkipStage: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock('@/api/client', () => ({ systemApi: { getVersion: vi.fn().mockResolvedValue({ version: 'test' }) } }));
vi.mock('@/components/ui/scroll-area', () => ({
  ScrollArea: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

const pipeline: PipelineExecution = {
  id: '17',
  pipelineId: '17',
  pipelineName: 'Invoice processing',
  description: '',
  status: 'paused',
  environment: 'production',
  startedAt: '1 minute ago',
  startedAtExact: '2026-09-17 12:00:00',
  owner: 'Billing',
  tags: [],
  correlationId: '',
  context: [],
  executionNumber: 17,
  traceId: '',
  traceUrl: '',
  logTraceUrl: '',
  stages: [{ id: '31', name: 'Charge payment', status: 'error' }],
  actions: [
    { id: '31', name: 'Charge payment', status: 'error', logs: '', input: {}, output: {}, error: 'Payment failed' },
  ],
};
const clients: QueryClient[] = [];

function Location() {
  const { pathname, search, hash } = useLocation();
  return (
    <output data-testid="location">
      {pathname}
      {search}
      {hash}
    </output>
  );
}

function setup(children: ReactNode, path = '/pipelines') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <MemoryRouter initialEntries={[path]}>
          {children}
          <Location />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
});

describe('pipeline navigation', () => {
  it('provides a native link to the full pipeline page without opening its quick preview', async () => {
    const onSelect = vi.fn();
    setup(<PipelineTable pipelines={[pipeline]} selectedId={null} onSelect={onSelect} />);
    const link = screen.getByRole('link', { name: 'Open pipeline 17: Invoice processing' });
    expect(link.tagName).toBe('A');
    expect(link.getAttribute('href')).toBe('/pipelines/17');
    fireEvent.click(link);
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/pipelines/17'));
    expect(onSelect).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('Invoice processing'));
    expect(onSelect).toHaveBeenCalledWith(pipeline);
  });

  it('opens the current preview tab and expanded stage on the full page', () => {
    setup(<PipelineSidePanel pipelineId={17} onClose={vi.fn()} />);
    const fullPage = screen.getByRole('link', { name: 'View details' });
    expect(fullPage.getAttribute('href')).toBe('/pipelines/17?tab=stages');
    fireEvent.click(screen.getByRole('button', { name: /Charge payment/ }));
    expect(fullPage.getAttribute('href')).toBe('/pipelines/17?tab=stages&stage=31');
    fireEvent.click(screen.getByRole('button', { name: 'Logs' }));
    expect(fullPage.getAttribute('href')).toBe('/pipelines/17?tab=logs');
    fireEvent.click(screen.getByRole('button', { name: 'Context' }));
    expect(fullPage.getAttribute('href')).toBe('/pipelines/17?tab=context');
  });

  it('redirects old execution-history bookmarks to real pipeline history, keeping their filters', async () => {
    setup(
      <Routes>
        <Route path="/executions" element={<ExecutionsRedirect />} />
        <Route path="/pipelines" element={<h1>Pipeline history</h1>} />
      </Routes>,
      '/executions?search=invoice&status=Failed#history',
    );
    expect(await screen.findByRole('heading', { name: 'Pipeline history' })).toBeTruthy();
    expect(screen.getByTestId('location').textContent).toBe('/pipelines?search=invoice&status=Failed#history');
  });

  it('keeps Pipelines discoverable in the collapsed sidebar without the duplicate Executions section', () => {
    setup(<AppSidebar />, '/pipelines/17');
    expect(screen.getByRole('link', { name: 'Pipelines' }).getAttribute('href')).toBe('/pipelines');
    expect(screen.getByRole('link', { name: 'Pipelines' }).getAttribute('aria-current')).toBe('page');
    expect(screen.queryByRole('link', { name: 'Executions' })).toBeNull();
  });
});
