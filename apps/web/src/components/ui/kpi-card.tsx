import { cn } from "@/lib/utils";
import { LucideIcon } from "lucide-react";

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
   * "critical" marks a figure that means something is wrong — failures, dead
   * workers. It only colours the tile when the value is actually non-zero, so
   * a healthy dashboard stays monochrome and the one red number is the thing
   * your eye lands on.
   */
  tone?: "default" | "critical";
  className?: string;
}

export function KpiCard({
  title,
  value,
  subtitle,
  icon: Icon,
  trend,
  tone = "default",
  className,
}: KpiCardProps) {
  const isAlarming =
    tone === "critical" && value !== 0 && value !== "0" && value !== "—";

  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-xl border border-border bg-card p-6 transition-colors hover:border-border-strong",
        className
      )}
    >
      <div className="flex items-start justify-between">
        <div>
          {/* Tier 1: the label, in foreground — it names the figure, so it is
              not secondary information. */}
          <p className="metric-label">{title}</p>
          <div className="flex items-baseline gap-2.5">
            {/* Tier 2: the figure. The jump from 15px to 40px is what reads as
                confidence; a dashboard set entirely at 24px does not. */}
            <p className={cn("metric-value", isAlarming && "text-status-error")}>
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
              isAlarming ? "bg-status-error-bg" : "bg-muted"
            )}
          >
            <Icon
              className={cn(
                "h-[18px] w-[18px]",
                isAlarming ? "text-status-error" : "text-muted-foreground"
              )}
              strokeWidth={1.75}
            />
          </div>
        )}
      </div>
    </div>
  );
}
