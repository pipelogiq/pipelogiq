package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	observabilitymodel "pipelogiq/internal/observability/model"
	observabilityrepo "pipelogiq/internal/observability/repo"
	"pipelogiq/internal/store"
	"pipelogiq/internal/types"
)

const (
	defaultHTTPTimeout  = 4 * time.Second
	configCacheTTL      = 5 * time.Second
	defaultDedupeWindow = 5 * time.Minute
)

type Notifier struct {
	repo   observabilityrepo.Repository
	logger *slog.Logger
	client *http.Client

	mu          sync.Mutex
	cachedCfg   runtimeConfig
	cacheLoaded time.Time
	recentSent  map[string]time.Time
}

type runtimeConfig struct {
	enabled            bool
	enabledEvents      map[string]struct{}
	telegramEnabled    bool
	telegramBotToken   string
	telegramChatID     string
	webhookEnabled     bool
	webhookURL         string
	dedupeWindow       time.Duration
	sendResolved       bool
	configuredChannels []string
}

type outboundAlert struct {
	Event       string         `json:"event"`
	Title       string         `json:"title"`
	Message     string         `json:"message"`
	Severity    string         `json:"severity"`
	Timestamp   string         `json:"timestamp"`
	DedupeKey   string         `json:"dedupeKey,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	ChannelHint []string       `json:"channels,omitempty"`
}

var _ store.AlertSink = (*Notifier)(nil)

func New(repo observabilityrepo.Repository, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		repo:   repo,
		logger: logger,
		client: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		recentSent: make(map[string]time.Time),
	}
}

func (n *Notifier) NotifyStageChange(ctx context.Context, event store.StageAlertEvent) {
	alert, ok := mapStageEvent(event)
	if !ok {
		return
	}
	n.dispatch(ctx, alert)
}

func (n *Notifier) NotifyWorkerEvent(ctx context.Context, event store.WorkerAlertEvent) {
	alert, ok := mapWorkerEvent(event)
	if !ok {
		return
	}
	n.dispatch(ctx, alert)
}

func (n *Notifier) NotifyPolicyEvent(ctx context.Context, event types.PolicyEvent) {
	alert, ok := mapPolicyEvent(event)
	if !ok {
		return
	}
	n.dispatch(ctx, alert)
}

func (n *Notifier) SendTestAlert(ctx context.Context) error {
	cfg, err := n.loadConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.telegramEnabled && !cfg.webhookEnabled {
		return errors.New("no supported alert channels configured (telegram/webhook)")
	}

	alert := outboundAlert{
		Event:     "test_alert",
		Title:     "Pipelogiq test alert",
		Message:   "This is a test alert from Pipelogiq",
		Severity:  "info",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Details: map[string]any{
			"source": "observability.test",
		},
		ChannelHint: cfg.configuredChannels,
	}

	if cfg.telegramEnabled {
		if err := n.sendTelegram(ctx, cfg, alert); err != nil {
			return fmt.Errorf("telegram: %w", err)
		}
	}
	if cfg.webhookEnabled {
		if err := n.sendWebhook(ctx, cfg, alert); err != nil {
			return fmt.Errorf("webhook: %w", err)
		}
	}

	return nil
}

func (n *Notifier) dispatch(ctx context.Context, alert outboundAlert) {
	cfg, err := n.loadConfig(ctx)
	if err != nil {
		n.logger.Error("alerts config load failed", "err", err)
		return
	}
	if !cfg.enabled {
		return
	}
	if _, ok := cfg.enabledEvents[alert.Event]; !ok {
		return
	}
	if alert.DedupeKey != "" && cfg.dedupeWindow > 0 && n.shouldSuppress(alert.DedupeKey, cfg.dedupeWindow) {
		return
	}

	alert.ChannelHint = cfg.configuredChannels

	if cfg.telegramEnabled {
		if err := n.sendTelegram(ctx, cfg, alert); err != nil {
			n.logger.Error("telegram alert send failed", "err", err, "event", alert.Event)
		}
	}
	if cfg.webhookEnabled {
		if err := n.sendWebhook(ctx, cfg, alert); err != nil {
			n.logger.Error("webhook alert send failed", "err", err, "event", alert.Event)
		}
	}
}

func (n *Notifier) loadConfig(ctx context.Context) (runtimeConfig, error) {
	n.mu.Lock()
	if time.Since(n.cacheLoaded) <= configCacheTTL {
		cfg := n.cachedCfg
		n.mu.Unlock()
		return cfg, nil
	}
	n.mu.Unlock()

	integration, err := n.repo.GetIntegration(ctx, observabilitymodel.IntegrationTypeAlerting)
	if err != nil {
		return runtimeConfig{}, err
	}
	if integration == nil {
		cfg := runtimeConfig{}
		n.storeCachedConfig(cfg)
		return cfg, nil
	}

	cfg := parseRuntimeConfig(integration.Config)
	n.storeCachedConfig(cfg)
	return cfg, nil
}

func (n *Notifier) storeCachedConfig(cfg runtimeConfig) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cachedCfg = cfg
	n.cacheLoaded = time.Now().UTC()
}

func parseRuntimeConfig(config map[string]any) runtimeConfig {
	channels := parseStringList(config["channels"])
	events := parseStringList(config["enabledEvents"])
	eventSet := make(map[string]struct{}, len(events))
	for _, event := range events {
		eventSet[event] = struct{}{}
	}
	channelSet := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		channelSet[ch] = struct{}{}
	}

	telegramToken := parseString(config["telegramBotToken"])
	telegramChatID := parseString(config["telegramChatId"])
	webhookURL := parseString(config["webhookUrl"])
	dedupeWindow := defaultDedupeWindow
	if raw, ok := parseFloat(config["dedupeWindowSeconds"]); ok && raw > 0 {
		dedupeWindow = time.Duration(raw * float64(time.Second))
	}
	if raw, ok := parseFloat(config["dedupeWindowSeconds"]); ok && raw <= 0 {
		dedupeWindow = 0
	}
	sendResolved, _ := parseBool(config["sendResolved"])

	cfg := runtimeConfig{
		enabledEvents: eventSet,
		dedupeWindow:  dedupeWindow,
		sendResolved:  sendResolved,
	}

	if _, ok := channelSet["telegram"]; ok && telegramToken != "" && telegramChatID != "" {
		cfg.telegramEnabled = true
		cfg.telegramBotToken = telegramToken
		cfg.telegramChatID = telegramChatID
		cfg.configuredChannels = append(cfg.configuredChannels, "telegram")
	}
	if _, ok := channelSet["webhook"]; ok && webhookURL != "" {
		cfg.webhookEnabled = true
		cfg.webhookURL = webhookURL
		cfg.configuredChannels = append(cfg.configuredChannels, "webhook")
	}

	cfg.enabled = len(cfg.enabledEvents) > 0 && (cfg.telegramEnabled || cfg.webhookEnabled)
	return cfg
}

func (n *Notifier) shouldSuppress(key string, window time.Duration) bool {
	now := time.Now().UTC()
	n.mu.Lock()
	defer n.mu.Unlock()

	for k, ts := range n.recentSent {
		if now.Sub(ts) > window {
			delete(n.recentSent, k)
		}
	}

	if ts, ok := n.recentSent[key]; ok && now.Sub(ts) <= window {
		return true
	}
	n.recentSent[key] = now
	return false
}

func (n *Notifier) sendTelegram(ctx context.Context, cfg runtimeConfig, alert outboundAlert) error {
	payload := map[string]any{
		"chat_id": cfg.telegramChatID,
		"text":    formatTelegramText(alert),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultHTTPTimeout)
	defer cancel()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.telegramBotToken)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram status %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendWebhook(ctx context.Context, cfg runtimeConfig, alert outboundAlert) error {
	payload := map[string]any{
		"source":  "pipelogiq",
		"channel": "webhook",
		"alert":   alert,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

func mapStageEvent(event store.StageAlertEvent) (outboundAlert, bool) {
	ts := event.TS.UTC().Format(time.RFC3339)
	baseDetails := map[string]any{
		"pipelineId":   event.PipelineID,
		"pipelineName": strings.TrimSpace(event.PipelineName),
		"stageId":      event.StageID,
		"stageName":    strings.TrimSpace(event.StageName),
		"oldStatus":    event.OldStatus,
		"newStatus":    event.NewStatus,
		"source":       event.Source,
	}

	switch {
	case strings.EqualFold(event.NewStatus, types.StageStatusFailed):
		return outboundAlert{
			Event:     "stage_failed",
			Title:     "Stage failed",
			Message:   fmt.Sprintf("Pipeline %d stage %d failed (%s)", event.PipelineID, event.StageID, strings.TrimSpace(event.StageName)),
			Severity:  "error",
			Timestamp: ts,
			DedupeKey: fmt.Sprintf("stage_failed:%d:%d", event.PipelineID, event.StageID),
			Details:   baseDetails,
		}, true
	case strings.EqualFold(event.Source, "rerun_stage"):
		return outboundAlert{
			Event:     "stage_rerun_manual",
			Title:     "Stage rerun (manual)",
			Message:   fmt.Sprintf("Pipeline %d stage %d rerun manually", event.PipelineID, event.StageID),
			Severity:  "info",
			Timestamp: ts,
			DedupeKey: fmt.Sprintf("stage_rerun_manual:%d:%d:%s", event.PipelineID, event.StageID, ts),
			Details:   baseDetails,
		}, true
	case strings.EqualFold(event.Source, "skip_stage") && strings.EqualFold(event.NewStatus, types.StageStatusSkipped):
		return outboundAlert{
			Event:     "stage_skipped_manual",
			Title:     "Stage skipped (manual)",
			Message:   fmt.Sprintf("Pipeline %d stage %d skipped manually", event.PipelineID, event.StageID),
			Severity:  "warning",
			Timestamp: ts,
			DedupeKey: fmt.Sprintf("stage_skipped_manual:%d:%d:%s", event.PipelineID, event.StageID, ts),
			Details:   baseDetails,
		}, true
	default:
		return outboundAlert{}, false
	}
}

func mapWorkerEvent(event store.WorkerAlertEvent) (outboundAlert, bool) {
	level := strings.ToUpper(strings.TrimSpace(event.Level))
	eventType := strings.TrimSpace(event.EventType)
	ts := event.TS.UTC().Format(time.RFC3339)
	details := cloneMap(event.Details)
	if details == nil {
		details = map[string]any{}
	}
	details["workerId"] = event.WorkerID
	details["eventType"] = eventType
	details["level"] = level

	switch eventType {
	case "worker.bootstrap":
		return outboundAlert{
			Event:     "worker_started",
			Title:     "Worker started",
			Message:   fmt.Sprintf("Worker %s started", event.WorkerID),
			Severity:  "info",
			Timestamp: ts,
			DedupeKey: fmt.Sprintf("worker_started:%s:%s", event.WorkerID, ts),
			Details:   details,
		}, true
	case "worker.stopped":
		return outboundAlert{
			Event:     "worker_stopped",
			Title:     "Worker stopped",
			Message:   fmt.Sprintf("Worker %s stopped", event.WorkerID),
			Severity:  "warning",
			Timestamp: ts,
			DedupeKey: fmt.Sprintf("worker_stopped:%s:%s", event.WorkerID, ts),
			Details:   details,
		}, true
	case "worker.state_changed":
		toState := strings.ToLower(parseString(details["to"]))
		fromState := strings.ToLower(parseString(details["from"]))
		switch toState {
		case types.WorkerStateReady:
			if fromState == types.WorkerStateStarting {
				return outboundAlert{
					Event:     "worker_started",
					Title:     "Worker ready",
					Message:   fmt.Sprintf("Worker %s is ready", event.WorkerID),
					Severity:  "info",
					Timestamp: ts,
					DedupeKey: fmt.Sprintf("worker_ready:%s:%s", event.WorkerID, ts),
					Details:   details,
				}, true
			}
		case types.WorkerStateError:
			return outboundAlert{
				Event:     "worker_failed",
				Title:     "Worker failed",
				Message:   fmt.Sprintf("Worker %s entered error state", event.WorkerID),
				Severity:  "error",
				Timestamp: ts,
				DedupeKey: fmt.Sprintf("worker_failed:%s:%s", event.WorkerID, toState),
				Details:   details,
			}, true
		case types.WorkerStateOffline:
			return outboundAlert{
				Event:     "worker_heartbeat_lost",
				Title:     "Worker heartbeat lost",
				Message:   fmt.Sprintf("Worker %s is offline", event.WorkerID),
				Severity:  "error",
				Timestamp: ts,
				DedupeKey: fmt.Sprintf("worker_offline:%s", event.WorkerID),
				Details:   details,
			}, true
		case types.WorkerStateStopped:
			return outboundAlert{
				Event:     "worker_stopped",
				Title:     "Worker stopped",
				Message:   fmt.Sprintf("Worker %s stopped", event.WorkerID),
				Severity:  "warning",
				Timestamp: ts,
				DedupeKey: fmt.Sprintf("worker_stopped:%s", event.WorkerID),
				Details:   details,
			}, true
		}
	}

	if level == "ERROR" {
		return outboundAlert{
			Event:     "worker_failed",
			Title:     "Worker error event",
			Message:   fmt.Sprintf("Worker %s reported an error event", event.WorkerID),
			Severity:  "error",
			Timestamp: ts,
			DedupeKey: fmt.Sprintf("worker_error:%s:%s:%s", event.WorkerID, eventType, strings.TrimSpace(event.Message)),
			Details:   details,
		}, true
	}

	return outboundAlert{}, false
}

func mapPolicyEvent(event types.PolicyEvent) (outboundAlert, bool) {
	ts := event.TS.UTC().Format(time.RFC3339)
	details := cloneMap(event.Details)
	if details == nil {
		details = map[string]any{}
	}
	details["policyId"] = event.PolicyID
	details["actor"] = event.Actor
	details["eventType"] = string(event.Type)

	if event.Type == types.PolicyEventTypeTriggered {
		return outboundAlert{
			Event:     "policy_triggered",
			Title:     "Policy triggered",
			Message:   fmt.Sprintf("Policy %s triggered", event.PolicyID),
			Severity:  "warning",
			Timestamp: ts,
			DedupeKey: fmt.Sprintf("policy_triggered:%s:%s", event.PolicyID, ts),
			Details:   details,
		}, true
	}

	switch event.Type {
	case types.PolicyEventTypeCreated,
		types.PolicyEventTypeUpdated,
		types.PolicyEventTypeEnabled,
		types.PolicyEventTypeDisabled,
		types.PolicyEventTypePaused,
		types.PolicyEventTypeResumed,
		types.PolicyEventTypeDeleted:
		return outboundAlert{
			Event:     "policy_changed",
			Title:     "Policy changed",
			Message:   fmt.Sprintf("Policy %s changed (%s)", event.PolicyID, event.Type),
			Severity:  "info",
			Timestamp: ts,
			DedupeKey: fmt.Sprintf("policy_changed:%s:%s", event.PolicyID, ts),
			Details:   details,
		}, true
	default:
		return outboundAlert{}, false
	}
}

func formatTelegramText(alert outboundAlert) string {
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(strings.ToUpper(alert.Severity))
	b.WriteString("] ")
	b.WriteString(alert.Title)
	b.WriteString("\n")
	b.WriteString(alert.Message)
	b.WriteString("\n")
	b.WriteString("event: ")
	b.WriteString(alert.Event)
	b.WriteString("\n")
	b.WriteString("time: ")
	b.WriteString(alert.Timestamp)

	if len(alert.Details) > 0 {
		if value, ok := alert.Details["pipelineId"]; ok {
			fmt.Fprintf(&b, "\npipelineId: %v", value)
		}
		if value, ok := alert.Details["stageId"]; ok {
			fmt.Fprintf(&b, "\nstageId: %v", value)
		}
		if value, ok := alert.Details["workerId"]; ok {
			fmt.Fprintf(&b, "\nworkerId: %v", value)
		}
		if value, ok := alert.Details["policyId"]; ok {
			fmt.Fprintf(&b, "\npolicyId: %v", value)
		}
	}

	return b.String()
}

func parseString(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func parseBool(raw any) (bool, bool) {
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func parseFloat(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func parseStringList(raw any) []string {
	out := make([]string, 0)
	seen := map[string]struct{}{}
	appendValue := func(v string) {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	switch value := raw.(type) {
	case string:
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
			appendValue(part)
		}
	case []string:
		for _, part := range value {
			appendValue(part)
		}
	case []any:
		for _, part := range value {
			if s, ok := part.(string); ok {
				appendValue(s)
			}
		}
	}

	return out
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
