import { useState, useMemo } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/ui/status-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Search, Copy, Check, ExternalLink, ArrowRight, Loader2 } from "lucide-react";
import { useObservabilityTraces, useObservabilityConfig, buildTraceLink } from "@/hooks/use-observability";
import type { TimeRange, TraceEntry, OtelConfig } from "@/types/observability";
import { formatDistanceToNow } from "date-fns";
import { cn } from "@/lib/utils";

interface TracesTabProps {
  timeRange: TimeRange;
}

export function TracesTab({ timeRange }: TracesTabProps) {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [copiedId, setCopiedId] = useState<string | null>(null);

  const { data: traces, isLoading } = useObservabilityTraces({
    search: search || undefined,
    status: statusFilter !== "all" ? statusFilter : undefined,
    timeRange,
  });

  const { data: config } = useObservabilityConfig();

  const otelConfig = useMemo(() => {
    const otel = config?.integrations.find(i => i.type === "opentelemetry");
    return (otel?.config || {}) as Partial<OtelConfig>;
  }, [config]);

  const traceLinkTemplate = otelConfig.traceLinkTemplate;

  const handleCopy = (traceId: string) => {
    navigator.clipboard.writeText(traceId);
    setCopiedId(traceId);
    setTimeout(() => setCopiedId(null), 2000);
  };

  return (
    <TooltipProvider>
      <div className="space-y-4">
        {/* Filters */}
        <div className="flex items-center gap-3">
          <div className="relative flex-1 max-w-md">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="Search by traceId, executionId, or pipeline name..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9"
            />
          </div>
          <Select value={statusFilter} onValueChange={setStatusFilter}>
            <SelectTrigger className="w-36">
              <SelectValue placeholder="Status" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All statuses</SelectItem>
              <SelectItem value="success">Success</SelectItem>
              <SelectItem value="error">Error</SelectItem>
              <SelectItem value="running">Running</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Table */}
        <div className="rounded-xl border border-border bg-card overflow-hidden">
          {isLoading ? (
            <div className="flex items-center justify-center py-16">
              <Loader2 className="h-6 w-6 animate-spin text-primary" />
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[220px]">Trace ID</TableHead>
                  <TableHead>Pipeline</TableHead>
                  <TableHead className="w-[100px]">Status</TableHead>
                  <TableHead className="w-[90px] text-right">Duration</TableHead>
                  <TableHead className="w-[70px] text-right">Spans</TableHead>
                  <TableHead className="w-[120px] text-right">Time</TableHead>
                  <TableHead className="w-[100px] text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {traces && traces.length > 0 ? (
                  traces.map((trace) => (
                    <TraceRow
                      key={trace.traceId}
                      trace={trace}
                      traceLinkTemplate={traceLinkTemplate}
                      copiedId={copiedId}
                      onCopy={handleCopy}
                    />
                  ))
                ) : (
                  <TableRow>
                    <TableCell colSpan={7} className="text-center py-12 text-muted-foreground">
                      {search || statusFilter !== "all" ? "No traces match the current filters" : "No traces available"}
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </div>
      </div>
    </TooltipProvider>
  );
}

// ── Trace row ──

interface TraceRowProps {
  trace: TraceEntry;
  traceLinkTemplate?: string;
  copiedId: string | null;
  onCopy: (id: string) => void;
}

function TraceRow({ trace, traceLinkTemplate, copiedId, onCopy }: TraceRowProps) {
  const traceLink = buildTraceLink(traceLinkTemplate, trace.traceId);

  return (
    <TableRow className="hover:bg-muted/50">
      <TableCell>
        <div className="flex items-center gap-1.5">
          <span className="font-mono text-xs truncate max-w-[180px]">{trace.traceId}</span>
          <button
            onClick={() => onCopy(trace.traceId)}
            className="p-0.5 rounded hover:bg-muted transition-colors shrink-0"
          >
            {copiedId === trace.traceId ? (
              <Check className="h-3.5 w-3.5 text-green-600" />
            ) : (
              <Copy className="h-3.5 w-3.5 text-muted-foreground" />
            )}
          </button>
        </div>
      </TableCell>
      <TableCell>
        <span className="text-sm">{trace.pipelineName}</span>
        {trace.executionId && (
          <span className="text-xs text-muted-foreground ml-1.5">#{trace.executionId}</span>
        )}
      </TableCell>
      <TableCell>
        <StatusBadge
          status={trace.status === "success" ? "success" : trace.status === "error" ? "error" : "running"}
          size="sm"
          pulse={trace.status === "running"}
        >
          {trace.status}
        </StatusBadge>
      </TableCell>
      <TableCell className="text-right font-mono text-sm">
        {formatDuration(trace.durationMs)}
      </TableCell>
      <TableCell className="text-right text-sm">
        {trace.spansCount}
      </TableCell>
      <TableCell className="text-right text-sm text-muted-foreground">
        {formatDistanceToNow(new Date(trace.timestamp), { addSuffix: true })}
      </TableCell>
      <TableCell className="text-right">
        <div className="flex items-center justify-end gap-1">
          <Tooltip>
            <TooltipTrigger asChild>
              <span>
                <Button
                  variant="ghost"
                  size="icon"
                  className={cn("h-7 w-7", !traceLink && "opacity-40 cursor-not-allowed")}
                  disabled={!traceLink}
                  onClick={() => traceLink && window.open(traceLink, "_blank")}
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                </Button>
              </span>
            </TooltipTrigger>
            <TooltipContent>
              {traceLink ? "View in APM" : "Configure trace link template first"}
            </TooltipContent>
          </Tooltip>
          {trace.executionId && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  onClick={() => {
                    window.location.href = `/pipelines?id=${trace.executionId}`;
                  }}
                >
                  <ArrowRight className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Open in Pipelogiq</TooltipContent>
            </Tooltip>
          )}
        </div>
      </TableCell>
    </TableRow>
  );
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`;
  return `${(ms / 60_000).toFixed(1)}m`;
}
