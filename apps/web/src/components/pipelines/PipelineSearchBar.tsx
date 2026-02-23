import { useState, useCallback, KeyboardEvent, useEffect } from "react";
import { Search, X, Clock, ChevronDown, Layers } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";

export interface ContextKeyFilter {
  key: string;
  value: string;
}

export interface SearchFilters {
  query: string;
  status: string;
  environment: string;
  dateRange: string;
  owner: string;
  tags: string[];
  contextFilters: ContextKeyFilter[];
}

interface PipelineSearchBarProps {
  onFiltersChange: (filters: SearchFilters) => void;
  totalResults: number;
  isSearching?: boolean;
}

const statusOptions = [
  { value: "running", label: "Running", color: "bg-blue-500" },
  { value: "success", label: "Completed", color: "bg-emerald-500" },
  { value: "error", label: "Failed", color: "bg-red-500" },
  { value: "paused", label: "Paused", color: "bg-amber-500" },
  { value: "throttled", label: "Throttled", color: "bg-orange-500" },
];

const statusShortLabels: Record<string, string> = {
  running: "Run",
  success: "OK",
  error: "Fail",
  paused: "Pause",
  throttled: "Throt",
};

type PeriodMode = "all" | "last" | "range";
type PeriodUnit = "seconds" | "minutes" | "hours";

export function PipelineSearchBar({
  onFiltersChange,
  totalResults,
  isSearching = false,
}: PipelineSearchBarProps) {
  const [filters, setFilters] = useState<SearchFilters>({
    query: "",
    status: "all",
    environment: "all",
    dateRange: "all",
    owner: "all",
    tags: [],
    contextFilters: [],
  });

  const [searchInput, setSearchInput] = useState("");
  const [selectedStatuses, setSelectedStatuses] = useState<string[]>([]);
  const [statusOpen, setStatusOpen] = useState(false);
  const [periodOpen, setPeriodOpen] = useState(false);

  const [periodMode, setPeriodMode] = useState<PeriodMode>("last");
  const [lastValue, setLastValue] = useState<number>(10);
  const [lastUnit, setLastUnit] = useState<PeriodUnit>("minutes");
  const [rangeFrom, setRangeFrom] = useState<string>("");
  const [rangeTo, setRangeTo] = useState<string>("");

  const updateFilters = useCallback((updates: Partial<SearchFilters>) => {
    setFilters((prev) => ({ ...prev, ...updates }));
  }, []);

  // initial fetch on mount
  useEffect(() => {
    onFiltersChange(filters);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleTagToggle = (tag: string) => {
    const newTags = filters.tags.includes(tag)
      ? filters.tags.filter((t) => t !== tag)
      : [...filters.tags, tag];
    updateFilters({ tags: newTags });
  };

  const parseAndApplySearch = (input: string, apply = false) => {
    const keyValuePattern = /(\w+):([^\s]+)/g;
    const contextFilters: ContextKeyFilter[] = [];
    let remainingQuery = input;

    let match;
    while ((match = keyValuePattern.exec(input)) !== null) {
      contextFilters.push({ key: match[1], value: match[2] });
      remainingQuery = remainingQuery.replace(match[0], "").trim();
    }

    const newFilters = {
      ...filters,
      query: remainingQuery,
      contextFilters,
    };
    updateFilters(newFilters);
    if (apply) {
      onFiltersChange(newFilters);
    }
  };

  const handleSearchKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      handleSearch();
    }
  };

  const handleSearchChange = (value: string) => {
    setSearchInput(value);
  };

  const removeContextFilter = (index: number) => {
    const newContextFilters = filters.contextFilters.filter((_, i) => i !== index);
    updateFilters({ contextFilters: newContextFilters });
    const cf = filters.contextFilters[index];
    setSearchInput((prev) => prev.replace(`${cf.key}:${cf.value}`, "").trim());
  };

  const handleStatusToggle = (statusValue: string) => {
    const newStatuses = selectedStatuses.includes(statusValue)
      ? selectedStatuses.filter((s) => s !== statusValue)
      : [...selectedStatuses, statusValue];
    setSelectedStatuses(newStatuses);
    updateFilters({ status: newStatuses.length === 0 ? "all" : newStatuses.join(",") });
  };

  const clearStatuses = () => {
    setSelectedStatuses([]);
    updateFilters({ status: "all" });
  };

  const handlePeriodApply = () => {
    if (periodMode === "all") {
      updateFilters({ dateRange: "all" });
    } else if (periodMode === "last") {
      updateFilters({
        dateRange: `last:${lastValue}:${lastUnit}`,
      });
    } else {
      updateFilters({
        dateRange: `range:${rangeFrom || "?"}-${rangeTo || "?"}`,
      });
    }
    setPeriodOpen(false);
  };

  const handleSearch = () => {
    parseAndApplySearch(searchInput, true);
  };

  const dateLabel = () => {
    if (filters.dateRange === "all") return "All Time";
    if (periodMode === "last") {
      return `Last ${lastValue} ${lastUnit}`;
    }
    return `Range: ${rangeFrom || "?"} — ${rangeTo || "?"}`;
  };

  return (
    <div className="space-y-2">
      {/* Main Search Bar */}
      <div className="flex gap-2 items-center">
        {/* Search Input */}
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            placeholder="Search... Use key:value for context (e.g. orderId:123)"
            className="pl-9 pr-4 h-9 font-mono text-sm"
            value={searchInput}
            onChange={(e) => handleSearchChange(e.target.value)}
            onKeyDown={handleSearchKeyDown}
          />
          {isSearching && (
            <div className="absolute right-3 top-1/2 -translate-y-1/2">
              <div className="h-4 w-4 animate-spin rounded-full border-2 border-primary border-t-transparent" />
            </div>
          )}
        </div>

        {/* Period Dropdown */}
        <Popover open={periodOpen} onOpenChange={setPeriodOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="outline"
              size="sm"
              className={cn(
                "h-9 gap-1.5 text-xs font-medium shrink-0",
                filters.dateRange !== "all" && "border-primary/50 bg-primary/5 text-primary"
              )}
            >
              <Clock className="h-3.5 w-3.5" />
              <span>{dateLabel()}</span>
              <ChevronDown className="h-3 w-3 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-64 p-3 z-50 bg-popover" align="end">
            <div className="flex flex-col gap-3 text-sm">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="font-medium text-xs">Mode</label>
                  <button
                    className="text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                    onClick={() => {
                      setPeriodMode("last");
                      setLastValue(10);
                      setLastUnit("minutes");
                      setRangeFrom("");
                      setRangeTo("");
                      updateFilters({ dateRange: "all" });
                      setPeriodOpen(false);
                    }}
                  >
                    Clear
                  </button>
                </div>
                <div className="flex gap-2">
                  {(["last", "range"] as PeriodMode[]).map((m) => (
                    <Button
                      key={m}
                      variant={periodMode === m ? "secondary" : "outline"}
                      size="sm"
                      className="h-8 flex-1 text-xs"
                      onClick={() => setPeriodMode(m)}
                    >
                      {m === "last" ? "Last" : "Range"}
                    </Button>
                  ))}
                </div>
              </div>
              {periodMode === "last" && (
                <div className="flex items-center gap-2">
                  <Input
                    type="number"
                    min={1}
                    value={lastValue}
                    onChange={(e) => setLastValue(Number(e.target.value) || 1)}
                    className="h-9 w-20"
                  />
                  <select
                    value={lastUnit}
                    onChange={(e) => setLastUnit(e.target.value as PeriodUnit)}
                    className="h-9 flex-1 rounded-md border border-input bg-background px-2 text-sm"
                  >
                    <option value="seconds">seconds</option>
                    <option value="minutes">minutes</option>
                    <option value="hours">hours</option>
                  </select>
                </div>
              )}

              {periodMode === "range" && (
                <div className="space-y-2">
                  <Input
                    type="datetime-local"
                    value={rangeFrom}
                    onChange={(e) => setRangeFrom(e.target.value)}
                    className="h-9 text-xs"
                  />
                  <Input
                    type="datetime-local"
                    value={rangeTo}
                    onChange={(e) => setRangeTo(e.target.value)}
                    className="h-9 text-xs"
                  />
                </div>
              )}

              <div className="flex flex-col gap-2">
                <Button size="sm" onClick={handlePeriodApply}>
                  Apply
                </Button>
              </div>
            </div>
          </PopoverContent>
        </Popover>

        {/* Status Multi-Select Dropdown */}
        <Popover open={statusOpen} onOpenChange={setStatusOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="outline"
              size="sm"
              className={cn(
                "h-9 gap-1.5 text-xs font-medium shrink-0 max-w-[280px]",
                selectedStatuses.length > 0 && "border-primary/50 bg-primary/5"
              )}
            >
              {selectedStatuses.length === 0 ? (
                <>
                  <Layers className="h-3.5 w-3.5" />
                  <span>Status</span>
                </>
              ) : (
                <div className="flex items-center gap-1">
                  {selectedStatuses.map((s) => {
                    const opt = statusOptions.find((o) => o.value === s);
                    return (
                      <span
                        key={s}
                        className={cn(
                          "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-bold leading-none",
                          s === "running" && "bg-blue-100 text-blue-700",
                          s === "success" && "bg-emerald-100 text-emerald-700",
                          s === "error" && "bg-red-100 text-red-700",
                          s === "paused" && "bg-amber-100 text-amber-700",
                          s === "throttled" && "bg-orange-100 text-orange-700"
                        )}
                      >
                        <span className={cn("h-1.5 w-1.5 rounded-full", opt?.color)} />
                        {statusShortLabels[s]}
                      </span>
                    );
                  })}
                </div>
              )}
              <ChevronDown className="h-3 w-3 opacity-50 shrink-0" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-52 p-2 z-50 bg-popover" align="end">
            <div className="flex items-center justify-between mb-2 px-1">
              <span className="text-xs font-semibold text-foreground">Filter by status</span>
              {selectedStatuses.length > 0 && (
                <button
                  onClick={clearStatuses}
                  className="text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                >
                  Clear
                </button>
              )}
            </div>
            <div className="flex flex-col gap-0.5">
              {statusOptions.map((opt) => (
                <label
                  key={opt.value}
                  className="flex items-center gap-2.5 px-2 py-1.5 rounded-md hover:bg-muted cursor-pointer transition-colors"
                >
                  <Checkbox
                    checked={selectedStatuses.includes(opt.value)}
                    onCheckedChange={() => handleStatusToggle(opt.value)}
                    className="h-3.5 w-3.5"
                  />
                  <span className={cn("h-2 w-2 rounded-full shrink-0", opt.color)} />
                  <span className="text-sm text-foreground">{opt.label}</span>
                </label>
              ))}
            </div>
          </PopoverContent>
        </Popover>

        <Button variant="default" size="sm" className="h-9 gap-1.5" onClick={handleSearch}>
          <Search className="h-3.5 w-3.5" />
          <span>Search</span>
        </Button>
      </div>

      {/* Active Filters Row */}
      {(filters.contextFilters.length > 0) && (
        <div className="flex items-center gap-1.5 flex-wrap">
          {filters.contextFilters.map((cf, index) => (
            <Badge
              key={`ctx-${index}`}
              variant="outline"
              className="gap-1 h-6 font-mono text-xs bg-primary/5 border-primary/30"
            >
              {cf.key}:{cf.value}
              <button onClick={() => removeContextFilter(index)} className="hover:text-destructive">
                <X className="h-3 w-3" />
              </button>
            </Badge>
          ))}
          <span className="text-xs text-muted-foreground ml-auto">
            {totalResults.toLocaleString()} results
          </span>
        </div>
      )}
    </div>
  );
}
