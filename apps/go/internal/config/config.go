package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDevDatabaseURL = "sqlite://../../data/pipelogiq.db"

	TopologyOwnershipServer = "server-owned"
	TopologyOwnershipClient = "client-owned"
)

type Common struct {
	AppID       string
	DatabaseURL string
	RabbitURL   string
	LogLevel    string
	MetricsAddr string
	// SecretsEncryptionKey encrypts integration secrets at rest (base64, 32 bytes).
	SecretsEncryptionKey string
	PublishRetry         struct {
		Base time.Duration
		Max  time.Duration
	}
}

type APIConfig struct {
	Common
	HTTPAddr                string
	ExternalHTTPAddr        string
	JWTSecret               string
	AllowedOrigins          []string
	GatewayVisibilityTTL    time.Duration
	GatewayMaxInFlight      int
	QueuePrefetch           int
	QueueTopologyOwnership  string
	QueueDLQEnabled         bool
	QueueDLQMessageTTL      time.Duration
	WorkerHeartbeatInterval time.Duration
	WorkerOfflineAfter      time.Duration
	WorkerSessionTTL        time.Duration
	WorkerEventsMaxBatch    int
	HealthLivenessEndpoint  string
	HealthReadyEndpoint     string
	AdminEmail              string
	AdminPasswordHash       string
	AdminFirstName          string
	AdminLastName           string
	BootstrapAppName        string
	BootstrapAppID          string
	BootstrapAPIKey         string
	// WorkerRabbitURL is the broker address handed to AMQP workers at bootstrap. It
	// should carry a least-privilege user, not the server's own credentials.
	WorkerRabbitURL string
	// WorkerBrokerHandout controls whether bootstrap includes a connection string at
	// all. When false, workers must be given PIPELOGIQ_RABBITMQ_URL locally.
	WorkerBrokerHandout bool
}

type WorkerConfig struct {
	Common
	PollInterval           time.Duration
	SchedulerPollInterval  time.Duration
	StageActiveTimeout     time.Duration
	Prefetch               int
	QueueTopologyOwnership string
	QueueDLQEnabled        bool
	QueueDLQMessageTTL     time.Duration
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
		JWTSecret:               strings.TrimSpace(os.Getenv("JWT_SECRET")),
		AllowedOrigins:          getCSVEnv("CORS_ALLOWED_ORIGINS", []string{"http://localhost:3300", "http://127.0.0.1:3300"}),
		GatewayVisibilityTTL:    getDuration("GATEWAY_VISIBILITY_TIMEOUT", time.Minute),
		GatewayMaxInFlight:      getInt("GATEWAY_MAX_INFLIGHT", 128),
		QueuePrefetch:           getInt("RABBIT_PREFETCH", 10),
		QueueTopologyOwnership:  getTopologyOwnership("RABBIT_TOPOLOGY_OWNERSHIP", TopologyOwnershipServer),
		QueueDLQEnabled:         getBool("RABBIT_DLQ_ENABLED", true),
		QueueDLQMessageTTL:      getDuration("RABBIT_DLQ_TTL", 30*time.Second),
		WorkerHeartbeatInterval: getDuration("WORKER_HEARTBEAT_INTERVAL", 15*time.Second),
		WorkerOfflineAfter:      getDuration("WORKER_OFFLINE_AFTER", 45*time.Second),
		WorkerSessionTTL:        getDuration("WORKER_SESSION_TTL", 24*time.Hour),
		WorkerEventsMaxBatch:    getInt("WORKER_EVENTS_MAX_BATCH", 200),
		HealthLivenessEndpoint:  getEnv("HEALTH_LIVENESS_PATH", "/healthz"),
		HealthReadyEndpoint:     getEnv("HEALTH_READY_PATH", "/readyz"),
		AdminEmail:              strings.TrimSpace(os.Getenv("ADMIN_EMAIL")),
		AdminPasswordHash:       strings.TrimSpace(os.Getenv("ADMIN_PASSWORD_HASH")),
		AdminFirstName:          getEnv("ADMIN_FIRST_NAME", "Admin"),
		AdminLastName:           getEnv("ADMIN_LAST_NAME", "User"),
		BootstrapAppName:        strings.TrimSpace(os.Getenv("BOOTSTRAP_APPLICATION_NAME")),
		BootstrapAppID:          strings.TrimSpace(os.Getenv("BOOTSTRAP_APPLICATION_APP_ID")),
		BootstrapAPIKey:         strings.TrimSpace(os.Getenv("BOOTSTRAP_API_KEY")),
		WorkerRabbitURL:         strings.TrimSpace(os.Getenv("WORKER_RABBITMQ_URL")),
		WorkerBrokerHandout:     getBool("WORKER_BROKER_HANDOUT", true),
	}

	// Metrics live on their own listener because nginx proxies the whole API surface.
	if cfg.MetricsAddr == "" {
		cfg.MetricsAddr = ":9091"
	}

	if cfg.JWTSecret == "" {
		return APIConfig{}, errors.New("JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return APIConfig{}, errors.New("JWT_SECRET must be at least 32 characters")
	}
	if isWellKnownJWTSecret(cfg.JWTSecret) {
		return APIConfig{}, errors.New("JWT_SECRET matches a published example value; generate a unique secret")
	}

	return cfg, nil
}

func LoadWorker() (WorkerConfig, error) {
	common, err := loadCommon()
	if err != nil {
		return WorkerConfig{}, err
	}

	cfg := WorkerConfig{
		Common:                 common,
		PollInterval:           getDuration("WORKER_POLL_INTERVAL", time.Second),
		SchedulerPollInterval:  getDuration("SCHEDULER_POLL_INTERVAL", time.Second),
		StageActiveTimeout:     getDurationCompat(5*time.Minute, "STAGE_ACTIVE_TIMEOUT", "STAGE_PENDING_TIMEOUT"),
		Prefetch:               getInt("RABBIT_PREFETCH", 5),
		QueueTopologyOwnership: getTopologyOwnership("RABBIT_TOPOLOGY_OWNERSHIP", TopologyOwnershipServer),
		QueueDLQEnabled:        getBool("RABBIT_DLQ_ENABLED", true),
		QueueDLQMessageTTL:     getDuration("RABBIT_DLQ_TTL", 30*time.Second),
	}

	return cfg, nil
}

// wellKnownJWTSecrets are example values published in the repository. Accepting one in a
// deployment would let anyone forge an administrator token, so startup fails instead.
var wellKnownJWTSecrets = []string{
	"local-dev-jwt-secret-0123456789abcdef",
	"change-me-to-a-long-random-secret-at-least-32-characters",
}

func isWellKnownJWTSecret(secret string) bool {
	for _, known := range wellKnownJWTSecrets {
		if secret == known {
			return true
		}
	}
	return false
}

func loadCommon() (Common, error) {
	appID := strings.TrimSpace(os.Getenv("APP_ID"))
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	rabbitURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))

	if appID == "" {
		return Common{}, errors.New("APP_ID is required")
	}
	// A missing DATABASE_URL used to fall back to a local SQLite file silently. In a
	// misconfigured deployment that meant running on a file inside the container and
	// losing every pipeline on restart. The fallback is now opt-in for development.
	if dbURL == "" {
		if !getBool("ALLOW_SQLITE_FALLBACK", false) {
			return Common{}, errors.New("DATABASE_URL is required (set ALLOW_SQLITE_FALLBACK=true for a local SQLite database)")
		}
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

		SecretsEncryptionKey: strings.TrimSpace(os.Getenv("SECRETS_ENCRYPTION_KEY")),
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

func getDurationCompat(def time.Duration, keys ...string) time.Duration {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			if d, err := time.ParseDuration(val); err == nil {
				return d
			}
		}
	}
	return def
}

func getTopologyOwnership(key, def string) string {
	def = strings.ToLower(strings.TrimSpace(def))
	if def != TopologyOwnershipClient {
		def = TopologyOwnershipServer
	}

	val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch val {
	case TopologyOwnershipServer, TopologyOwnershipClient:
		return val
	default:
		return def
	}
}

func getCSVEnv(key string, def []string) []string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return append([]string(nil), def...)
	}

	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), def...)
	}
	return out
}
