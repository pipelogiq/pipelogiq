# Observability

Pipelogiq bridges workflow execution data with external observability systems. It propagates trace context, exposes metrics, and provides integration config for common backends.

## Trace Context

Pipelogiq uses [W3C Trace Context](https://www.w3.org/TR/trace-context/) for distributed tracing:

- **`trace_id`** — a 32-hex-character identifier assigned to each pipeline execution, stored in the `pipeline` table
- **`span_id`** — a 16-hex-character identifier assigned to each stage, stored in the `stage` table

When a pipeline is created with a `traceparent` header or field, Pipelogiq extracts and stores the trace and span IDs. These are passed to workers in stage job payloads, allowing workers to continue the trace in their own spans.

### How "View Trace" works

The dashboard can link directly to traces in your tracing backend (Grafana Tempo, Jaeger, etc.). This is configured through the observability integration settings:

1. Configure an OpenTelemetry integration via the dashboard or API (`POST /observability/config`)
2. Set the trace link template, e.g.: `http://localhost:3000/explore?left=["now-1h","now","Tempo",{"query":"${traceId}"}]`
3. The dashboard substitutes `${traceId}` with the pipeline's trace ID to generate clickable links

## OpenTelemetry

Both `pipeline-api` and `pipeline-worker` initialize an OpenTelemetry trace exporter on startup. Configuration is via standard OTEL environment variables:

| Variable | Default | Description |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `tempo:4317` | OTLP collector endpoint |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` | `grpc` or `http` |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Skip TLS verification |
| `OTEL_TRACES_SAMPLER` | `parentbased_traceidratio` | Sampling strategy |
| `OTEL_TRACES_SAMPLER_ARG` | `1` | Sample ratio (1 = 100%) |
| `OTEL_SERVICE_NAME` | per-service | Service name in traces |

The Docker Compose stack includes [Grafana Tempo](https://grafana.com/oss/tempo/) as the trace backend, with Grafana pre-configured as a query frontend.

### Minimal setup with an external collector

If you run your own OpenTelemetry Collector, point the services at it:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=your-collector:4317
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
OTEL_EXPORTER_OTLP_INSECURE=false
```

The API and worker will export traces via OTLP gRPC. HTTP export is also supported by setting the protocol to `http`.

## Prometheus Metrics

Both services expose Prometheus-compatible metrics:

| Service | Endpoint | Port |
|---|---|---|
| pipeline-api | `/metrics` | 8080 |
| pipeline-worker | `/metrics` | 9090 |

### Available metrics

**Worker (pipeline-worker):**

| Metric | Type | Description |
|---|---|---|
| `stage_published_total` | Counter | Stages dispatched to MQ |
| `stage_result_processed_total` | Counter | Results processed successfully |
| `stage_result_failed_total` | Counter | Result processing failures |
| `stage_status_updated_total` | Counter | Status update messages processed |
| `pending_marked_failed_total` | Counter | Stages timed out in Pending |

**External API (pipeline-api):**

| Metric | Type | Description |
|---|---|---|
| `ext_pipeline_created_total` | Counter | Pipelines created via external API |
| `ext_stage_jobs_pulled_total` | Counter | Stage jobs pulled by workers |
| `ext_stage_jobs_acked_total` | Counter | Stage jobs acknowledged |
| `ext_stage_jobs_nacked_total` | Counter | Stage jobs rejected |

> **Note:** Metrics currently use counters only, without labels for application or handler. Histograms and labeled metrics are planned for a future release.
> The metrics endpoints remain available even if the dashboard "Prometheus integration" form is disabled/replaced by alerting configuration.

## Integration Config

The dashboard provides a UI to configure connections to external observability systems:

| Integration | Connection test | Status |
|---|---|---|
| OpenTelemetry | TCP dial to gRPC endpoint | Functional |
| Alerts | Optional HTTP health/webhook reachability | Functional (config + validation) |
| Grafana | — | Config storage only |
| Sentry | — | Config storage only |
| Datadog | — | Config storage only |
| Graylog | HTTP reachability | Functional |

Integration configs are stored in the `observability_integration_config` table. Health status (last tested, last success, last error) is tracked in `observability_integration_health`.

## Observability Insights

The API provides computed insights from pipeline execution data:

- **P95 stage duration** — 95th percentile execution time per stage handler
- **Error hotspots** — stages with the highest failure rates
- **Throughput** — pipeline/stage completion rates over time

Access via `GET /observability/insights` (internal API, requires auth).

## Alerting

The dashboard now includes an **Alerts** integration for routing operational notifications to external channels. It is intended as a lightweight replacement for the previous Prometheus UI config slot while preserving Prometheus-compatible `/metrics` endpoints on the API and worker.

Current delivery implementation sends notifications to:

- `telegram`
- `generic webhook`

Other channels listed below are supported at the config/UI level and can be wired in the same notifier pipeline.

### Telegram setup (step-by-step)

1. Create a bot in Telegram via **BotFather**
   - Open Telegram and find `@BotFather`
   - Run `/newbot`
   - Set bot name and username
   - Copy the bot token (looks like `123456789:AA...`)

2. Start a chat with your bot
   - Open your bot by username
   - Press **Start** (or send any message)
   - This is required so Telegram can deliver messages to your chat

3. Get your `chat_id`
   - In a browser or with `curl`, call:
   - `https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates`
   - Send a message to the bot first, then call again
   - Find `message.chat.id` in the JSON response

4. Configure Alerts in Pipelogiq UI
   - Open **Observability → Alerts**
   - Add channel configuration: `Telegram`
   - Paste `telegramBotToken`
   - Paste `telegramChatId`
   - Select alert events (for example: `stage_failed`, `stage_rerun_manual`, `stage_skipped_manual`, `worker_failed`)
   - Save configuration

5. Verify delivery
   - Trigger a manual stage rerun/skip from the UI, or cause a stage failure in test env
   - Confirm a message appears in Telegram

6. Security notes
   - Treat bot token as a secret
   - If token is leaked, revoke/rotate it in BotFather (`/revoke`)

#### Notes for group chats

- Add the bot to the group
- Send at least one message in the group (or mention the bot)
- Run `getUpdates` again and use the group `chat.id` (often a negative number)

### Supported alert channels (config)

- `telegram` (bot token + chat ID)
- `whatsapp` (provider webhook URL)
- `slack` (incoming webhook)
- `microsoft teams` (incoming webhook)
- `generic webhook` (POST JSON)
- `email` (recipient list)
- `pagerduty` (routing key)

### Recommended channels commonly connected in production

- Slack / Microsoft Teams (team-visible operational alerts)
- PagerDuty / Opsgenie (on-call paging and escalations)
- Email (audit trail and lower-priority notifications)
- Generic webhook (for internal alert-router/services)
- Discord (for non-prod/dev environments)
- SMS/voice via provider gateways (critical-only, after dedupe)

### Recommended alert events (pipeline + worker + policy)

- **Stage failed**
- **Manual stage rerun**
- **Manual stage skip**
- **Worker started / stopped / failed**
- **Worker heartbeat lost**
- **Policy triggered**

### Additional useful alert events

- Pipeline failed (final status = failed)
- Pipeline stuck / SLA timeout exceeded
- Retry storm (same stage failing repeatedly)
- DLQ message detected
- Queue backlog high (RabbitMQ depth threshold)
- Consecutive worker registration/heartbeat failures
- Policy changed / disabled / deleted (audit-sensitive environments)
- Integration connectivity checks failing repeatedly (OTel/logs/alerts webhook)

### Delivery hooks (where to wire notifications)

The current codebase already records the main event sources needed for notifications:

- Stage lifecycle changes: `apps/go/internal/store/logs.go` (`LogStageChange`) and stage transitions in `apps/go/internal/store/store.go` / `apps/go/internal/store/pipeline_ext.go`
- Worker lifecycle/events: `apps/go/internal/store/workers.go` (`insertWorkerEvent`, `worker.bootstrap`, `worker.state_changed`, `worker.stopped`)
- Policy audit/trigger events: `apps/go/internal/api/policies.go` (`appendEventLocked`)

These are the recommended integration points for emitting alert notifications.

> **Note:** `policy_changed` notifications are emitted from policy audit events today. `policy_triggered` notifications are supported by the alert notifier, but require the runtime policy engine to append `triggered` events (the current in-memory policy repository does not yet emit them).
