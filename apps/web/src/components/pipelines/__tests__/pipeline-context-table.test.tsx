import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PipelineContextTable } from '@/components/pipelines/PipelineContextTable';
import type { ContextItem } from '@/types/api';

const mocks = vi.hoisted(() => ({ toast: vi.fn(), copy: vi.fn() }));
vi.mock('@/hooks/use-toast', () => ({ toast: mocks.toast }));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.copy.mockResolvedValue(undefined);
  vi.stubGlobal('navigator', { clipboard: { writeText: mocks.copy } });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function rowFor(key: string) {
  const button = screen.getByRole('button', { name: `Expand ${key}` });
  return within(button.closest('tr')!);
}

describe('pipeline context table', () => {
  it('searches keys and original values without case sensitivity and clears no matches', () => {
    const items: ContextItem[] = [
      { key: 'agent:request', value: '"Reserve exactly one item"' },
      { key: 'traceparent', value: '"00-123-456-01"' },
      { key: 'tool:result', value: '{"inventory":10}' },
    ];
    render(<PipelineContextTable items={items} />);
    const search = screen.getByRole('textbox', { name: 'Search context' });
    fireEvent.change(search, { target: { value: '  AGENT:  ' } });
    expect(screen.getByRole('status').textContent).toBe('Showing 1 of 3 entries');
    expect(screen.getByRole('button', { name: 'Expand agent:request' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Expand traceparent' })).toBeNull();
    fireEvent.change(search, { target: { value: 'INVENTORY' } });
    expect(screen.getByRole('button', { name: 'Expand tool:result' })).toBeTruthy();
    fireEvent.change(search, { target: { value: 'not present' } });
    expect(screen.getByText('No context values match your search.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Clear search' }));
    expect(screen.getByRole('status').textContent).toBe('Showing 3 of 3 entries');
  });

  it('shows readable scalar values while distinguishing false, null, zero and empty values', () => {
    render(<PipelineContextTable items={[
      { key: 'message', value: '"Reserve one unit"', valueType: 'System.String, System.Private.CoreLib, Version=8.0.0.0' },
      { key: 'allowed', value: 'false' },
      { key: 'missing', value: 'null' },
      { key: 'count', value: '0' },
      { key: 'empty-text', value: '' },
      { key: 'empty-string', value: '""' },
    ]} />);
    expect(rowFor('message').getByText('Reserve one unit')).toBeTruthy();
    expect(rowFor('message').getByText('String')).toBeTruthy();
    expect(screen.queryByText('"Reserve one unit"')).toBeNull();
    expect(rowFor('allowed').getByText('false')).toBeTruthy();
    expect(rowFor('allowed').getByText('Boolean')).toBeTruthy();
    expect(rowFor('missing').getByText('null')).toBeTruthy();
    expect(rowFor('missing').getByText('Null')).toBeTruthy();
    expect(rowFor('count').getByText('0')).toBeTruthy();
    expect(rowFor('empty-text').getByText('Empty text')).toBeTruthy();
    expect(rowFor('empty-string').getByText('Empty string')).toBeTruthy();
    expect(screen.queryByText('System.String, System.Private.CoreLib, Version=8.0.0.0')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Expand message' }));
    expect(screen.getByLabelText('message payload').textContent).toBe('"Reserve one unit"');
    expect(screen.getByText(/System.String, System.Private.CoreLib, Version=8.0.0.0/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Collapse message' }).getAttribute('aria-expanded')).toBe('true');
  });

  it('preserves large number lexemes, duplicate keys and full structured values when expanding and copying', async () => {
    const original = '{"id":9007199254740993,"id":1e+40,"text":"' + 'long value '.repeat(200) + 'END"}';
    render(<PipelineContextTable items={[{ key: 'result', value: original }]} />);
    expect(rowFor('result').getByText('Object')).toBeTruthy();
    expect(rowFor('result').getByText(original)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Copy result value' }));
    await waitFor(() => expect(mocks.copy).toHaveBeenCalledWith(original));
    fireEvent.click(screen.getByRole('button', { name: 'Expand result' }));
    const expanded = screen.getByLabelText('result payload').textContent ?? '';
    expect(expanded).toContain('9007199254740993');
    expect(expanded).toContain('1e+40');
    expect(expanded.match(/"id"/g)).toHaveLength(2);
    expect(expanded).toContain('END');
    fireEvent.click(screen.getByRole('button', { name: 'Raw' }));
    expect(screen.getByLabelText('result payload').textContent).toBe(original);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse result' }));
    expect(screen.queryByLabelText('result payload')).toBeNull();
  });

  it('keeps redacted and deleted values intact and copies the original encoded scalar', async () => {
    render(<PipelineContextTable items={[
      { key: 'token', value: '"[REDACTED]"', isSensitive: true },
      { key: 'old-value', value: '', isDeleted: true },
    ]} />);
    expect(rowFor('token').getByText('Sensitive')).toBeTruthy();
    expect(rowFor('token').getByText('[REDACTED]')).toBeTruthy();
    expect(rowFor('old-value').getByText('Deleted')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Copy token value' }));
    await waitFor(() => expect(mocks.copy).toHaveBeenCalledWith('"[REDACTED]"'));
    expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Context value copied' }));
    fireEvent.click(screen.getByRole('button', { name: 'Expand token' }));
    expect(screen.getByLabelText('token payload').textContent).toBe('"[REDACTED]"');
  });

  it('reports clipboard failure and keeps empty values copyable', async () => {
    mocks.copy.mockRejectedValue(new Error('clipboard unavailable'));
    render(<PipelineContextTable items={[{ key: 'empty', value: '' }]} />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy empty value' }));
    await waitFor(() => expect(mocks.copy).toHaveBeenCalledWith(''));
    await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith(expect.objectContaining({ title: 'Could not copy context value', variant: 'destructive' })));
  });

  it('distinguishes an empty context from a search with no matches', () => {
    render(<PipelineContextTable items={[]} />);
    expect(screen.getByText('No context recorded for this pipeline.')).toBeTruthy();
    expect(screen.queryByText('No context values match your search.')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Clear search' })).toBeNull();
  });
});
