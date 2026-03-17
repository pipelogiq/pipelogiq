package mq

import (
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
