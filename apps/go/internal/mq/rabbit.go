package mq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"pipelogiq/internal/telemetry"
)

var rabbitTracer = otel.Tracer("pipelogiq/mq")

type QueueOptions struct {
	Durable     bool
	AutoDelete  bool
	DLQEnabled  bool
	DLQTTL      time.Duration
	Prefetch    int
	ContentType string
}

type ConsumeOptions struct {
	QueueOptions
	HandlerTimeout   time.Duration
	DeadLetterOnFail bool
}

type Client struct {
	url    string
	logger *slog.Logger

	mu             sync.Mutex
	conn           *amqp.Connection
	topologyBypass sync.Map
}

func NewClient(url string, logger *slog.Logger) *Client {
	return &Client{url: url, logger: logger}
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) PublishWithRetry(ctx context.Context, queue string, body []byte, opts QueueOptions, headers amqp.Table) error {
	ctx, span := rabbitTracer.Start(ctx, "rabbitmq.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", queue),
			attribute.String("messaging.operation", "publish"),
		),
	)
	defer span.End()

	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 300 * time.Millisecond
	exp.MaxInterval = 10 * time.Second
	exp.MaxElapsedTime = 0 // never stop until ctx done

	pub := func() error {
		ch, err := c.channelForQueue(ctx, queue, opts)
		if err != nil {
			span.RecordError(err)
			return err
		}
		defer ch.Close()

		ct := opts.ContentType
		if ct == "" {
			ct = "application/json"
		}
		msgHeaders := telemetry.CloneAMQPTable(headers)
		msgHeaders = telemetry.InjectAMQPContext(ctx, msgHeaders)

		if err := publishMessage(ctx, ch, queue, amqp.Publishing{
			Body:         body,
			ContentType:  ct,
			Headers:      msgHeaders,
			MessageId:    uuid.NewString(),
			Timestamp:    time.Now().UTC(),
			DeliveryMode: amqp.Persistent,
		}); err != nil {
			span.RecordError(err)
			return err
		}
		return nil
	}

	if err := backoff.Retry(func() error {
		if ctx.Err() != nil {
			return backoff.Permanent(ctx.Err())
		}
		return pub()
	}, backoff.WithContext(exp, ctx)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (c *Client) Consume(ctx context.Context, queue string, opts ConsumeOptions, handler func(context.Context, amqp.Delivery) error) error {
	if handler == nil {
		return errors.New("handler is nil")
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		ch, err := c.channelForQueue(ctx, queue, opts.QueueOptions)
		if err != nil {
			c.logger.Error("rabbitmq: failed to open channel", "err", err)
			time.Sleep(time.Second)
			continue
		}

		if opts.Prefetch > 0 {
			if err := ch.Qos(opts.Prefetch, 0, false); err != nil {
				c.logger.Error("rabbitmq: qos failed", "err", err)
			}
		}

		deliveries, err := ch.Consume(queue, "", false, false, false, false, nil)
		if err != nil {
			ch.Close()
			c.logger.Error("rabbitmq: consume failed", "queue", queue, "err", err)
			time.Sleep(time.Second)
			continue
		}

		closeCh := ch.NotifyClose(make(chan *amqp.Error, 1))

		for {
			select {
			case d, ok := <-deliveries:
				if !ok {
					c.logger.Warn("rabbitmq: delivery channel closed, reconnecting", "queue", queue)
					drainDeliveries(deliveries)
					goto reconnect
				}

				hctx := telemetry.ExtractAMQPContext(ctx, d.Headers)
				hctx, span := rabbitTracer.Start(hctx, "rabbitmq.consume",
					trace.WithSpanKind(trace.SpanKindConsumer),
					trace.WithAttributes(
						attribute.String("messaging.system", "rabbitmq"),
						attribute.String("messaging.destination.name", queue),
						attribute.String("messaging.operation", "process"),
						attribute.String("messaging.message.id", d.MessageId),
					),
				)
				var cancel context.CancelFunc
				if opts.HandlerTimeout > 0 {
					hctx, cancel = context.WithTimeout(hctx, opts.HandlerTimeout)
				}
				if err := handler(hctx, d); err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					c.logger.Error("rabbitmq: handler error", "queue", queue, "err", err)
					if opts.DeadLetterOnFail {
						if retryErr := c.publishDeliveryToRetryQueue(hctx, ch, queue, d, opts.QueueOptions); retryErr != nil {
							c.logger.Error("rabbitmq: retry publish failed", "queue", queue, "err", retryErr)
							_ = d.Nack(false, true)
						} else {
							_ = d.Ack(false)
						}
					} else {
						_ = d.Nack(false, true)
					}
					if cancel != nil {
						cancel()
					}
					span.End()
					continue
				}
				if cancel != nil {
					cancel()
				}
				_ = d.Ack(false)
				span.End()
			case err := <-closeCh:
				if err != nil {
					c.logger.Warn("rabbitmq: channel closed", "queue", queue, "err", err)
				}
				goto reconnect
			case <-ctx.Done():
				c.logger.Info("rabbitmq: stopping consumer", "queue", queue)
				ch.Cancel("", false)
				ch.Close()
				return ctx.Err()
			}
		}

	reconnect:
		ch.Close()
		time.Sleep(time.Second)
	}
}

func drainDeliveries(deliveries <-chan amqp.Delivery) {
	for {
		select {
		case _, ok := <-deliveries:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// Get pulls one message without auto-acking. Caller must ack/nack using returned functions.
type GetResult struct {
	Body      []byte
	Headers   amqp.Table
	MessageID string
	Ack       func() error
	Nack      func(requeue bool) error
	Queue     string
	Delivery  amqp.Delivery
}

func (c *Client) Get(ctx context.Context, queue string, opts QueueOptions) (*GetResult, error) {
	ctx, span := rabbitTracer.Start(ctx, "rabbitmq.get",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", queue),
			attribute.String("messaging.operation", "receive"),
		),
	)
	defer span.End()

	ch, err := c.channelForQueue(ctx, queue, opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if opts.Prefetch > 0 {
		_ = ch.Qos(opts.Prefetch, 0, false)
	}

	d, ok, err := ch.Get(queue, false)
	if err != nil {
		ch.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if !ok {
		ch.Close()
		return nil, nil
	}

	res := &GetResult{
		Body:      d.Body,
		Headers:   d.Headers,
		MessageID: d.MessageId,
		Queue:     queue,
		Delivery:  d,
	}
	span.SetAttributes(attribute.String("messaging.message.id", d.MessageId))
	res.Ack = func() error {
		defer ch.Close()
		return d.Ack(false)
	}
	res.Nack = func(requeue bool) error {
		defer ch.Close()
		return d.Nack(false, requeue)
	}
	return res, nil
}

func (c *Client) channel(ctx context.Context) (*amqp.Channel, error) {
	conn, err := c.connection(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	return ch, nil
}

func (c *Client) connection(ctx context.Context) (*amqp.Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn, nil
	}

	var conn *amqp.Connection
	operation := func() error {
		var err error
		conn, err = amqp.DialConfig(c.url, amqp.Config{
			Properties: amqp.Table{
				"connection_name": "pipelogiq",
			},
			Dial: amqp.DefaultDial(5 * time.Second),
		})
		return err
	}

	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 500 * time.Millisecond
	exp.MaxElapsedTime = 0 // keep retrying until ctx canceled

	if err := backoff.Retry(func() error {
		if ctx.Err() != nil {
			return backoff.Permanent(ctx.Err())
		}
		return operation()
	}, backoff.WithContext(exp, ctx)); err != nil {
		return nil, fmt.Errorf("connect rabbitmq: %w", err)
	}

	c.conn = conn
	c.logger.Info("connected to rabbitmq")
	return conn, nil
}

func (c *Client) channelForQueue(ctx context.Context, queue string, opts QueueOptions) (*amqp.Channel, error) {
	ch, err := c.channel(ctx)
	if err != nil {
		return nil, err
	}
	if c.shouldBypassTopology(queue) {
		if err := inspectQueueTopology(ch, queue, opts); err == nil {
			return ch, nil
		} else {
			ch.Close()
			if !isNotFound(err) {
				return nil, err
			}
			c.clearTopologyBypass(queue)

			ch, err = c.channel(ctx)
			if err != nil {
				return nil, err
			}
		}
	}
	if err := declareQueue(ch, queue, opts); err != nil {
		ch.Close()
		if !isPreconditionFailed(err) {
			return nil, err
		}
		c.rememberTopologyBypass(queue, err)
		ch, err = c.channel(ctx)
		if err != nil {
			return nil, err
		}
		if err := inspectQueueTopology(ch, queue, opts); err != nil {
			ch.Close()
			return nil, err
		}
		return ch, nil
	}
	return ch, nil
}

func (c *Client) publishDeliveryToRetryQueue(
	ctx context.Context,
	ch *amqp.Channel,
	queue string,
	d amqp.Delivery,
	opts QueueOptions,
) error {
	if !opts.DLQEnabled {
		return errors.New("retry queue is disabled")
	}

	headers := telemetry.CloneAMQPTable(d.Headers)
	headers = telemetry.InjectAMQPContext(ctx, headers)

	return publishMessage(ctx, ch, retryQueueName(queue), amqp.Publishing{
		Body:         d.Body,
		ContentType:  firstNonEmpty(d.ContentType, opts.ContentType, "application/json"),
		Headers:      headers,
		MessageId:    firstNonEmpty(d.MessageId, uuid.NewString()),
		Timestamp:    time.Now().UTC(),
		DeliveryMode: amqp.Persistent,
	})
}

func publishMessage(ctx context.Context, ch *amqp.Channel, queue string, msg amqp.Publishing) error {
	return ch.PublishWithContext(ctx, "", queue, false, false, msg)
}

// PublishToExchange publishes a message to a fanout exchange.
func (c *Client) PublishToExchange(ctx context.Context, exchange string, body []byte) error {
	ctx, span := rabbitTracer.Start(ctx, "rabbitmq.publish.fanout",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", exchange),
			attribute.String("messaging.operation", "publish"),
		),
	)
	defer span.End()

	ch, err := c.channel(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(exchange, "fanout", true, false, false, false, nil); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("declare exchange %s: %w", exchange, err)
	}

	headers := telemetry.InjectAMQPContext(ctx, amqp.Table{})
	if err := ch.PublishWithContext(ctx, exchange, "", false, false, amqp.Publishing{
		Body:        body,
		ContentType: "application/json",
		Headers:     headers,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// SubscribeFanout creates an exclusive auto-delete queue bound to a fanout exchange and consumes from it.
// Each caller gets its own queue so all subscribers receive every message.
func (c *Client) SubscribeFanout(ctx context.Context, exchange string, handler func(context.Context, []byte)) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		ch, err := c.channel(ctx)
		if err != nil {
			c.logger.Error("rabbitmq: fanout channel failed", "exchange", exchange, "err", err)
			time.Sleep(time.Second)
			continue
		}

		if err := ch.ExchangeDeclare(exchange, "fanout", true, false, false, false, nil); err != nil {
			ch.Close()
			c.logger.Error("rabbitmq: declare fanout exchange failed", "exchange", exchange, "err", err)
			time.Sleep(time.Second)
			continue
		}

		q, err := ch.QueueDeclare("", false, true, true, false, nil)
		if err != nil {
			ch.Close()
			c.logger.Error("rabbitmq: declare exclusive queue failed", "err", err)
			time.Sleep(time.Second)
			continue
		}

		if err := ch.QueueBind(q.Name, "", exchange, false, nil); err != nil {
			ch.Close()
			c.logger.Error("rabbitmq: bind queue to exchange failed", "exchange", exchange, "err", err)
			time.Sleep(time.Second)
			continue
		}

		deliveries, err := ch.Consume(q.Name, "", true, true, false, false, nil)
		if err != nil {
			ch.Close()
			c.logger.Error("rabbitmq: consume fanout failed", "exchange", exchange, "err", err)
			time.Sleep(time.Second)
			continue
		}

		closeCh := ch.NotifyClose(make(chan *amqp.Error, 1))

		for {
			select {
			case d, ok := <-deliveries:
				if !ok {
					goto reconnect
				}
				handlerCtx := telemetry.ExtractAMQPContext(ctx, d.Headers)
				handlerCtx, span := rabbitTracer.Start(handlerCtx, "rabbitmq.consume.fanout",
					trace.WithSpanKind(trace.SpanKindConsumer),
					trace.WithAttributes(
						attribute.String("messaging.system", "rabbitmq"),
						attribute.String("messaging.destination.name", exchange),
						attribute.String("messaging.operation", "process"),
						attribute.String("messaging.message.id", d.MessageId),
					),
				)
				handler(handlerCtx, d.Body)
				span.End()
			case err := <-closeCh:
				if err != nil {
					c.logger.Warn("rabbitmq: fanout channel closed", "exchange", exchange, "err", err)
				}
				goto reconnect
			case <-ctx.Done():
				ch.Close()
				return ctx.Err()
			}
		}

	reconnect:
		ch.Close()
		time.Sleep(time.Second)
	}
}

func declareQueue(ch *amqp.Channel, name string, opts QueueOptions) error {
	if opts.DLQEnabled {
		if err := declareRawQueue(ch, retryQueueName(name), opts.Durable, opts.AutoDelete, retryQueueArgs(name, opts.DLQTTL)); err != nil {
			return err
		}
	}
	return declareRawQueue(ch, name, opts.Durable, opts.AutoDelete, nil)
}

func declareRawQueue(ch *amqp.Channel, name string, durable, autoDelete bool, args amqp.Table) error {
	_, err := ch.QueueDeclare(name, durable, autoDelete, false, false, args)
	return err
}

func inspectQueueTopology(ch *amqp.Channel, name string, opts QueueOptions) error {
	if _, err := ch.QueueInspect(name); err != nil {
		return err
	}
	if opts.DLQEnabled {
		if _, err := ch.QueueInspect(retryQueueName(name)); err != nil {
			return err
		}
	}
	return nil
}

func retryQueueName(name string) string {
	return name + ".dlq"
}

func retryQueueArgs(name string, ttl time.Duration) amqp.Table {
	if ttl <= 0 {
		return nil
	}
	return amqp.Table{
		"x-message-ttl":             int64(ttl / time.Millisecond),
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": name,
	}
}

func isPreconditionFailed(err error) bool {
	var amqpErr *amqp.Error
	return errors.As(err, &amqpErr) && amqpErr.Code == amqp.PreconditionFailed
}

func isNotFound(err error) bool {
	var amqpErr *amqp.Error
	return errors.As(err, &amqpErr) && amqpErr.Code == amqp.NotFound
}

func (c *Client) shouldBypassTopology(queue string) bool {
	_, ok := c.topologyBypass.Load(queue)
	return ok
}

func (c *Client) rememberTopologyBypass(queue string, err error) {
	if _, loaded := c.topologyBypass.LoadOrStore(queue, struct{}{}); loaded {
		return
	}
	c.logger.Warn("rabbitmq: queue topology differs, using existing queue", "queue", queue, "err", err)
}

func (c *Client) clearTopologyBypass(queue string) {
	c.topologyBypass.Delete(queue)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
