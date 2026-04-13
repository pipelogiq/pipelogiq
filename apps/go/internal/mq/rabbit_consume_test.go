package mq

import (
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

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
