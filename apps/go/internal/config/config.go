package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultDevDatabaseURL = "sqlite://../../data/pipelogiq.db"

type Common struct {
	AppID        string
	DatabaseURL  string
	RabbitURL    string
	LogLevel     string
	MetricsAddr  string
	PublishRetry struct {
		Base time.Duration
		Max  time.Duration
	}
}

type APIConfig struct {
	Common
	HTTPAddr                string
	ExternalHTTPAddr        string
	GatewayVisibilityTTL    time.Duration
	GatewayMaxInFlight      int
	QueuePrefetch           int
	QueueDLQEnabled         bool
	QueueDLQMessageTTL      time.Duration
	WorkerHeartbeatInterval time.Duration
	WorkerOfflineAfter      time.Duration
	WorkerSessionTTL        time.Duration
	WorkerEventsMaxBatch    int
	HealthLivenessEndpoint  string
	HealthReadyEndpoint     string
}

type WorkerConfig struct {
	Common
	PollInterval        time.Duration
	StagePendingTimeout time.Duration
	Prefetch            int
	QueueDLQEnabled     bool
	QueueDLQMessageTTL  time.Duration
}

func LoadAPI() (APIConfig, error) {
	common, err := loadCommon()
	if err != nil {
		return APIConfig{}, err
	}

	cfg := APIConfig{
		Common:                  common,
		HTTPAddr:                getEnv("HTTP_ADDR", ":8080"),
		ExternalHTTPAddr:        getEnv("EXTERNAL_HTTP_ADDR", ":8081"),
		GatewayVisibilityTTL:    getDuration("GATEWAY_VISIBILITY_TIMEOUT", time.Minute),
		GatewayMaxInFlight:      getInt("GATEWAY_MAX_INFLIGHT", 128),
		QueuePrefetch:           getInt("RABBIT_PREFETCH", 10),
		QueueDLQEnabled:         getBool("RABBIT_DLQ_ENABLED", true),
		QueueDLQMessageTTL:      getDuration("RABBIT_DLQ_TTL", 30*time.Second),
		WorkerHeartbeatInterval: getDuration("WORKER_HEARTBEAT_INTERVAL", 15*time.Second),
		WorkerOfflineAfter:      getDuration("WORKER_OFFLINE_AFTER", 45*time.Second),
		WorkerSessionTTL:        getDuration("WORKER_SESSION_TTL", 24*time.Hour),
		WorkerEventsMaxBatch:    getInt("WORKER_EVENTS_MAX_BATCH", 200),
		HealthLivenessEndpoint:  getEnv("HEALTH_LIVENESS_PATH", "/healthz"),
		HealthReadyEndpoint:     getEnv("HEALTH_READY_PATH", "/readyz"),
	}

	return cfg, nil
}

func LoadWorker() (WorkerConfig, error) {
	common, err := loadCommon()
	if err != nil {
		return WorkerConfig{}, err
	}

	cfg := WorkerConfig{
		Common:              common,
		PollInterval:        getDuration("WORKER_POLL_INTERVAL", time.Second),
		StagePendingTimeout: getDuration("STAGE_PENDING_TIMEOUT", 5*time.Minute),
		Prefetch:            getInt("RABBIT_PREFETCH", 5),
		QueueDLQEnabled:     getBool("RABBIT_DLQ_ENABLED", true),
		QueueDLQMessageTTL:  getDuration("RABBIT_DLQ_TTL", 30*time.Second),
	}

	return cfg, nil
}

func loadCommon() (Common, error) {
	appID := firstNonEmpty(
		os.Getenv("APP_ID"),
		os.Getenv("APP_ENV__APPID"),
		os.Getenv("APPENV__APPID"),
		os.Getenv("APPENV_APPID"),
		os.Getenv("APPENV__APP_ID"),
		os.Getenv("APPENV_APP_ID"),
	)

	dbURL := firstNonEmpty(
		os.Getenv("DATABASE_URL"),
		os.Getenv("CONNECTIONSTRINGS__DATABASE"),
		os.Getenv("CONNECTION_STRINGS__DATABASE"),
	)

	rabbitURL := firstNonEmpty(
		os.Getenv("RABBITMQ_URL"),
		os.Getenv("CONNECTIONSTRINGS__MESSAGEBROKER"),
		os.Getenv("MESSAGE_BROKER_URL"),
		os.Getenv("MESSAGEBROKER__CONNECTIONSTRING"),
		os.Getenv("CONNECTIONSTRINGS__MESSAGEBROKER"),
	)

	if appID == "" {
		return Common{}, errors.New("APP_ID is required")
	}
	if dbURL == "" {
		dbURL = defaultDevDatabaseURL
	}
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
	}

	logLevel := strings.ToLower(getEnv("LOG_LEVEL", "info"))

	common := Common{
		AppID:       appID,
		DatabaseURL: dbURL,
		RabbitURL:   rabbitURL,
		LogLevel:    logLevel,
		MetricsAddr: getEnv("METRICS_ADDR", ""),
	}
	common.PublishRetry.Base = getDuration("RABBIT_RETRY_BASE", 500*time.Millisecond)
	common.PublishRetry.Max = getDuration("RABBIT_RETRY_MAX", 30*time.Second)

	return common, nil
}

func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

func getInt(key string, def int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return def
}

func getBool(key string, def bool) bool {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.ParseBool(val); err == nil {
			return parsed
		}
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return def
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
