package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// Init changes OTel globals and reads process configuration, so these tests must
// stay serial and restore both. Never inherit an external collector or headers.
func prepareTelemetryTest(t *testing.T) {
	t.Helper()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		if otel.GetTracerProvider() != previousProvider {
			otel.SetTracerProvider(previousProvider)
		}
		otel.SetTextMapPropagator(previousPropagator)
	})
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("APP_ID", "regression-app")
	t.Setenv("APP_ENV", "test")
	t.Setenv("APP_VERSION", "test-version")
}

func initExportTest(t *testing.T, endpoint, protocol string) *sdktrace.TracerProvider {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Bearer regression-token,x-test-scope=pipeline")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	shutdown, err := Init(context.Background(), "pipelogiq-export-regression", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			t.Errorf("telemetry shutdown: %v", err)
		}
	})
	provider, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("Init did not install an SDK tracer provider")
	}
	return provider
}

func recordExportTestSpan(t *testing.T, provider *sdktrace.TracerProvider) trace.SpanContext {
	t.Helper()
	_, span := otel.Tracer("pipelogiq/telemetry-regression").Start(context.Background(), "stage.process", trace.WithSpanKind(trace.SpanKindConsumer))
	span.SetAttributes(attribute.Int("pipeline.id", 701), attribute.String("stage.name", "charge-payment"))
	span.AddEvent("stage.failed", trace.WithAttributes(attribute.String("error.code", "test-timeout")))
	span.SetStatus(codes.Error, "test failure")
	spanContext := span.SpanContext()
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush: %v", err)
	}
	return spanContext
}

func assertExportedSpan(t *testing.T, request *collectortrace.ExportTraceServiceRequest, expected trace.SpanContext) {
	t.Helper()
	if len(request.ResourceSpans) != 1 || len(request.ResourceSpans[0].ScopeSpans) != 1 || len(request.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
		t.Fatalf("expected one resource, instrumentation scope and span: %v", request)
	}
	resource := request.ResourceSpans[0]
	for key, want := range map[string]string{
		"service.name": "pipelogiq-export-regression", "pipelogiq.app_id": "regression-app",
		"deployment.environment": "test", "service.version": "test-version",
	} {
		if got := stringAttribute(resource.Resource.Attributes, key); got != want {
			t.Errorf("resource %s = %q, want %q", key, got, want)
		}
	}
	scope := resource.ScopeSpans[0]
	if scope.Scope.Name != "pipelogiq/telemetry-regression" {
		t.Errorf("instrumentation scope = %q", scope.Scope.Name)
	}
	span := scope.Spans[0]
	traceID, spanID := expected.TraceID(), expected.SpanID()
	if span.Name != "stage.process" || !bytes.Equal(span.TraceId, traceID[:]) || !bytes.Equal(span.SpanId, spanID[:]) {
		t.Errorf("span identity changed during export: %v", span)
	}
	if span.Kind != tracepb.Span_SPAN_KIND_CONSUMER || span.Status.Code != tracepb.Status_STATUS_CODE_ERROR || span.Status.Message != "test failure" {
		t.Errorf("span kind/status changed during export: %v", span)
	}
	if span.StartTimeUnixNano == 0 || span.EndTimeUnixNano < span.StartTimeUnixNano {
		t.Errorf("invalid exported timestamps: %v", span)
	}
	if stringAttribute(span.Attributes, "stage.name") != "charge-payment" {
		t.Errorf("missing stage attribute: %v", span.Attributes)
	}
	if len(span.Events) != 1 || span.Events[0].Name != "stage.failed" || stringAttribute(span.Events[0].Attributes, "error.code") != "test-timeout" {
		t.Errorf("span event lost during export: %v", span.Events)
	}
}

func stringAttribute(attributes []*commonpb.KeyValue, key string) string {
	for _, attribute := range attributes {
		if attribute.Key == key {
			return attribute.Value.GetStringValue()
		}
	}
	return ""
}

func TestOTLPHTTPExportsRecordedSpans(t *testing.T) {
	for _, path := range []string{"", "/collector/custom/traces"} {
		t.Run("path="+path, func(t *testing.T) {
			prepareTelemetryTest(t)
			type receivedExport struct {
				request                                         *collectortrace.ExportTraceServiceRequest
				path, method, contentType, authorization, scope string
				err                                             error
			}
			received := make(chan receivedExport, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				exported := receivedExport{
					request: &collectortrace.ExportTraceServiceRequest{},
					path:    r.URL.Path, method: r.Method, contentType: r.Header.Get("Content-Type"),
					authorization: r.Header.Get("Authorization"), scope: r.Header.Get("X-Test-Scope"),
				}
				body, err := io.ReadAll(r.Body)
				if err == nil {
					err = proto.Unmarshal(body, exported.request)
				}
				exported.err = err
				received <- exported
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)
			provider := initExportTest(t, server.URL+path, "http/protobuf")
			spanContext := recordExportTestSpan(t, provider)
			select {
			case exported := <-received:
				wantPath := path
				if wantPath == "" {
					wantPath = "/v1/traces"
				}
				if exported.err != nil || exported.path != wantPath || exported.method != http.MethodPost || exported.contentType != "application/x-protobuf" {
					t.Fatalf("unexpected OTLP HTTP request: path=%q method=%q contentType=%q error=%v", exported.path, exported.method, exported.contentType, exported.err)
				}
				if exported.authorization != "Bearer regression-token" || exported.scope != "pipeline" {
					t.Error("configured collector headers were not exported")
				}
				assertExportedSpan(t, exported.request, spanContext)
			default:
				t.Fatal("ForceFlush returned without delivering the span to the HTTP collector")
			}
		})
	}
}

type regressionGRPCCollector struct {
	collectortrace.UnimplementedTraceServiceServer
	received chan *collectortrace.ExportTraceServiceRequest
	metadata chan metadata.MD
}

func (c *regressionGRPCCollector) Export(ctx context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	c.metadata <- md.Copy()
	c.received <- request
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func TestOTLPGRPCExportsRecordedSpans(t *testing.T) {
	prepareTelemetryTest(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collector := &regressionGRPCCollector{
		received: make(chan *collectortrace.ExportTraceServiceRequest, 1),
		metadata: make(chan metadata.MD, 1),
	}
	collectortrace.RegisterTraceServiceServer(server, collector)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	provider := initExportTest(t, "http://"+listener.Addr().String(), "grpc")
	spanContext := recordExportTestSpan(t, provider)
	select {
	case request := <-collector.received:
		md := <-collector.metadata
		if fmt.Sprint(md.Get("authorization")) != "[Bearer regression-token]" || fmt.Sprint(md.Get("x-test-scope")) != "[pipeline]" {
			t.Error("configured collector headers were not exported as gRPC metadata")
		}
		assertExportedSpan(t, request, spanContext)
	default:
		t.Fatal("ForceFlush returned without delivering the span to the gRPC collector")
	}
}
