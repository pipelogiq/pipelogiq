package mq

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestConsumeCancelsRegisteredConsumerAndClosesChannel(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cancelErr   error
		wantWarning bool
	}{
		{"successful cancel", nil, false},
		{"already closed", amqp.ErrClosed, false},
		{"cancel failure", errors.New("broker rejected cancellation"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			channel := &fakeConsumingChannel{ready: make(chan struct{}), deliveries: make(chan amqp.Delivery), cancelErr: tc.cancelErr}
			client := &Client{
				logger:                slog.New(slog.NewTextHandler(&logs, nil)),
				consumeChannelFactory: func(context.Context, string, QueueOptions) (consumingChannel, error) { return channel, nil },
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- client.Consume(ctx, "stage-results", ConsumeOptions{}, func(context.Context, amqp.Delivery) error { t.Error("no delivery expected"); return nil })
			}()
			select {
			case <-channel.ready:
			case <-time.After(time.Second):
				t.Fatal("consumer was not registered")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Consume returned %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("consumer did not stop after cancellation")
			}
			if channel.consumerTag == "" || channel.cancelTag != channel.consumerTag {
				t.Fatalf("canceled tag %q differs from registered tag %q", channel.cancelTag, channel.consumerTag)
			}
			if channel.closeCalls != 1 {
				t.Fatalf("channel closed %d times, want once", channel.closeCalls)
			}
			if got := strings.Contains(logs.String(), "cancel consumer failed"); got != tc.wantWarning {
				t.Fatalf("cancel warning = %v, want %v: %s", got, tc.wantWarning, logs.String())
			}
		})
	}
}

type fakeConsumingChannel struct {
	ready       chan struct{}
	deliveries  chan amqp.Delivery
	consumerTag string
	cancelTag   string
	cancelErr   error
	closeCalls  int
}

func (f *fakeConsumingChannel) Qos(int, int, bool) error { return nil }
func (f *fakeConsumingChannel) Consume(_ string, consumer string, _, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	f.consumerTag = consumer
	close(f.ready)
	return f.deliveries, nil
}
func (f *fakeConsumingChannel) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	return receiver
}
func (f *fakeConsumingChannel) Cancel(consumer string, _ bool) error {
	f.cancelTag = consumer
	return f.cancelErr
}
func (f *fakeConsumingChannel) Close() error { f.closeCalls++; return nil }

func TestDrainDeliveriesReturnsWhenChannelClosed(t *testing.T) {
	deliveries := make(chan amqp.Delivery)
	close(deliveries)

	done := make(chan struct{})
	go func() {
		drainDeliveries(deliveries)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drainDeliveries blocked on closed channel")
	}
}

func TestDrainDeliveriesDrainsBufferedMessagesAndReturns(t *testing.T) {
	deliveries := make(chan amqp.Delivery, 2)
	deliveries <- amqp.Delivery{MessageId: "one"}
	deliveries <- amqp.Delivery{MessageId: "two"}
	close(deliveries)

	done := make(chan struct{})
	go func() {
		drainDeliveries(deliveries)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drainDeliveries did not finish after draining buffered channel")
	}
}
