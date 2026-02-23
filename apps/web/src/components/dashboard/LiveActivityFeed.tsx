import { cn } from "@/lib/utils";
import { StatusBadge } from "@/components/ui/status-badge";
import {
  CheckCircle2,
  XCircle,
  Clock,
  PlayCircle,
  Timer,
  AlertCircle,
} from "lucide-react";

interface ActivityItem {
  id: string;
  pipeline: string;
  step: string;
  status: "success" | "error" | "running" | "waiting" | "throttled";
  timestamp: string;
  duration?: string;
  message?: string;
}

const activities: ActivityItem[] = [
  {
    id: "1",
    pipeline: "order-processing",
    step: "validate-payment",
    status: "success",
    timestamp: "2s ago",
    duration: "124ms",
  },
  {
    id: "2",
    pipeline: "user-onboarding",
    step: "send-welcome-email",
    status: "throttled",
    timestamp: "5s ago",
    message: "Rate limited: waiting 3.2s",
  },
  {
    id: "3",
    pipeline: "betting-settlement",
    step: "calculate-odds",
    status: "running",
    timestamp: "8s ago",
  },
  {
    id: "4",
    pipeline: "kyc-verification",
    step: "document-scan",
    status: "error",
    timestamp: "12s ago",
    message: "OCR service timeout",
  },
  {
    id: "5",
    pipeline: "payment-webhook",
    step: "notify-merchant",
    status: "success",
    timestamp: "15s ago",
    duration: "89ms",
  },
  {
    id: "6",
    pipeline: "fraud-detection",
    step: "ml-scoring",
    status: "running",
    timestamp: "18s ago",
  },
  {
    id: "7",
    pipeline: "order-processing",
    step: "inventory-check",
    status: "success",
    timestamp: "22s ago",
    duration: "45ms",
  },
  {
    id: "8",
    pipeline: "user-onboarding",
    step: "create-profile",
    status: "waiting",
    timestamp: "25s ago",
    message: "Waiting for email confirmation",
  },
];

const statusIcons = {
  success: CheckCircle2,
  error: XCircle,
  running: PlayCircle,
  waiting: Clock,
  throttled: Timer,
};

const statusColors = {
  success: "text-status-success",
  error: "text-status-error",
  running: "text-status-running",
  waiting: "text-muted-foreground",
  throttled: "text-status-throttled",
};

export function LiveActivityFeed() {
  return (
    <div className="rounded-xl border border-border bg-card">
      <div className="flex items-center justify-between border-b border-border px-5 py-4">
        <div className="flex items-center gap-2">
          <div className="relative flex h-2 w-2">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-status-success opacity-75" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-status-success" />
          </div>
          <h3 className="font-semibold text-foreground">Live Activity</h3>
        </div>
        <span className="text-xs text-muted-foreground">Last 30 seconds</span>
      </div>

      <div className="max-h-[400px] overflow-y-auto scrollbar-thin">
        {activities.map((activity, index) => {
          const Icon = statusIcons[activity.status];
          
          return (
            <div
              key={activity.id}
              className={cn(
                "flex items-start gap-3 px-5 py-3.5 transition-colors hover:bg-muted/50",
                "animate-fade-in-up",
                index !== activities.length - 1 && "border-b border-border"
              )}
              style={{ animationDelay: `${index * 50}ms` }}
            >
              <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", statusColors[activity.status])} />
              
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-foreground">
                    {activity.pipeline}
                  </span>
                  <span className="text-muted-foreground">→</span>
                  <span className="text-sm text-muted-foreground">
                    {activity.step}
                  </span>
                </div>
                
                {activity.message && (
                  <p className="mt-0.5 text-sm text-muted-foreground">
                    {activity.message}
                  </p>
                )}
              </div>

              <div className="flex flex-col items-end gap-1">
                <span className="text-xs text-muted-foreground">
                  {activity.timestamp}
                </span>
                {activity.duration && (
                  <span className="text-xs font-mono text-muted-foreground">
                    {activity.duration}
                  </span>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
