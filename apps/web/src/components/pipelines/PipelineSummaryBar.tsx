import { cn } from "@/lib/utils";
import { usePipelineSummary } from "@/hooks/use-pipeline-summary";

/**
 * The summary strip above the pipelines table.
 *
 * Composed as one object with hairline dividers rather than four separate
 * cards: this sits directly under the page header and above a filter bar, and
 * four bordered tiles there would stack chrome on chrome before the reader
 * reaches the data.
 *
 * The window is fixed at 24 hours and does not follow the table's filters —
 * see use-pipeline-summary for why.
 */
export function PipelineSummaryBar() {
  const { data, isError } = usePipelineSummary();

  // A summary that cannot load is not worth a broken state above the table;
  // the table below is the page's actual job.
  if (isError) return null;

  const successRate =
    data?.successRate == null ? "—" : `${data.successRate.toFixed(1)}%`;

  // Colour only when something is actually wrong, so a healthy day stays
  // monochrome and the one red figure is what the eye lands on.
  const hasFailures = (data?.failed ?? 0) > 0;

  // The window rides in the label instead of a third line: three of the four
  // cells used to repeat "last 24 hours" under the figure, which is exactly
  // the small print nobody reads. In flight has no window — it is a reading
  // of right now — so it simply carries none.
  const items = [
    { label: "Started", window: "24h", value: data?.started },
    { label: "In flight", window: null, value: data?.inFlight },
    { label: "Failed", window: "24h", value: data?.failed, alarming: hasFailures },
    { label: "Success rate", window: "24h", value: successRate },
  ];

  return (
    // One column stacked on narrow screens so the dividers stay meaningful:
    // divide-y across a two-column grid would rule between cells that sit
    // side by side.
    <div className="grid grid-cols-2 divide-x divide-y divide-border rounded-xl border border-border bg-card sm:grid-cols-4 sm:divide-y-0">
      {items.map((item) => (
        <div key={item.label} className="px-4 py-2.5">
          <p className="flex items-baseline gap-1.5 text-[13px] leading-none text-muted-foreground">
            {item.label}
            {item.window && <span className="text-[11px]">{item.window}</span>}
          </p>
          <p
            className={cn(
              "mt-1.5 text-[22px] font-semibold leading-none tracking-[-0.02em] tabular-nums",
              item.alarming ? "text-status-error" : "text-foreground"
            )}
          >
            {/* An em dash holds the line's height while the counts load, so
                the strip does not jump when they arrive. */}
            {item.value ?? "\u2014"}
          </p>
        </div>
      ))}
    </div>
  );
}
