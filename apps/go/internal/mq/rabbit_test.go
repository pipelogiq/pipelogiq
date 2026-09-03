package mq

import (
	"context"
	"reflect"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRetryQueueName(t *testing.T) {
	if got := retryQueueName("StageResult"); got != "StageResult.dlq" {
		t.Fatalf("retryQueueName() = %q, want %q", got, "StageResult.dlq")
	}
}

func TestRetryQueueArgsWithTTL(t *testing.T) {
	got := retryQueueArgs("StageResult", 30*time.Second)
	want := amqp.Table{
		"x-message-ttl":             int64(30000),
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": "StageResult",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retryQueueArgs() = %#v, want %#v", got, want)
	}
}

func TestRetryQueueArgsWithoutTTL(t *testing.T) {
	if got := retryQueueArgs("StageResult", 0); got != nil {
		t.Fatalf("retryQueueArgs() = %#v, want nil", got)
	}
}

func TestRetryQueuePublishUsesFreshConfirmedChannelForEveryFailure(t *testing.T) {
	var channels []*fakeConfirmPublishingChannel
	client := &Client{
		retryPublishChannelFactory: func(context.Context) (confirmPublishingChannel, error) {
			ch := &fakeConfirmPublishingChannel{}
			channels = append(channels, ch)
			return ch, nil
		},
	}
	delivery := amqp.Delivery{
		Body:        []byte(`{"stageId":42}`),
		ContentType: "application/json",
		MessageId:   "stage-result-42",
	}
	options := QueueOptions{DLQEnabled: true}

	for attempt := 0; attempt < 3; attempt++ {
		if err := client.publishDeliveryToRetryQueue(
			context.Background(),
			"StageResult",
			delivery,
			options,
		); err != nil {
			t.Fatalf("retry publish %d: %v", attempt+1, err)
		}
	}

	if len(channels) != 3 {
		t.Fatalf("publish channel count = %d, want 3", len(channels))
	}
	for idx, ch := range channels {
		if ch.confirmCalls != 1 || ch.notifyCalls != 1 || ch.publishCalls != 1 || ch.closeCalls != 1 {
			t.Fatalf(
				"channel %d calls: confirm=%d notify=%d publish=%d close=%d",
				idx+1,
				ch.confirmCalls,
				ch.notifyCalls,
				ch.publishCalls,
				ch.closeCalls,
			)
		}
	}
}

type fakeConfirmPublishingChannel struct {
	confirmCalls  int
	notifyCalls   int
	publishCalls  int
	closeCalls    int
	confirmations chan amqp.Confirmation
}

func (f *fakeConfirmPublishingChannel) Confirm(bool) error {
	f.confirmCalls++
	return nil
}

func (f *fakeConfirmPublishingChannel) NotifyPublish(
	confirmations chan amqp.Confirmation,
) chan amqp.Confirmation {
	f.notifyCalls++
	f.confirmations = confirmations
	return confirmations
}

func (f *fakeConfirmPublishingChannel) PublishWithContext(
	context.Context,
	string,
	string,
	bool,
	bool,
	amqp.Publishing,
) error {
	f.publishCalls++
	f.confirmations <- amqp.Confirmation{Ack: true}
	return nil
}

func (f *fakeConfirmPublishingChannel) Close() error {
	f.closeCalls++
	close(f.confirmations)
	return nil
}
