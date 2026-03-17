import { useEffect, useMemo, useState } from "react";
import { Braces, Check, Copy, Eye, Type } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";

type ContextValueItem = {
  key: string;
  value: string;
  valueType?: string;
};

interface ContextValueViewerProps {
  item: ContextValueItem;
  className?: string;
}

export function ContextValueViewer({ item, className }: ContextValueViewerProps) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [showRaw, setShowRaw] = useState(false);
  const display = useMemo(() => buildDisplayState(item.value), [item.value]);

  useEffect(() => {
    if (!copied) return;
    const timeoutId = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(timeoutId);
  }, [copied]);

  useEffect(() => {
    if (!open) {
      setShowRaw(false);
    }
  }, [open]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(item.value);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  if (!display.isExpandable) {
    return (
      <div className={cn("font-mono text-sm text-slate-700 break-all", className)}>
        {item.value}
      </div>
    );
  }

  return (
    <>
      <div className={cn("min-w-0", className)}>
        <div className="flex items-start gap-3 rounded-lg border border-slate-200 bg-slate-50/70 p-3">
          <div className="min-w-0 flex-1">
            <div className="font-mono text-xs leading-5 text-slate-700 break-words">
              {display.preview}
            </div>
          </div>
        </div>

        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 shrink-0 gap-1.5"
          onClick={() => setOpen(true)}
        >
          Open full view
        </Button>
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-5xl gap-0 overflow-hidden p-0">
          <DialogHeader className="border-b border-slate-200 bg-slate-50 px-6 py-5">
            <div className="flex items-start justify-between gap-4 pr-8">
              <div className="min-w-0 space-y-2">
                <DialogTitle className="break-all font-mono text-base text-slate-900">
                  {item.key}
                </DialogTitle>
                <DialogDescription className="flex flex-wrap items-center gap-2">
                  <Badge variant="outline" className="border-slate-200 bg-white text-[11px] text-slate-600">
                    {display.kind === "json" ? "JSON" : "Text"}
                  </Badge>
                  <Badge variant="outline" className="border-slate-200 bg-white text-[11px] text-slate-500">
                    {display.sizeLabel}
                  </Badge>
                  {item.valueType && (
                    <Badge
                      variant="outline"
                      className="max-w-full border-blue-200 bg-blue-50 text-[11px] text-blue-700"
                      title={item.valueType}
                    >
                      <span className="truncate">{item.valueType}</span>
                    </Badge>
                  )}
                </DialogDescription>
              </div>

              <div className="flex items-center gap-2">
                {display.kind === "json" && (
                  <div className="rounded-lg border border-slate-200 bg-white p-1">
                    <Button
                      type="button"
                      variant={showRaw ? "ghost" : "secondary"}
                      size="sm"
                      className="h-7 px-2 text-xs"
                      onClick={() => setShowRaw(false)}
                    >
                      Pretty
                    </Button>
                    <Button
                      type="button"
                      variant={showRaw ? "secondary" : "ghost"}
                      size="sm"
                      className="h-7 px-2 text-xs"
                      onClick={() => setShowRaw(true)}
                    >
                      Raw
                    </Button>
                  </div>
                )}

                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-8 gap-1.5"
                  onClick={handleCopy}
                >
                  {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  {copied ? "Copied" : "Copy"}
                </Button>
              </div>
            </div>
          </DialogHeader>

          <ScrollArea className="max-h-[70vh] bg-slate-950">
            <pre className="overflow-x-auto p-6 font-mono text-xs leading-6 text-slate-100 whitespace-pre-wrap break-words">
              {display.kind === "json" && !showRaw ? display.prettyValue : item.value}
            </pre>
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </>
  );
}

type DisplayState = {
  kind: "json" | "text";
  prettyValue: string;
  preview: string;
  sizeLabel: string;
  isExpandable: boolean;
};

function buildDisplayState(value: string): DisplayState {
  const parsed = parseJsonValue(value);
  const prettyValue = parsed ? JSON.stringify(parsed, null, 2) : value;
  const previewSource = parsed ? prettyValue : value;

  return {
    kind: parsed ? "json" : "text",
    prettyValue,
    preview: buildPreview(previewSource),
    sizeLabel: formatBytes(new TextEncoder().encode(value).length),
    isExpandable: Boolean(parsed) || isLargeText(value),
  };
}

function parseJsonValue(value: string): unknown | null {
  const trimmed = value.trim();
  if (!(trimmed.startsWith("{") || trimmed.startsWith("["))) {
    return null;
  }

  try {
    return JSON.parse(trimmed);
  } catch {
    return null;
  }
}

function buildPreview(value: string): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  if (!normalized) {
    return "(empty)";
  }
  if (normalized.length <= 220) {
    return normalized;
  }
  return `${normalized.slice(0, 220)}...`;
}

function isLargeText(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) {
    return false;
  }
  if (trimmed.length > 240) {
    return true;
  }
  return trimmed.split("\n").length > 4;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
