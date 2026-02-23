import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface PaginationProps {
  currentPage: number;
  totalItems: number;
  pageSize: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
}

const pageSizeOptions = [25, 50, 100, 200];

export function Pagination({
  currentPage,
  totalItems,
  pageSize,
  onPageChange,
  onPageSizeChange,
}: PaginationProps) {
  const totalPages = Math.ceil(totalItems / pageSize);
  const startItem = (currentPage - 1) * pageSize + 1;
  const endItem = Math.min(currentPage * pageSize, totalItems);

  const canGoPrev = currentPage > 1;
  const canGoNext = currentPage < totalPages;

  if (totalItems === 0) return null;

  return (
    <div className="flex items-center justify-between border-t border-border bg-background px-4 py-2">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <span className="tabular-nums">
          {startItem.toLocaleString()}–{endItem.toLocaleString()} of {totalItems.toLocaleString()}
        </span>
        <div className="flex items-center gap-1.5">
          <Select
            value={pageSize.toString()}
            onValueChange={(v) => onPageSizeChange(Number(v))}
          >
            <SelectTrigger className="h-7 w-16 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {pageSizeOptions.map((size) => (
                <SelectItem key={size} value={size.toString()} className="text-xs">
                  {size}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-xs text-muted-foreground">/page</span>
        </div>
      </div>

      <div className="flex items-center gap-1">
        <Button
          variant="ghost"
          size="sm"
          className="h-7 px-2 text-xs"
          onClick={() => onPageChange(currentPage - 1)}
          disabled={!canGoPrev}
        >
          <ChevronLeft className="h-4 w-4 mr-1" />
          Prev
        </Button>
        <span className="px-2 text-xs text-muted-foreground tabular-nums">
          Page {currentPage.toLocaleString()} of {totalPages.toLocaleString()}
        </span>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 px-2 text-xs"
          onClick={() => onPageChange(currentPage + 1)}
          disabled={!canGoNext}
        >
          Next
          <ChevronRight className="h-4 w-4 ml-1" />
        </Button>
      </div>
    </div>
  );
}
