import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

/**
 * Status colour is spent on what needs a decision, not on every state.
 *
 * The surface stays neutral for anything the operator does not have to act on
 * — Completed is the expected outcome, and a wall of green makes the one red
 * row harder to find, not easier. Two states break that rule on purpose:
 * `error`, which is the thing you are scanning for, and `running`, which is
 * the thing you are waiting on.
 *
 * Distinction at close range comes from the dot, which keeps its semantic hue
 * in every variant. So the list reads as "one row needs me" from across the
 * desk, and still tells Skipped from Paused when you lean in.
 */
const statusBadgeVariants = cva(
  "inline-flex items-center gap-1.5 rounded-full font-medium transition-colors",
  {
    variants: {
      status: {
        success: "bg-muted text-muted-foreground",
        warning: "bg-muted text-muted-foreground",
        error: "bg-status-error-bg text-status-error",
        running: "bg-status-running-bg text-status-running",
        rescheduled: "bg-muted text-muted-foreground",
        paused: "bg-muted text-muted-foreground",
        throttled: "bg-muted text-muted-foreground",
        waiting: "bg-muted text-muted-foreground",
        queued: "bg-muted text-muted-foreground",
        skipped: "bg-muted text-muted-foreground",
        default: "bg-muted text-muted-foreground",
      },
      size: {
        sm: "px-2 py-0.5 text-[11px]",
        default: "px-2.5 py-1 text-xs",
        lg: "px-3 py-1.5 text-sm",
      },
    },
    defaultVariants: {
      status: "default",
      size: "default",
    },
  }
);

/** The dot keeps the hue the surface gives up. */
const dotColor: Record<string, string> = {
  success: "bg-status-success",
  warning: "bg-status-warning",
  error: "bg-status-error",
  running: "bg-status-running",
  rescheduled: "bg-status-warning",
  paused: "bg-status-paused",
  throttled: "bg-status-throttled",
  waiting: "bg-muted-foreground/50",
  queued: "bg-muted-foreground/40",
  skipped: "bg-muted-foreground/40",
  default: "bg-muted-foreground/50",
};

const dotSize: Record<string, string> = {
  sm: "h-1.5 w-1.5",
  default: "h-2 w-2",
  lg: "h-2 w-2",
};

interface StatusBadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof statusBadgeVariants> {
  /** Adds the ping animation. The dot itself is always drawn. */
  pulse?: boolean;
}

export function StatusBadge({
  className,
  status,
  size,
  pulse,
  children,
  ...props
}: StatusBadgeProps) {
  const tone = dotColor[status ?? "default"] ?? dotColor.default;
  const dot = dotSize[size ?? "default"] ?? dotSize.default;

  return (
    <span className={cn(statusBadgeVariants({ status, size }), className)} {...props}>
      <span className={cn("relative flex flex-none", dot)}>
        {pulse && (
          <span
            className={cn(
              "absolute inline-flex h-full w-full animate-ping rounded-full opacity-75 motion-reduce:animate-none",
              tone
            )}
          />
        )}
        <span className={cn("relative inline-flex rounded-full", dot, tone)} />
      </span>
      {children}
    </span>
  );
}
