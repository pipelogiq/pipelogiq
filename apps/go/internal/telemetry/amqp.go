package telemetry

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type amqpCarrier struct {
	headers amqp.Table
}

func (c amqpCarrier) Get(key string) string {
	if c.headers == nil {
		return ""
	}
	raw, ok := c.headers[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}

func (c amqpCarrier) Set(key, value string) {
	if c.headers == nil {
		return
	}
	c.headers[key] = value
}

func (c amqpCarrier) Keys() []string {
	if len(c.headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c.headers))
	for key := range c.headers {
		keys = append(keys, key)
	}
	return keys
}

func InjectAMQPContext(ctx context.Context, headers amqp.Table) amqp.Table {
	if headers == nil {
		headers = amqp.Table{}
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.TextMapCarrier(amqpCarrier{headers: headers}))
	return headers
}

func ExtractAMQPContext(ctx context.Context, headers amqp.Table) context.Context {
	if headers == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.TextMapCarrier(amqpCarrier{headers: headers}))
}

func CloneAMQPTable(headers amqp.Table) amqp.Table {
	if headers == nil {
		return amqp.Table{}
	}
	clone := make(amqp.Table, len(headers))
	for key, value := range headers {
		clone[key] = value
	}
	return clone
}
