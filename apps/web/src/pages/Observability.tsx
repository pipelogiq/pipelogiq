import { useState } from "react";
import { AppHeader } from "@/components/layout/AppHeader";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ScrollArea } from "@/components/ui/scroll-area";
import { OverviewTab } from "@/components/observability/OverviewTab";
import { TracesTab } from "@/components/observability/TracesTab";
import { MetricsTab } from "@/components/observability/MetricsTab";
import { IntegrationsTab } from "@/components/observability/IntegrationsTab";
import type { TimeRange } from "@/types/observability";

export default function Observability() {
  const [timeRange, setTimeRange] = useState<TimeRange>("1h");

  return (
    <div className="flex flex-col h-screen">
      <AppHeader
        title="Observability"
        subtitle="Control plane for traces, metrics, and operational insights"
      />

      <div className="flex-1 flex flex-col min-h-0">
        <Tabs defaultValue="overview" className="flex-1 flex flex-col min-h-0">
          {/* Tab bar + time range selector */}
          <div className="flex items-center justify-between px-6 py-3 border-b border-border bg-background">
            <TabsList>
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="traces">Traces</TabsTrigger>
              <TabsTrigger value="metrics">Metrics</TabsTrigger>
              <TabsTrigger value="integrations">Integrations</TabsTrigger>
            </TabsList>

            <Select value={timeRange} onValueChange={(v) => setTimeRange(v as TimeRange)}>
              <SelectTrigger className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="15m">Last 15 min</SelectItem>
                <SelectItem value="1h">Last 1 hour</SelectItem>
                <SelectItem value="6h">Last 6 hours</SelectItem>
                <SelectItem value="24h">Last 24 hours</SelectItem>
                <SelectItem value="7d">Last 7 days</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Tab content */}
          <ScrollArea className="flex-1">
            <div className="p-6">
              <TabsContent value="overview" className="mt-0">
                <OverviewTab timeRange={timeRange} />
              </TabsContent>

              <TabsContent value="traces" className="mt-0">
                <TracesTab timeRange={timeRange} />
              </TabsContent>

              <TabsContent value="metrics" className="mt-0">
                <MetricsTab timeRange={timeRange} />
              </TabsContent>

              <TabsContent value="integrations" className="mt-0">
                <IntegrationsTab />
              </TabsContent>
            </div>
          </ScrollArea>
        </Tabs>
      </div>
    </div>
  );
}
