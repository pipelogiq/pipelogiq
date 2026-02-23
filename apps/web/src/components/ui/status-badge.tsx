import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const statusBadgeVariants = cva(
  "inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium transition-colors",
  {
    variants: {
      status: {
        success: "bg-status-success-bg text-status-success",
        warning: "bg-status-warning-bg text-status-warning",
        error: "bg-status-error-bg text-status-error",
        running: "bg-status-running-bg text-status-running",
        paused: "bg-status-paused-bg text-status-paused",
        throttled: "bg-status-throttled-bg text-status-throttled",
        waiting: "bg-muted text-muted-foreground",
        queued: "bg-slate-100 text-slate-400",
        skipped: "bg-violet-50 text-violet-500",
        default: "bg-muted text-muted-foreground",
      },
      size: {
        sm: "px-2 py-0.5 text-[10px]",
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

interface StatusBadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof statusBadgeVariants> {
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
  return (
    <span
      className={cn(statusBadgeVariants({ status, size }), className)}
      {...props}
    >
      {pulse && (
        <span className="relative flex h-2 w-2">
          <span
            className={cn(
              "animate-ping absolute inline-flex h-full w-full rounded-full opacity-75",
              status === "running" && "bg-status-running",
              status === "success" && "bg-status-success",
              status === "error" && "bg-status-error",
              status === "warning" && "bg-status-warning",
              status === "paused" && "bg-status-paused",
              status === "throttled" && "bg-status-throttled"
            )}
          />
          <span
            className={cn(
              "relative inline-flex rounded-full h-2 w-2",
              status === "running" && "bg-status-running",
              status === "success" && "bg-status-success",
              status === "error" && "bg-status-error",
              status === "warning" && "bg-status-warning",
              status === "paused" && "bg-status-paused",
              status === "throttled" && "bg-status-throttled"
            )}
          />
        </span>
      )}
      {children}
    </span>
  );
}
