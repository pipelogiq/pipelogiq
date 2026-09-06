import { cn } from '@/lib/utils';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import type { ScheduleRunOutcome } from '@/types/schedules';

const outcomeClass: Record<ScheduleRunOutcome, string> = {
  Created: 'bg-status-success',
  Failed: 'bg-destructive',
  Skipped: 'bg-muted-foreground/40',
};

interface RunOutcomeStripProps {
  /** Newest first, as the API returns it. */
  outcomes: ScheduleRunOutcome[];
  className?: string;
}

/**
 * RunOutcomeStrip shows the last runs oldest-to-newest, so the rightmost bar is
 * the most recent — the direction people read a timeline in.
 */
export function RunOutcomeStrip({ outcomes, className }: RunOutcomeStripProps) {
  if (outcomes.length === 0) {
    return <span className="text-xs text-muted-foreground">no runs yet</span>;
  }

  const oldestFirst = [...outcomes].reverse();
  return (
    <div className={cn('flex items-center gap-0.5', className)} aria-label="recent run outcomes">
      {oldestFirst.map((outcome, index) => (
        <Tooltip key={`${outcome}-${index}`}>
          <TooltipTrigger asChild>
            <span
              className={cn('h-4 w-1.5 rounded-sm', outcomeClass[outcome])}
              data-outcome={outcome}
            />
          </TooltipTrigger>
          <TooltipContent>{outcome}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}
