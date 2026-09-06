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

  const items = [
    { label: "Started", value: data?.started, context: "last 24 hours" },
    { label: "In flight", value: data?.inFlight, context: "running now" },
    {
      label: "Failed",
      value: data?.failed,
      context: "last 24 hours",
      alarming: hasFailures,
    },
    { label: "Success rate", value: successRate, context: "last 24 hours" },
  ];

  return (
    // One column stacked on narrow screens so the dividers stay meaningful:
    // divide-y across a two-column grid would rule between cells that sit
    // side by side.
    <div className="grid grid-cols-1 divide-y divide-border rounded-xl border border-border bg-card sm:grid-cols-4 sm:divide-x sm:divide-y-0">
      {items.map((item) => (
        <div key={item.label} className="px-5 py-4">
          <p className="metric-label text-sm">{item.label}</p>
          <p
            className={cn(
              "metric-value text-[32px]",
              item.alarming && "text-status-error"
            )}
          >
            {/* An em dash holds the line's height while the counts load, so
                the strip does not jump when they arrive. */}
            {item.value ?? "—"}
          </p>
          <p className="metric-context">{item.context}</p>
        </div>
      ))}
    </div>
  );
}
