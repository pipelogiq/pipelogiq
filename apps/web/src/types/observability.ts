// Integration status lifecycle: not_configured → configured → connected ↔ disconnected / error
export type IntegrationStatus = 'not_configured' | 'configured' | 'connected' | 'disconnected' | 'error';

export type IntegrationType = 'opentelemetry' | 'alerting' | 'grafana' | 'sentry' | 'datadog' | 'graylog';

// Per-integration configs

export interface OtelConfig {
  endpoint: string;
  protocol: 'grpc' | 'http';
  headers: string; // key=value, one per line
  samplingRate: number; // 0-100
  traceLinkTemplate: string; // e.g. "https://tempo.example.com/trace/${traceId}"
}

export type AlertChannel =
  | 'telegram'
  | 'whatsapp'
  | 'slack'
  | 'email'
  | 'webhook'
  | 'pagerduty'
  | 'teams';

export type AlertEvent =
  | 'stage_failed'
  | 'stage_rerun_manual'
  | 'stage_skipped_manual'
  | 'pipeline_failed'
  | 'pipeline_stuck'
  | 'worker_started'
  | 'worker_failed'
  | 'worker_stopped'
  | 'worker_heartbeat_lost'
  | 'policy_triggered'
  | 'policy_changed'
  | 'queue_backlog_high'
  | 'dlq_message_detected';

export interface AlertingConfig {
  channels: AlertChannel[];
  enabledEvents: AlertEvent[];
  sendResolved: boolean;
  dedupeWindowSeconds: number;
  healthEndpoint?: string;
  telegramBotToken?: string;
  telegramChatId?: string;
  whatsappWebhookUrl?: string;
  slackWebhookUrl?: string;
  teamsWebhookUrl?: string;
  webhookUrl?: string;
  emailRecipients?: string; // comma-separated
  pagerdutyRoutingKey?: string;
}

export interface GrafanaConfig {
  dashboardUrl: string;
}

export interface SentryConfig {
  dsn: string;
  environment: string;
}

export interface DatadogConfig {
  site: string; // e.g. datadoghq.com
  apiKey: string;
}

export interface LogsConfig {
  provider: 'graylog' | 'loki' | 'elastic';
  baseUrl: string;
  searchUrlTemplate: string; // supports ${traceId}, ${executionId}, ${stageId}
}

// Union config stored per integration
export type IntegrationConfigData =
  | { type: 'opentelemetry'; config: OtelConfig }
  | { type: 'alerting'; config: AlertingConfig }
  | { type: 'grafana'; config: GrafanaConfig }
  | { type: 'sentry'; config: SentryConfig }
  | { type: 'datadog'; config: DatadogConfig }
  | { type: 'graylog'; config: LogsConfig };

export interface Integration {
  type: IntegrationType;
  name: string;
  description: string;
  icon: string;
  status: IntegrationStatus;
  lastTestedAt?: string;
  lastSuccessAt?: string;
  lastError?: string;
  config?: Record<string, unknown>;
}

// GET /api/observability/config
export interface ObservabilityConfig {
  integrations: Integration[];
}

// GET /api/observability/status
export interface ObservabilityStatus {
  otel: {
    configured: boolean;
    connected: boolean;
    lastSuccessExportAt?: string;
    exportRatePerMin: number;
    dropRate: number;
    lastError?: string;
  };
  prometheus: {
    configured: boolean;
    connected: boolean;
    scrapeEndpoint?: string;
    lastScrapeAt?: string;
  };
  logs: {
    configured: boolean;
    provider?: string;
    linkTemplateConfigured: boolean;
  };
  alerting: {
    configured: boolean;
    channels: string[];
    events: string[];
  };
}

// POST /api/observability/test response
export interface TestConnectionResult {
  success: boolean;
  message: string;
  latencyMs?: number;
}

// GET /api/observability/traces
export interface TraceEntry {
  traceId: string;
  executionId?: number;
  pipelineName: string;
  status: 'success' | 'error' | 'running';
  durationMs: number;
  spansCount: number;
  timestamp: string;
}

// GET /api/observability/insights
export interface SlowestStage {
  pipelineName: string;
  stageName: string;
  p95Ms: number;
}

export interface ErrorHotspot {
  pipelineName: string;
  stageName: string;
  failureRate: number; // 0-100
  avgRetries: number;
}

export interface InsightsSummary {
  executionsPerMin: number;
  failuresPerMin: number;
  successRate: number; // 0-100
  avgStageMs: number;
}

export interface ObservabilityInsights {
  slowestStages: SlowestStage[];
  errorHotspots: ErrorHotspot[];
  summary: InsightsSummary;
}

// Save config request
export interface SaveIntegrationConfigRequest {
  type: IntegrationType;
  config: Record<string, unknown>;
}

// Time range for queries
export type TimeRange = '15m' | '1h' | '6h' | '24h' | '7d';
