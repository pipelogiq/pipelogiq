import { useQuery } from '@tanstack/react-query';
import { pipelinesApi } from '@/api/client';

/**
 * The summary above the pipelines table.
 *
 * Every figure comes from `totalCount` on a filtered query with `pageSize: 1`,
 * so this needs no new endpoint and no aggregate in the database: the server
 * already counts rows for pagination, and we ask it to count the ones we care
 * about. Four small requests, and they share the cache with nothing else.
 *
 * The window is deliberately fixed at 24 hours and does NOT follow the page's
 * own filters. A summary that moved with the filter would report "0 failed"
 * the moment you filtered to Completed, which is worse than no summary — it
 * reads as good news. "In flight" is the exception: it is a now-state, so it
 * carries no window.
 */

const WINDOW_HOURS = 24;

export interface PipelineSummary {
  /** Pipelines started in the window. */
  started: number;
  /** Running right now, regardless of when they started. */
  inFlight: number;
  /** Failed within the window. */
  failed: number;
  /** Completed within the window. */
  completed: number;
  /**
   * completed / (completed + failed), or null when nothing finished in the
   * window — a rate over zero runs is not 100%, it is unknown, and showing
   * it as 100% would be a lie the operator might act on.
   */
  successRate: number | null;
  windowHours: number;
}

/** Reads only the count, so the page size stays at one row. */
async function countMatching(params: Record<string, unknown>): Promise<number> {
  const result = await pipelinesApi.getAll({ ...params, pageNumber: 1, pageSize: 1 });
  return result.totalCount ?? 0;
}

export function usePipelineSummary() {
  return useQuery<PipelineSummary>({
    queryKey: ['pipeline-summary', WINDOW_HOURS],
    queryFn: async () => {
      // Pinned once per call: four requests sharing one boundary keep the
      // figures consistent with each other.
      const now = new Date();
      const from = new Date(now.getTime() - WINDOW_HOURS * 60 * 60 * 1000).toISOString();
      const to = now.toISOString();
      const inWindow = { pipelineStartFrom: from, pipelineStartTo: to };

      const [started, inFlight, failed, completed] = await Promise.all([
        countMatching(inWindow),
        countMatching({ statuses: ['Running'] }),
        countMatching({ ...inWindow, statuses: ['Failed'] }),
        countMatching({ ...inWindow, statuses: ['Completed'] }),
      ]);

      const finished = completed + failed;

      return {
        started,
        inFlight,
        failed,
        completed,
        successRate: finished > 0 ? (completed / finished) * 100 : null,
        windowHours: WINDOW_HOURS,
      };
    },
    // Matches the table's own cadence; the summary should not be visibly
    // staler than the rows underneath it.
    refetchInterval: 30_000,
    staleTime: 15_000,
  });
}
