import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { PipelineSearchBar } from '@/components/pipelines/PipelineSearchBar';

afterEach(cleanup);

describe('pipeline search query state', () => {
  it('parses the initial URL query before applying it', () => {
    const onFiltersChange = vi.fn();
    render(<PipelineSearchBar initialQuery="invoice orderId:123 tenant:east" totalResults={3} onFiltersChange={onFiltersChange} />);

    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('invoice orderId:123 tenant:east');
    expect(onFiltersChange).toHaveBeenCalledOnce();
    expect(onFiltersChange).toHaveBeenLastCalledWith(expect.objectContaining({
      query: 'invoice',
      status: 'all',
      contextFilters: [{ key: 'orderId', value: '123' }, { key: 'tenant', value: 'east' }],
    }));
  });

  it('keeps a local draft across parent renders and applies it only on submit', () => {
    const onFiltersChange = vi.fn();
    const { rerender } = render(<PipelineSearchBar initialQuery="original" totalResults={3} onFiltersChange={onFiltersChange} />);
    onFiltersChange.mockClear();
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'draft orderId:456' } });
    rerender(<PipelineSearchBar initialQuery="original" totalResults={8} onFiltersChange={onFiltersChange} />);

    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('draft orderId:456');
    expect(onFiltersChange).not.toHaveBeenCalled();
    fireEvent.keyDown(screen.getByRole('searchbox'), { key: 'Enter' });
    expect(onFiltersChange).toHaveBeenLastCalledWith(expect.objectContaining({
      query: 'draft', contextFilters: [{ key: 'orderId', value: '456' }],
    }));
  });

  it('replaces and clears URL search terms while retaining selected status', () => {
    const onFiltersChange = vi.fn();
    const { rerender } = render(<PipelineSearchBar initialQuery="old orderId:123" totalResults={3} onFiltersChange={onFiltersChange} />);
    fireEvent.click(screen.getByRole('button', { name: 'Status' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Failed' }));
    onFiltersChange.mockClear();

    rerender(<PipelineSearchBar initialQuery="new customer:456" totalResults={3} onFiltersChange={onFiltersChange} />);
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('new customer:456');
    expect(onFiltersChange).toHaveBeenCalledOnce();
    expect(onFiltersChange).toHaveBeenLastCalledWith(expect.objectContaining({
      query: 'new', status: 'error', contextFilters: [{ key: 'customer', value: '456' }],
    }));

    onFiltersChange.mockClear();
    rerender(<PipelineSearchBar initialQuery="" totalResults={3} onFiltersChange={onFiltersChange} />);
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('');
    expect(onFiltersChange).toHaveBeenCalledOnce();
    expect(onFiltersChange).toHaveBeenLastCalledWith(expect.objectContaining({ query: '', status: 'error', contextFilters: [] }));
    expect(screen.queryByText('customer:456')).toBeNull();
    expect((screen.getByRole('checkbox', { name: 'Failed' })).getAttribute('aria-checked')).toBe('true');
  });
});
