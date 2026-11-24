import { cn } from "@/lib/utils";
import { LucideIcon } from "lucide-react";

const accentStyles = {
  success: {
    rail: "before:bg-status-success",
    icon: "border-status-success/25 bg-status-success-bg text-status-success",
  },
  warning: {
    rail: "before:bg-status-warning",
    icon: "border-status-warning/25 bg-status-warning-bg text-status-warning",
  },
  error: {
    rail: "before:bg-status-error",
    icon: "border-status-error/25 bg-status-error-bg text-status-error",
  },
  neutral: {
    rail: "before:bg-muted-foreground/60",
    icon: "border-border-strong bg-muted text-muted-foreground",
  },
};

interface KpiCardProps {
  title: string;
  value: string | number;
  subtitle?: string;
  icon?: LucideIcon;
  trend?: {
    value: number;
    direction: "up" | "down";
    isPositive?: boolean;
  };
  /**
   * Highlights a non-zero figure that indicates a problem.
   */
  tone?: "default" | "critical";
  accent?: keyof typeof accentStyles;
  className?: string;
}

export function KpiCard({
  title,
  value,
  subtitle,
  icon: Icon,
  trend,
  tone = "default",
  accent,
  className,
}: KpiCardProps) {
  const hasNonzeroValue = value !== 0 && value !== "0" && value !== "—";
  const isAlarming = tone === "critical" && hasNonzeroValue;
  const decoration = accent && accentStyles[hasNonzeroValue ? accent : "neutral"];
  const alarmText = accent === "warning" ? "text-status-warning" : "text-status-error";

  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-xl border border-border bg-card p-6 transition-colors hover:border-border-strong",
        decoration && "kpi-card-accent rounded-2xl border-foreground/80 shadow-card dark:border-border-strong before:pointer-events-none before:absolute before:inset-y-4 before:left-0 before:w-1 before:rounded-r-full",
        decoration?.rail,
        className
      )}
    >
      <div className={cn("flex items-start justify-between", decoration && "kpi-card-body")}>
        <div className={decoration ? "kpi-card-copy" : undefined}>
          {/* Tier 1: the label, in foreground — it names the figure, so it is
              not secondary information. */}
          <p className={cn("metric-label", decoration && "text-xs font-bold uppercase tracking-wide")}>
            {title}
          </p>
          <div className={cn("flex items-baseline gap-2.5", decoration && "kpi-card-figure")}>
            <p className={cn("metric-value", decoration && "text-[34px] font-semibold", isAlarming && alarmText)}>
              {value}
            </p>
            {trend && (
              <span
                className={cn(
                  "inline-flex items-center text-xs font-medium tabular-nums",
                  trend.isPositive ? "text-status-success" : "text-status-error"
                )}
              >
                {trend.direction === "up" ? "↑" : "↓"} {Math.abs(trend.value)}%
              </span>
            )}
          </div>
          {/* Tier 3: what the figure is out of, or over what window. */}
          {subtitle && <p className="metric-context">{subtitle}</p>}
        </div>
        {Icon && (
          <div
            className={cn(
              "rounded-lg p-2.5",
              decoration ? cn("kpi-card-icon shrink-0 rounded-xl border", decoration.icon) : isAlarming ? "bg-status-error-bg" : "bg-muted"
            )}
          >
            <Icon
              className={cn(
                "h-[18px] w-[18px]",
                !decoration && (isAlarming ? "text-status-error" : "text-muted-foreground")
              )}
              strokeWidth={1.75}
            />
          </div>
        )}
      </div>
    </div>
  );
}
