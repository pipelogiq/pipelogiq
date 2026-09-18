import { useState } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { MultiSelect } from '@/components/ui/multi-select';
import '@/test/select';

function Field() {
  const [values, setValues] = useState(['saved-target']);
  return <>
    <MultiSelect label="Pipelines" values={values} onChange={setValues} options={[
      { value: 'orders', label: 'Order processing' },
      { value: 'invoices', label: 'Invoice processing' },
    ]} />
    <output data-testid="selected">{JSON.stringify(values)}</output>
  </>;
}

afterEach(cleanup);

describe('multi-select targets', () => {
  it('preserves saved targets outside current choices and other selections when searching and toggling', () => {
    render(<Field />);
    fireEvent.click(screen.getByRole('button', { name: 'Pipelines' }));
    expect(screen.getByRole('checkbox', { name: 'saved-target' }).getAttribute('aria-checked')).toBe('true');
    fireEvent.change(screen.getByRole('textbox', { name: 'Search pipelines' }), { target: { value: 'order' } });
    fireEvent.click(screen.getByRole('checkbox', { name: 'Order processing' }));
    expect(screen.getByTestId('selected').textContent).toBe('["saved-target","orders"]');
    fireEvent.change(screen.getByRole('textbox', { name: 'Search pipelines' }), { target: { value: 'invoice' } });
    fireEvent.click(screen.getByRole('checkbox', { name: 'Invoice processing' }));
    expect(screen.getByTestId('selected').textContent).toBe('["saved-target","orders","invoices"]');
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
    expect(screen.getByTestId('selected').textContent).toBe('[]');
    expect(screen.getByText('All included')).toBeTruthy();
  });
});
