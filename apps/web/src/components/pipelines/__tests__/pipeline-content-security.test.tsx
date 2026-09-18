import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { PipelinePayload } from '@/components/pipelines/PipelinePayload';
import { PipelineContextTable } from '@/components/pipelines/PipelineContextTable';
import { PipelineRunLogs } from '@/components/pipelines/PipelineRunLogs';

afterEach(cleanup);

describe('untrusted pipeline content', () => {
  it('renders markup in payloads, context and logs as text without creating executable elements', () => {
    const markup = '<img src=x onerror="alert(document.cookie)"><script>alert(1)</script><a href="javascript:alert(1)">open</a>';
    const key = '<iframe srcdoc="<script>alert(1)</script>">';
    const view = render(<>
      <PipelinePayload title="Input" value={markup} />
      <PipelineContextTable items={[{ key, value: JSON.stringify(markup), valueType: '<svg onload=alert(1)>' }]} />
      <PipelineRunLogs stages={[{
        id: 31, pipelineId: 17, name: markup, createdAt: '2026-09-17T10:00:00Z',
        logs: [{ message: markup, logLevel: 'ERROR', created: '2026-09-17T10:00:00Z' }],
      }]} onStageChange={vi.fn()} />
    </>);
    expect(screen.getByLabelText('Input payload').textContent).toBe(markup);
    fireEvent.click(screen.getByRole('button', { name: `Expand ${key}` }));
    expect(screen.getByLabelText(`${key} payload`).textContent).toBe(JSON.stringify(markup));
    expect(view.container.querySelectorAll('img, script, iframe, a, svg[onload]')).toHaveLength(0);
    expect(view.container.textContent).toContain(markup);
  });
});
