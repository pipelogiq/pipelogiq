import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { StatusBadge } from "@/components/ui/status-badge";
import { Loader2 } from "lucide-react";
import { useSaveIntegrationConfig } from "@/hooks/use-observability";
import type { Integration, LogsConfig } from "@/types/observability";

interface ConfigureLogsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  integration: Integration;
}

export function ConfigureLogsDialog({ open, onOpenChange, integration }: ConfigureLogsDialogProps) {
  const existing = (integration.config || {}) as Partial<LogsConfig>;

  const [provider, setProvider] = useState<"graylog" | "loki" | "elastic">(existing.provider || "graylog");
  const [baseUrl, setBaseUrl] = useState(existing.baseUrl || "");
  const [searchUrlTemplate, setSearchUrlTemplate] = useState(
    existing.searchUrlTemplate || "",
  );

  const saveMutation = useSaveIntegrationConfig();

  const handleSave = () => {
    saveMutation.mutate(
      {
        type: "graylog",
        config: { provider, baseUrl, searchUrlTemplate },
      },
      { onSuccess: () => onOpenChange(false) },
    );
  };

  // Build preview URL
  const previewUrl = searchUrlTemplate
    ? searchUrlTemplate
        .replace(/\$\{traceId\}/g, "abc123def456")
        .replace(/\$\{executionId\}/g, "42")
        .replace(/\$\{stageId\}/g, "7")
    : "";

  const defaultTemplates: Record<string, string> = {
    graylog: "${baseUrl}/search?q=traceId%3A${traceId}&rangetype=relative&relative=3600",
    loki: "${baseUrl}/explore?left=%5B%22now-1h%22%2C%22now%22%2C%22Loki%22%2C%7B%22expr%22%3A%22%7BtraceId%3D%5C%22${traceId}%5C%22%7D%22%7D%5D",
    elastic: "${baseUrl}/app/discover#/?_a=(query:(query_string:(query:'traceId:${traceId}')))",
  };

  const handleApplyTemplate = () => {
    if (baseUrl) {
      setSearchUrlTemplate(defaultTemplates[provider].replace("${baseUrl}", baseUrl));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Configure Logs</DialogTitle>
          <DialogDescription>
            Set up deep links to your centralized logging system for trace and execution lookup.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label>Provider</Label>
              <Select value={provider} onValueChange={(v) => setProvider(v as "graylog" | "loki" | "elastic")}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="graylog">Graylog</SelectItem>
                  <SelectItem value="loki">Grafana Loki</SelectItem>
                  <SelectItem value="elastic">Elasticsearch / Kibana</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>Status</Label>
              <div className="flex items-center h-10">
                <StatusBadge
                  status={
                    integration.status === "connected" ? "success" :
                    integration.status === "configured" ? "warning" :
                    "default"
                  }
                >
                  {integration.status === "not_configured" ? "Not configured" : integration.status}
                </StatusBadge>
              </div>
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="logs-base-url">Base URL</Label>
            <Input
              id="logs-base-url"
              placeholder="https://graylog.example.com"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label htmlFor="logs-search-template">Search URL Template</Label>
              <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={handleApplyTemplate} disabled={!baseUrl}>
                Apply default template
              </Button>
            </div>
            <Input
              id="logs-search-template"
              placeholder={"https://graylog.example.com/search?q=traceId:${traceId}"}
              value={searchUrlTemplate}
              onChange={(e) => setSearchUrlTemplate(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {"Available placeholders: ${traceId}, ${executionId}, ${stageId}"}
            </p>
          </div>

          {/* Preview */}
          {previewUrl && (
            <div className="space-y-2">
              <Label>Preview (example values)</Label>
              <pre className="rounded-md bg-muted p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap break-all">
                {previewUrl}
              </pre>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!baseUrl || saveMutation.isPending}>
            {saveMutation.isPending && <Loader2 className="h-4 w-4 animate-spin mr-1.5" />}
            Save Configuration
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
