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
import { StatusBadge } from "@/components/ui/status-badge";
import { Loader2 } from "lucide-react";
import { useSaveIntegrationConfig } from "@/hooks/use-observability";
import type { Integration, GrafanaConfig } from "@/types/observability";

interface ConfigureGrafanaDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  integration: Integration;
}

export function ConfigureGrafanaDialog({ open, onOpenChange, integration }: ConfigureGrafanaDialogProps) {
  const existing = (integration.config || {}) as Partial<GrafanaConfig>;
  const [dashboardUrl, setDashboardUrl] = useState(existing.dashboardUrl || "http://localhost:3000");

  const saveMutation = useSaveIntegrationConfig();

  const handleSave = () => {
    saveMutation.mutate(
      {
        type: "grafana",
        config: { dashboardUrl },
      },
      { onSuccess: () => onOpenChange(false) },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>Configure Grafana</DialogTitle>
          <DialogDescription>
            Configure the Grafana base URL used by the Observability views and trace links.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="grafana-dashboard-url">Dashboard URL</Label>
              <Input
                id="grafana-dashboard-url"
                placeholder="http://localhost:3000"
                value={dashboardUrl}
                onChange={(e) => setDashboardUrl(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>Status</Label>
              <div className="flex h-10 items-center">
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

          <p className="text-xs text-muted-foreground">
            Local Docker Compose defaults: Grafana at <code>http://localhost:3000</code>, Tempo API at <code>http://localhost:3200</code>.
          </p>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={!dashboardUrl || saveMutation.isPending}>
            {saveMutation.isPending && <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />}
            Save Configuration
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
