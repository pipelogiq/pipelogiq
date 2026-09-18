package telemetry

import (
	"context"
	"io"
	"log/slog"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

func initPropagationTest(t *testing.T) {
	t.Helper()
	prepareTelemetryTest(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "pipelogiq-propagation-regression", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := shutdown(context.Background()); err != nil {
			t.Errorf("propagation shutdown: %v", err)
		}
	})
}

func TestAMQPContextRoundTripWithoutExporter(t *testing.T) {
	initPropagationTest(t)
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	traceState, _ := trace.ParseTraceState("vendor=queued")
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled, TraceState: traceState,
	})
	member, err := baggage.NewMember("tenant", "test-tenant")
	if err != nil {
		t.Fatal(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatal(err)
	}
	publisher := baggage.ContextWithBaggage(trace.ContextWithSpanContext(context.Background(), parent), bag)
	original := amqp.Table{"message-kind": "stage", "retry-attempt": int32(2)}
	headers := InjectAMQPContext(publisher, CloneAMQPTable(original))
	if len(original) != 2 || original["traceparent"] != nil {
		t.Fatalf("injecting a cloned message mutated the original headers: %v", original)
	}
	if headers["message-kind"] != "stage" || headers["retry-attempt"] != int32(2) {
		t.Fatalf("trace injection discarded application headers: %v", headers)
	}
	if headers["traceparent"] != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("unexpected W3C trace header: %v", headers["traceparent"])
	}
	for _, byteHeaders := range []bool{false, true} {
		name := "string headers"
		if byteHeaders {
			name = "byte headers"
		}
		t.Run(name, func(t *testing.T) {
			transport := CloneAMQPTable(headers)
			if byteHeaders {
				for _, key := range []string{"traceparent", "tracestate", "baggage"} {
					transport[key] = []byte(transport[key].(string))
				}
			}
			consumer := ExtractAMQPContext(context.Background(), transport)
			got := trace.SpanContextFromContext(consumer)
			if !got.IsValid() || !got.IsRemote() || got.TraceID() != parent.TraceID() || got.SpanID() != parent.SpanID() || got.TraceFlags() != parent.TraceFlags() || got.TraceState().String() != parent.TraceState().String() {
				t.Fatalf("AMQP delivery lost its remote parent context: %v", got)
			}
			if got := baggage.FromContext(consumer).Member("tenant").Value(); got != "test-tenant" {
				t.Fatalf("AMQP baggage = %q", got)
			}
		})
	}
	if injected := InjectAMQPContext(publisher, nil); injected["traceparent"] != headers["traceparent"] {
		t.Fatal("injecting into nil headers did not allocate a carrier")
	}
}

func TestAMQPIgnoresMissingAndMalformedTraceHeaders(t *testing.T) {
	initPropagationTest(t)
	for _, headers := range []amqp.Table{
		nil,
		{"message-kind": "stage"},
		{"traceparent": "not-a-trace"},
		{"traceparent": []byte("00-00000000000000000000000000000000-00f067aa0ba902b7-01")},
		{"traceparent": int64(42)},
	} {
		ctx := ExtractAMQPContext(context.Background(), headers)
		if trace.SpanContextFromContext(ctx).IsValid() {
			t.Fatalf("malformed AMQP headers created a valid parent span: %v", headers)
		}
	}
}
