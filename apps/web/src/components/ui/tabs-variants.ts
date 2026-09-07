import { cva } from "class-variance-authority";

/**
 * Two tab treatments, chosen by role — not by taste.
 *
 * `underline` navigates between sections of a page: Stages / Logs / Context.
 * The sections hold different things, the rail is a piece of page structure,
 * and the tab bar can carry the page's own bottom border. This is the default
 * because it is the common case.
 *
 * `segment` switches the *view* of one dataset: Donut / Trend, Table / Chart.
 * The enclosing track says "these are alternatives of the same thing", which
 * is exactly the wrong message for page sections.
 *
 * Layout stays with the caller — margins, overflow, how the strip sits in its
 * container. Only the treatment lives here, so every strip reads as one
 * pattern wherever it is used.
 *
 * Where a section has a meaningful count, put it in the label — `Stages (12)`.
 * It sits where the eye already is and saves a line of chrome elsewhere. Skip
 * it where there is no honest number to show; a padded-out `(0)` is noise.
 */
export const tabsListVariants = cva("items-center text-muted-foreground", {
  variants: {
    variant: {
      underline: "flex h-auto justify-start gap-6 border-b border-border bg-transparent p-0",
      segment: "inline-flex h-10 justify-center rounded-md bg-muted p-1",
    },
  },
  defaultVariants: { variant: "underline" },
});

/**
 * Exported so the one hand-rolled tab strip that cannot use Radix — the
 * pipeline side panel drives its tabs from URL state — takes its classes from
 * here rather than keeping a second copy that drifts.
 */
export const tabsTriggerVariants = cva(
  "inline-flex items-center justify-center whitespace-nowrap text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        // -mb-px lets the active rail sit *on* the list's border rather than
        // under it, so the two never render as a 3px stack.
        underline:
          "-mb-px rounded-none border-b-2 border-transparent px-0 py-2.5 hover:text-foreground data-[state=active]:border-primary data-[state=active]:text-foreground",
        segment:
          "rounded-sm px-3 py-1.5 data-[state=active]:bg-background data-[state=active]:text-foreground data-[state=active]:shadow-sm",
      },
    },
    defaultVariants: { variant: "underline" },
  },
);
