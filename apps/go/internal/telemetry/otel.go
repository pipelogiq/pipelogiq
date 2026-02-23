package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	defaultOTLPGRPCPort = "4317"
	defaultOTLPHTTPPort = "4318"
	defaultHTTPPath     = "/v1/traces"
)

// Init configures OpenTelemetry tracing for the running service.
// It is a no-op when OTEL_EXPORTER_OTLP_ENDPOINT is not set.
func Init(ctx context.Context, serviceName string, logger *slog.Logger) (func(context.Context) error, error) {
	if logger == nil {
		logger = slog.Default()
	}

	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		otel.SetTextMapPropagator(
			propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			),
		)
		logger.Info("opentelemetry disabled", "reason", "OTEL_EXPORTER_OTLP_ENDPOINT not set")
		return func(context.Context) error { return nil }, nil
	}

	protocol := normalizeProtocol(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))
	headers := parseHeadersEnv(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))

	exporter, resolvedEndpoint, err := newExporter(ctx, protocol, endpoint, headers)
	if err != nil {
		return nil, err
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", strings.TrimSpace(serviceName)),
	}
	if appID := strings.TrimSpace(os.Getenv("APP_ID")); appID != "" {
		attrs = append(attrs, attribute.String("pipelogiq.app_id", appID))
	}
	if env := strings.TrimSpace(os.Getenv("APP_ENV")); env != "" {
		attrs = append(attrs, attribute.String("deployment.environment", env))
	}
	if version := strings.TrimSpace(os.Getenv("APP_VERSION")); version != "" {
		attrs = append(attrs, attribute.String("service.version", version))
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("", attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(buildSampler()),
		sdktrace.WithBatcher(exporter),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	logger.Info(
		"opentelemetry tracing enabled",
		"service", serviceName,
		"protocol", protocol,
		"endpoint", resolvedEndpoint,
	)

	return tp.Shutdown, nil
}

func newExporter(
	ctx context.Context,
	protocol string,
	rawEndpoint string,
	headers map[string]string,
) (sdktrace.SpanExporter, string, error) {
	switch protocol {
	case "http":
		host, path, insecure, err := normalizeHTTPEndpoint(rawEndpoint)
		if err != nil {
			return nil, "", fmt.Errorf("invalid OTLP HTTP endpoint: %w", err)
		}
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(host),
			otlptracehttp.WithURLPath(path),
			otlptracehttp.WithTimeout(5 * time.Second),
		}
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(headers))
		}
		exporter, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, "", fmt.Errorf("create OTLP HTTP exporter: %w", err)
		}
		return exporter, host + path, nil
	default:
		hostPort, insecure, err := normalizeGRPCEndpoint(rawEndpoint)
		if err != nil {
			return nil, "", fmt.Errorf("invalid OTLP gRPC endpoint: %w", err)
		}
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(hostPort),
			otlptracegrpc.WithTimeout(5 * time.Second),
		}
		if insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(headers))
		}
		exporter, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, "", fmt.Errorf("create OTLP gRPC exporter: %w", err)
		}
		return exporter, hostPort, nil
	}
}

func normalizeProtocol(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "grpc":
		return "grpc"
	case "http", "http/protobuf":
		return "http"
	default:
		return "grpc"
	}
}

func normalizeGRPCEndpoint(raw string) (hostPort string, insecure bool, err error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", false, fmt.Errorf("endpoint is required")
	}

	if strings.Contains(candidate, "://") {
		parsed, parseErr := url.Parse(candidate)
		if parseErr != nil {
			return "", false, parseErr
		}
		if parsed.Host == "" {
			return "", false, fmt.Errorf("host is required")
		}
		candidate = parsed.Host
		if parsed.Scheme == "http" {
			insecure = true
		}
	}

	if !strings.Contains(candidate, ":") {
		candidate = candidate + ":" + defaultOTLPGRPCPort
	}

	if value, ok := parseBoolEnv("OTEL_EXPORTER_OTLP_INSECURE"); ok {
		insecure = value
	} else if !strings.Contains(raw, "://") {
		// Docker-local default; explicit env still overrides.
		insecure = true
	}

	return candidate, insecure, nil
}

func normalizeHTTPEndpoint(raw string) (host string, path string, insecure bool, err error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", "", false, fmt.Errorf("endpoint is required")
	}

	path = defaultHTTPPath
	if strings.Contains(candidate, "://") {
		parsed, parseErr := url.Parse(candidate)
		if parseErr != nil {
			return "", "", false, parseErr
		}
		if parsed.Host == "" {
			return "", "", false, fmt.Errorf("host is required")
		}
		host = parsed.Host
		if parsed.Path != "" && parsed.Path != "/" {
			path = parsed.Path
		}
		if parsed.Scheme == "http" {
			insecure = true
		}
		if parsed.Scheme == "https" {
			insecure = false
		}
	} else {
		host = candidate
		if !strings.Contains(host, ":") {
			host = host + ":" + defaultOTLPHTTPPort
		}
		insecure = true
	}

	if value, ok := parseBoolEnv("OTEL_EXPORTER_OTLP_INSECURE"); ok {
		insecure = value
	}

	return host, path, insecure, nil
}

func buildSampler() sdktrace.Sampler {
	ratio := 1.0
	if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			ratio = clamp(parsed, 0, 1)
		}
	}

	samplerName := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	switch samplerName {
	case "always_off", "alwaysoff":
		return sdktrace.NeverSample()
	case "always_on", "alwayson":
		return sdktrace.AlwaysSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratio)
	case "", "parentbased_traceidratio":
		fallthrough
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	}
}

func parseHeadersEnv(raw string) map[string]string {
	headers := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			continue
		}
		headers[key] = value
	}
	return headers
}

func parseBoolEnv(key string) (bool, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return parsed, true
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
