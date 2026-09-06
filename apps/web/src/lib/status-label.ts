import type { UIStatus } from '@/types/api';

/**
 * The label a person reads for a pipeline or stage status.
 *
 * The wire values are engine states — `success`, `queued` — and showing them
 * raw makes the reader translate. This lives here rather than inside one table
 * so every surface says the same word for the same state.
 */
export function pipelineStatusLabel(status: UIStatus | string): string {
  switch (status) {
    case 'success':
      return 'Completed';
    case 'error':
      return 'Failed';
    case 'queued':
      return 'Not Started';
    case 'waiting':
      return 'Pending';
    case 'rescheduled':
      return 'Rescheduled';
    default:
      return status.charAt(0).toUpperCase() + status.slice(1);
  }
}
