import { useState, useMemo, useCallback, useEffect } from "react";
import { AppHeader } from "@/components/layout/AppHeader";
import { PipelineSearchBar, SearchFilters } from "@/components/pipelines/PipelineSearchBar";
import { PipelineTable } from "@/components/pipelines/PipelineTable";
import { PipelineSidePanel } from "@/components/pipelines/PipelineSidePanel";
import { Pagination } from "@/components/pipelines/Pagination";
import { ScrollArea } from "@/components/ui/scroll-area";
import { usePipelines } from "@/hooks/use-pipelines";
import { Loader2 } from "lucide-react";

// Map UI status to API status
function mapUIStatusToAPI(status: string): string[] {
  switch (status) {
    case 'success':
      return ['Completed'];
    case 'error':
      return ['Failed'];
    case 'running':
      return ['Running'];
    case 'waiting':
      return ['NotStarted', 'Pending', 'RetryScheduled'];
    case 'paused':
      return ['Skipped'];
    default:
      return [];
  }
}

export default function Pipelines() {
  const [filters, setFilters] = useState<SearchFilters>({
    query: "",
    status: "all",
    environment: "all",
    dateRange: "all",
    owner: "all",
    tags: [],
    contextFilters: [],
  });
  const [selectedPipelineId, setSelectedPipelineId] = useState<string | null>(null);
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);

  // Build API params from filters
  const apiParams = useMemo(() => {
    const params: Record<string, unknown> = {
      pageNumber: currentPage,
      pageSize: pageSize,
    };

    // Status filter
    if (filters.status !== "all") {
      const statuses = filters.status.split(",").flatMap(s => mapUIStatusToAPI(s.trim()));
      if (statuses.length > 0) {
        params.statuses = statuses;
      }
    }

    // Search query
    if (filters.query) {
      params.search = filters.query;
    }

    // Keywords from tags
    if (filters.tags.length > 0) {
      params.keywords = [...(params.keywords as string[] || []), ...filters.tags];
    }

    // Date range filter
    if (filters.dateRange && filters.dateRange !== "all") {
      if (filters.dateRange.startsWith("last:")) {
        const [, valueStr, unit] = filters.dateRange.split(":");
        const value = Number(valueStr) || 0;
        if (value > 0) {
          const now = new Date();
          const ms =
            unit === "hours" ? value * 60 * 60 * 1000 :
            unit === "minutes" ? value * 60 * 1000 :

            value * 1000; // seconds
          const from = new Date(now.getTime() - ms).toISOString();
          params.pipelineStartFrom = from;
          params.pipelineStartTo = now.toISOString();
        }
      } else if (filters.dateRange.startsWith("range:")) {
        const rangePart = filters.dateRange.replace("range:", "");
        const [from, to] = rangePart.split("-").map(s => s.trim()).filter(Boolean);
        if (from) params.pipelineStartFrom = from;
        if (to) params.pipelineStartTo = to;
      }
    }

    return params;
  }, [filters, currentPage, pageSize]);

  const { data, isLoading, error } = usePipelines(apiParams);

  // Client-side filtering for environment and owner (if API doesn't support)
  const filteredPipelines = useMemo(() => {
    if (!data?.items) return [];

    return data.items.filter((pipeline) => {
      // Environment filter
      if (filters.environment !== "all" && pipeline.environment !== filters.environment) {
        return false;
      }

      // Owner filter
      if (filters.owner !== "all" && pipeline.owner !== filters.owner) {
        return false;
      }

      // Context key-value filters
      if (filters.contextFilters.length > 0) {
        const allContextFiltersMatch = filters.contextFilters.every((cf) => {
          return pipeline.context.some(
            (ctx) => ctx.key.toLowerCase() === cf.key.toLowerCase() &&
              ctx.value.toLowerCase().includes(cf.value.toLowerCase())
          );
        });
        if (!allContextFiltersMatch) {
          return false;
        }
      }

      return true;
    });
  }, [data?.items, filters.environment, filters.owner, filters.contextFilters]);

  // Close detail panel if selected pipeline is no longer in results
  useEffect(() => {
    if (selectedPipelineId && filteredPipelines.length > 0 && !filteredPipelines.some(p => p.id === selectedPipelineId)) {
      setSelectedPipelineId(null);
    }
  }, [filteredPipelines, selectedPipelineId]);

  const totalResults = data?.totalCount || 0;

  const handlePageChange = useCallback((page: number) => {
    setCurrentPage(page);
    setSelectedPipelineId(null);
  }, []);

  const handlePageSizeChange = useCallback((size: number) => {
    setPageSize(size);
    setCurrentPage(1);
  }, []);

  const handleFiltersChange = useCallback((f: SearchFilters) => {
    setFilters(f);
    setCurrentPage(1);
  }, []);

  if (error) {
    return (
      <div className="flex flex-col h-screen">
        <AppHeader
          title="Pipelines"
          subtitle="Search and monitor pipeline executions"
        />
        <div className="flex-1 flex items-center justify-center">
          <div className="text-center">
            <p className="text-destructive mb-2">Failed to load pipelines</p>
            <p className="text-sm text-muted-foreground">
              {error instanceof Error ? error.message : 'Unknown error'}
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-screen">
      <AppHeader
        title="Pipelines"
        subtitle="Search and monitor pipeline executions"
      />

      <div className="px-4 py-4 border-b border-border bg-background">
        <PipelineSearchBar
          onFiltersChange={handleFiltersChange}
          totalResults={totalResults}
        />
      </div>

      <div className="flex-1 flex min-h-0 gap-4 px-4 pb-4 pt-4">
        {/* Pipeline Table Card */}
        <div className="flex-1 flex flex-col min-h-0 rounded-lg border border-border bg-card overflow-hidden shadow-sm">
          {isLoading ? (
            <div className="flex-1 flex items-center justify-center">
              <Loader2 className="h-8 w-8 animate-spin text-primary" />
            </div>
          ) : (
            <>
              <ScrollArea className="flex-1">
                <PipelineTable
                  pipelines={filteredPipelines}
                  selectedId={selectedPipelineId}
                  onSelect={(pipeline) => setSelectedPipelineId(pipeline.id)}
                  isPanelOpen={!!selectedPipelineId}
                />
              </ScrollArea>

              <div className="border-t border-border">
                <Pagination
                  currentPage={currentPage}
                  totalItems={totalResults}
                  pageSize={pageSize}
                  onPageChange={handlePageChange}
                  onPageSizeChange={handlePageSizeChange}
                />
              </div>
            </>
          )}
        </div>

        {/* Side Panel Card */}
        {selectedPipelineId && (
          <div className="w-[40%] min-w-[480px] max-w-[600px] shrink-0 rounded-lg border border-border bg-card overflow-hidden shadow-sm">
            <PipelineSidePanel
              pipelineId={Number(selectedPipelineId)}
              onClose={() => setSelectedPipelineId(null)}
            />
          </div>
        )}
      </div>
    </div>
  );
}
