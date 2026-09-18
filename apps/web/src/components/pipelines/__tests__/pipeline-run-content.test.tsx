import { useState } from 'react';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PipelinePayload } from '@/components/pipelines/PipelinePayload';
import { PipelineRunLogs } from '@/components/pipelines/PipelineRunLogs';
import type { StageResponse } from '@/types/api';
import { selectOption } from '@/test/select';

const mocks = vi.hoisted(() => ({ toast: vi.fn(), copy: vi.fn() }));
vi.mock('@/hooks/use-toast', () => ({ toast: mocks.toast }));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.copy.mockResolvedValue(undefined);
  vi.stubGlobal('navigator', { clipboard: { writeText: mocks.copy } });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe('pipeline payloads', () => {
  it.each(['null', 'false', '0', '[]', '""'])('renders the literal JSON payload %s', (value) => {
    render(<PipelinePayload title="Input" value={value} />);
    expect(screen.getByLabelText('Input payload').textContent).toBe(value);
    expect(screen.getByRole('button', { name: 'Formatted' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.queryByText('No payload recorded.')).toBeNull();
  });

  it('distinguishes a missing payload from an empty string', () => {
    const view = render(<PipelinePayload title="Input" />);
    expect(screen.getByText('No payload recorded.')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Copy Input' }) as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<PipelinePayload title="Input" value="" />);
    expect(screen.getByText('Empty text payload.')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Copy Input' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('formats without changing numbers, duplicate keys, escaped text or redaction', async () => {
    const raw = String.raw`{"id":9007199254740993,"id":1e+40,"text":"line\n\"{}","secret":"[REDACTED]","empty":{},"list":[false,null,0]}`;
    render(<PipelinePayload title="Output" value={raw} />);
    const formatted = screen.getByLabelText('Output payload').textContent ?? '';
    expect(formatted).toContain('9007199254740993');
    expect(formatted).toContain('1e+40');
    expect(formatted.match(/"id"/g)).toHaveLength(2);
    expect(formatted).toContain('"secret": "[REDACTED]"');
    expect(JSON.parse(formatted)).toEqual(JSON.parse(raw));
    fireEvent.click(screen.getByRole('button', { name: 'Raw' }));
    expect(screen.getByLabelText('Output payload').textContent).toBe(raw);
    fireEvent.click(screen.getByRole('button', { name: 'Copy Output' }));
    await waitFor(() => expect(mocks.copy).toHaveBeenCalledWith(raw));
    expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Output copied' }));
  });

  it('shows the full plain-text value and reports clipboard failure', async () => {
    const raw = `  <script>literal text</script>\n${'long payload '.repeat(3000)}END`;
    mocks.copy.mockRejectedValue(new Error('blocked'));
    const view = render(<PipelinePayload title="Input" value={raw} />);
    expect(screen.getByLabelText('Input payload').textContent).toBe(raw);
    expect(view.container.querySelector('script')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Raw' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Copy Input' }));
    await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ variant: 'destructive' })));
  });
});

const stages: StageResponse[] = [
  {
    id: 10, pipelineId: 7, name: 'Validate', createdAt: '2026-09-17T10:00:00Z',
    logs: [
      { id: 5, message: '  warning\n  details [REDACTED]', logLevel: 'warn', created: '2026-09-17T10:00:03Z' },
      { id: 3, message: 'first tied event', logLevel: 'INFO', created: '2026-09-17T10:00:02Z' },
    ],
  },
  {
    id: 20, pipelineId: 7, name: 'Send', createdAt: '2026-09-17T10:00:00Z',
    logs: [
      { id: 1, message: 'earliest event', logLevel: 'INFO', created: '2026-09-17T10:00:01Z' },
      { id: 4, message: 'second tied event', logLevel: 'ERROR', created: '2026-09-17T10:00:02Z' },
    ],
  },
];

function Logs() {
  const [stageId, setStageId] = useState<number>();
  return <PipelineRunLogs stages={stages} stageId={stageId} onStageChange={setStageId} />;
}

function messages() {
  return within(screen.getByRole('table', { name: 'Pipeline log entries' })).getAllByRole('row').slice(1).map((row) => row.querySelector('pre')?.textContent);
}

describe('pipeline run logs', () => {
  it('sorts retained logs chronologically with stable ties and exact message whitespace', () => {
    render(<Logs />);
    expect(messages()).toEqual(['earliest event', 'first tied event', 'second tied event', '  warning\n  details [REDACTED]']);
    expect(screen.getByRole('status').textContent).toBe('Showing 4 of 4 retained log entries');
  });

  it('combines filters, copies only matching entries, and clears filters', async () => {
    render(<Logs />);
    selectOption(screen.getByLabelText('Filter logs by stage'), '1. Validate');
    selectOption(screen.getByLabelText('Filter logs by severity'), 'WARN');
    fireEvent.change(screen.getByRole('textbox', { name: 'Search logs' }), { target: { value: 'WARNING' } });
    expect(messages()).toEqual(['  warning\n  details [REDACTED]']);
    fireEvent.click(screen.getByRole('button', { name: 'Copy filtered logs' }));
    await waitFor(() => expect(mocks.copy).toHaveBeenCalledOnce());
    expect(mocks.copy.mock.calls[0][0]).toBe('[2026-09-17T10:00:03Z] [WARN] [Validate #10]   warning\n  details [REDACTED]');
    expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Logs copied' }));
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(messages()).toHaveLength(4);
    expect(screen.getByLabelText('Filter logs by stage').textContent).toBe('All stages');
  });

  it('distinguishes no matching logs from an empty retained history', () => {
    const view = render(<Logs />);
    fireEvent.change(screen.getByRole('textbox', { name: 'Search logs' }), { target: { value: 'no such entry' } });
    expect(screen.getByText('No logs match these filters.')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Copy filtered logs' }) as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<PipelineRunLogs stages={[{ ...stages[0], logs: [] }]} onStageChange={vi.fn()} />);
    expect(screen.getByText('No logs have been recorded for this pipeline.')).toBeTruthy();
  });

  it('retains an unavailable deep-linked stage without showing another stage', () => {
    const onStageChange = vi.fn();
    render(<PipelineRunLogs stages={stages} stageId={999} onStageChange={onStageChange} />);
    expect(screen.getByLabelText('Filter logs by stage').textContent).toBe('Stage #999 (unavailable)');
    expect(screen.getByText('No logs match these filters.')).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();
    expect(onStageChange).not.toHaveBeenCalled();
  });

  it('keeps every log and reports clipboard errors', async () => {
    mocks.copy.mockRejectedValue(new Error('blocked'));
    const longMessage = 'retained message '.repeat(3000);
    render(<PipelineRunLogs stages={[{ ...stages[0], logs: [{ message: longMessage, created: '2026-09-17T10:00:00Z' }] }]} onStageChange={vi.fn()} />);
    expect(messages()).toEqual([longMessage]);
    selectOption(screen.getByLabelText('Filter logs by severity'), 'Unspecified');
    fireEvent.click(screen.getByRole('button', { name: 'Copy filtered logs' }));
    await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Could not copy logs', variant: 'destructive' })));
  });
});
