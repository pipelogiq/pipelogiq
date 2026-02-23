import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { StatusBadge } from "@/components/ui/status-badge";
import { Loader2, Plus, X, Zap } from "lucide-react";
import { useSaveIntegrationConfig, useTestConnection } from "@/hooks/use-observability";
import type { AlertChannel, AlertEvent, AlertingConfig, Integration } from "@/types/observability";

interface ConfigureAlertingDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  integration: Integration;
}

const CHANNEL_OPTIONS: Array<{ value: AlertChannel; label: string; hint: string }> = [
  { value: "telegram", label: "Telegram", hint: "Bot token + chat ID" },
  { value: "whatsapp", label: "WhatsApp", hint: "Webhook/provider endpoint" },
  { value: "slack", label: "Slack", hint: "Incoming webhook" },
  { value: "teams", label: "Microsoft Teams", hint: "Incoming webhook" },
  { value: "webhook", label: "Generic webhook", hint: "POST JSON to any service" },
  { value: "email", label: "Email", hint: "Recipients list" },
  { value: "pagerduty", label: "PagerDuty", hint: "Events API routing key" },
];

const EVENT_OPTIONS: Array<{ value: AlertEvent; label: string }> = [
  { value: "stage_failed", label: "Stage failed" },
  { value: "stage_rerun_manual", label: "Stage rerun (manual)" },
  { value: "stage_skipped_manual", label: "Stage skipped (manual)" },
  { value: "pipeline_failed", label: "Pipeline failed" },
  { value: "pipeline_stuck", label: "Pipeline stuck / timeout" },
  { value: "worker_started", label: "Worker started" },
  { value: "worker_failed", label: "Worker failed" },
  { value: "worker_stopped", label: "Worker stopped" },
  { value: "worker_heartbeat_lost", label: "Worker heartbeat lost" },
  { value: "policy_triggered", label: "Policy triggered" },
  { value: "policy_changed", label: "Policy changed" },
  { value: "queue_backlog_high", label: "Queue backlog high" },
  { value: "dlq_message_detected", label: "DLQ message detected" },
];

function toStringArray(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((v): v is string => typeof v === "string").map(v => v.trim()).filter(Boolean);
  }
  if (typeof value === "string") {
    return value
      .split(/[,\n;]+/)
      .map(v => v.trim())
      .filter(Boolean);
  }
  return [];
}

function toggleItem<T extends string>(list: T[], value: T, enabled: boolean): T[] {
  if (enabled) {
    return list.includes(value) ? list : [...list, value];
  }
  return list.filter(item => item !== value);
}

export function ConfigureAlertingDialog({ open, onOpenChange, integration }: ConfigureAlertingDialogProps) {
  const existing = (integration.config || {}) as Partial<AlertingConfig>;

  const [channels, setChannels] = useState<AlertChannel[]>(toStringArray(existing.channels) as AlertChannel[]);
  const [enabledEvents, setEnabledEvents] = useState<AlertEvent[]>(
    (toStringArray(existing.enabledEvents).length
      ? toStringArray(existing.enabledEvents)
      : ["stage_failed", "stage_rerun_manual", "stage_skipped_manual", "worker_failed", "policy_triggered"]) as AlertEvent[],
  );
  const [sendResolved, setSendResolved] = useState(Boolean(existing.sendResolved ?? true));
  const [dedupeWindowSeconds, setDedupeWindowSeconds] = useState(String(existing.dedupeWindowSeconds ?? 300));
  const [telegramBotToken, setTelegramBotToken] = useState(existing.telegramBotToken || "");
  const [telegramChatId, setTelegramChatId] = useState(existing.telegramChatId || "");
  const [whatsappWebhookUrl, setWhatsappWebhookUrl] = useState(existing.whatsappWebhookUrl || "");
  const [slackWebhookUrl, setSlackWebhookUrl] = useState(existing.slackWebhookUrl || "");
  const [teamsWebhookUrl, setTeamsWebhookUrl] = useState(existing.teamsWebhookUrl || "");
  const [webhookUrl, setWebhookUrl] = useState(existing.webhookUrl || "");
  const [emailRecipients, setEmailRecipients] = useState(existing.emailRecipients || "");
  const [pagerdutyRoutingKey, setPagerdutyRoutingKey] = useState(existing.pagerdutyRoutingKey || "");
  const [channelToAdd, setChannelToAdd] = useState<AlertChannel | "">("");

  useEffect(() => {
    if (!open) return;

    const cfg = (integration.config || {}) as Partial<AlertingConfig>;
    setChannels(toStringArray(cfg.channels) as AlertChannel[]);

    const events = toStringArray(cfg.enabledEvents);
    setEnabledEvents(
      (events.length
        ? events
        : ["stage_failed", "stage_rerun_manual", "stage_skipped_manual", "worker_failed", "policy_triggered"]) as AlertEvent[],
    );
    setSendResolved(Boolean(cfg.sendResolved ?? true));
    setDedupeWindowSeconds(String(cfg.dedupeWindowSeconds ?? 300));
    setTelegramBotToken(cfg.telegramBotToken || "");
    setTelegramChatId(cfg.telegramChatId || "");
    setWhatsappWebhookUrl(cfg.whatsappWebhookUrl || "");
    setSlackWebhookUrl(cfg.slackWebhookUrl || "");
    setTeamsWebhookUrl(cfg.teamsWebhookUrl || "");
    setWebhookUrl(cfg.webhookUrl || "");
    setEmailRecipients(cfg.emailRecipients || "");
    setPagerdutyRoutingKey(cfg.pagerdutyRoutingKey || "");
    setChannelToAdd("");
  }, [open, integration.config]);

  const saveMutation = useSaveIntegrationConfig();
  const testMutation = useTestConnection();

  const buildConfigPayload = () => ({
    channels,
    enabledEvents,
    sendResolved,
    dedupeWindowSeconds: Number(dedupeWindowSeconds) || 300,
    telegramBotToken: telegramBotToken.trim(),
    telegramChatId: telegramChatId.trim(),
    whatsappWebhookUrl: whatsappWebhookUrl.trim(),
    slackWebhookUrl: slackWebhookUrl.trim(),
    teamsWebhookUrl: teamsWebhookUrl.trim(),
    webhookUrl: webhookUrl.trim(),
    emailRecipients: emailRecipients.trim(),
    pagerdutyRoutingKey: pagerdutyRoutingKey.trim(),
  });

  const handleSave = () => {
    saveMutation.mutate(
      {
        type: "alerting",
        config: buildConfigPayload(),
      },
      { onSuccess: () => onOpenChange(false) },
    );
  };

  const handleSendTestAlert = async () => {
    try {
      await saveMutation.mutateAsync({
        type: "alerting",
        config: buildConfigPayload(),
      });
      await testMutation.mutateAsync("alerting");
    } catch {
      // Errors are rendered by save/test mutation states below.
    }
  };

  const availableChannels = CHANNEL_OPTIONS.filter(option => !channels.includes(option.value));

  const addChannel = () => {
    if (!channelToAdd) return;
    setChannels(prev => toggleItem(prev, channelToAdd, true));
    setChannelToAdd("");
  };

  const removeChannel = (channel: AlertChannel) => {
    setChannels(prev => prev.filter(item => item !== channel));
  };

  const channelLabel = (channel: AlertChannel) =>
    CHANNEL_OPTIONS.find(option => option.value === channel)?.label ?? channel;

  const statusBadge = (() => {
    if (integration.status === "connected") return "success" as const;
    if (integration.status === "disconnected" || integration.status === "error") return "error" as const;
    if (integration.status === "configured") return "warning" as const;
    return "default" as const;
  })();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Configure Alerts</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="grid gap-4 md:grid-cols-3">
            <div className="space-y-2">
              <Label>Status</Label>
              <div className="flex h-10 items-center">
                <StatusBadge status={statusBadge}>
                  {integration.status === "not_configured" ? "Not configured" : integration.status}
                </StatusBadge>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="alerts-dedupe-window">Dedupe Window (sec)</Label>
              <Input
                id="alerts-dedupe-window"
                type="number"
                min={1}
                value={dedupeWindowSeconds}
                onChange={(e) => setDedupeWindowSeconds(e.target.value)}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="alerts-send-resolved">Send Resolved</Label>
              <div className="flex h-10 items-center gap-3">
                <Switch id="alerts-send-resolved" checked={sendResolved} onCheckedChange={setSendResolved} />
                <span className="text-sm text-muted-foreground">Notify when issue recovers</span>
              </div>
            </div>
          </div>

          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <Label>Channel Configurations</Label>
              <span className="text-xs text-muted-foreground">{channels.length} added</span>
            </div>
            <div className="flex gap-2">
              <div className="flex-1">
                <Select value={channelToAdd} onValueChange={(v) => setChannelToAdd(v as AlertChannel | "")}>
                  <SelectTrigger>
                    <SelectValue placeholder="Add configuration from dropdown" />
                  </SelectTrigger>
                  <SelectContent>
                    {availableChannels.length === 0 ? (
                      <SelectItem value="__none" disabled>All supported channels added</SelectItem>
                    ) : (
                      availableChannels.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))
                    )}
                  </SelectContent>
                </Select>
              </div>
              <Button type="button" variant="outline" onClick={addChannel} disabled={!channelToAdd}>
                <Plus className="mr-1.5 h-4 w-4" />
                Add
              </Button>
            </div>
            <div className="space-y-3">
              {channels.map((channel) => (
                <div key={channel} className="rounded-lg border border-border p-3">
                  <div className="mb-2 flex items-center justify-between gap-3">
                    <div>
                      <p className="text-sm font-medium">{channelLabel(channel)}</p>
                      <p className="text-xs text-muted-foreground">
                        {CHANNEL_OPTIONS.find(option => option.value === channel)?.hint}
                      </p>
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-8 px-2"
                      onClick={() => removeChannel(channel)}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>

                  {channel === "telegram" && (
                    <div className="grid gap-2 md:grid-cols-2">
                      <Input
                        placeholder="Bot token"
                        value={telegramBotToken}
                        onChange={(e) => setTelegramBotToken(e.target.value)}
                      />
                      <Input
                        placeholder="Chat ID"
                        value={telegramChatId}
                        onChange={(e) => setTelegramChatId(e.target.value)}
                      />
                    </div>
                  )}

                  {channel === "whatsapp" && (
                    <Input
                      placeholder="Provider webhook URL"
                      value={whatsappWebhookUrl}
                      onChange={(e) => setWhatsappWebhookUrl(e.target.value)}
                    />
                  )}

                  {channel === "slack" && (
                    <Input
                      placeholder="Slack webhook URL"
                      value={slackWebhookUrl}
                      onChange={(e) => setSlackWebhookUrl(e.target.value)}
                    />
                  )}

                  {channel === "teams" && (
                    <Input
                      placeholder="Teams webhook URL"
                      value={teamsWebhookUrl}
                      onChange={(e) => setTeamsWebhookUrl(e.target.value)}
                    />
                  )}

                  {channel === "webhook" && (
                    <Input
                      placeholder="Webhook URL"
                      value={webhookUrl}
                      onChange={(e) => setWebhookUrl(e.target.value)}
                    />
                  )}

                  {channel === "email" && (
                    <Input
                      placeholder="ops@example.com, oncall@example.com"
                      value={emailRecipients}
                      onChange={(e) => setEmailRecipients(e.target.value)}
                    />
                  )}

                  {channel === "pagerduty" && (
                    <Input
                      placeholder="Routing key"
                      value={pagerdutyRoutingKey}
                      onChange={(e) => setPagerdutyRoutingKey(e.target.value)}
                    />
                  )}
                </div>
              ))}
            </div>
          </div>

          <details className="rounded-lg border border-border p-3">
            <summary className="cursor-pointer list-none text-sm font-medium">
              Events to Notify ({enabledEvents.length})
            </summary>
            <div className="mt-3 grid gap-2 md:grid-cols-2">
              {EVENT_OPTIONS.map((option) => (
                <label
                  key={option.value}
                  className="flex items-center gap-2 rounded-md border border-border px-2.5 py-2 cursor-pointer hover:bg-muted/30"
                >
                  <Checkbox
                    checked={enabledEvents.includes(option.value)}
                    onCheckedChange={(checked) =>
                      setEnabledEvents(prev => toggleItem(prev, option.value, checked === true))
                    }
                  />
                  <span className="text-sm">{option.label}</span>
                </label>
              ))}
            </div>
          </details>

          {saveMutation.isError && (
            <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
              Failed to save: {saveMutation.error instanceof Error ? saveMutation.error.message : "Unknown error"}
            </div>
          )}

          {testMutation.data && (
            <div className={`rounded-md p-3 text-sm ${testMutation.data.success ? "bg-green-50 text-green-800 border border-green-200" : "bg-red-50 text-red-800 border border-red-200"}`}>
              {testMutation.data.message}
              {testMutation.data.latencyMs != null && ` (${testMutation.data.latencyMs}ms)`}
            </div>
          )}
          {testMutation.isError && (
            <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
              Failed to send test alert: {testMutation.error instanceof Error ? testMutation.error.message : "Unknown error"}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={handleSendTestAlert}
            disabled={channels.length === 0 || enabledEvents.length === 0 || saveMutation.isPending || testMutation.isPending}
          >
            {(saveMutation.isPending || testMutation.isPending) ? (
              <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
            ) : (
              <Zap className="mr-1.5 h-4 w-4" />
            )}
            Send test alert
          </Button>
          <Button
            onClick={handleSave}
            disabled={channels.length === 0 || enabledEvents.length === 0 || saveMutation.isPending || testMutation.isPending}
          >
            {saveMutation.isPending && <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />}
            Save Configuration
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
