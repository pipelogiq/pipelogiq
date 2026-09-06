package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"pipelogiq/internal/constants"
	"pipelogiq/internal/mq"
)

// fakeQueue stands in for one RabbitMQ stage queue. Publishing after a pull has
// already started is what proves the long poll actually waits.
type fakeQueue struct {
	mu       sync.Mutex
	messages [][]byte
	acked    int
	nacked   int
	requeued int
	err      error
}

func (q *fakeQueue) publish(body []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, body)
}

func (q *fakeQueue) get(_ context.Context, queue string) (*mq.GetResult, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return nil, q.err
	}
	if len(q.messages) == 0 {
		return nil, nil
	}
	body := q.messages[0]
	q.messages = q.messages[1:]
	return &mq.GetResult{
		Body:      body,
		MessageID: "msg-1",
		Queue:     queue,
		Ack: func() error {
			q.mu.Lock()
			defer q.mu.Unlock()
			q.acked++
			return nil
		},
		Nack: func(requeue bool) error {
			q.mu.Lock()
			defer q.mu.Unlock()
			q.nacked++
			if requeue {
				q.requeued++
				q.messages = append(q.messages, body)
			}
			return nil
		},
	}, nil
}

func (q *fakeQueue) counts() (acked, requeued int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.acked, q.requeued
}

func stageQueueName(handler string) string {
	return constants.StageNextQueueName(constants.ApplicationQueueID(externalTestApplicationID), handler)
}

func postPull(t *testing.T, router http.Handler, body any, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := makeJSONRequest(t, http.MethodPost, "/jobs/pull", body)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestPullJobEndpoint(t *testing.T) {
	t.Run("returns a job from the application queue", func(t *testing.T) {
		server, _, router := setupExternalEndpointTest(t)
		queue := &fakeQueue{}
		server.getStageJob = queue.get
		queue.publish([]byte(`{"stageId":7}`))

		res := postPull(t, router, map[string]any{"queue": stageQueueName("PayHandler")}, "test-api-key")
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
		}

		var payload pullResponse
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if payload.Token == "" {
			t.Fatalf("token must be issued")
		}
		if string(payload.Payload) != `{"stageId":7}` {
			t.Fatalf("payload = %s", payload.Payload)
		}
	})

	t.Run("empty queue answers 204 without waiting", func(t *testing.T) {
		server, _, router := setupExternalEndpointTest(t)
		queue := &fakeQueue{}
		server.getStageJob = queue.get

		started := time.Now()
		res := postPull(t, router, map[string]any{"queue": stageQueueName("PayHandler")}, "test-api-key")
		if res.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", res.Code)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("default pull waited %s, want an immediate answer", elapsed)
		}
	})

	t.Run("queue outside the application is forbidden", func(t *testing.T) {
		server, _, router := setupExternalEndpointTest(t)
		queue := &fakeQueue{}
		server.getStageJob = queue.get
		queue.publish([]byte(`{"stageId":7}`))

		foreign := constants.StageNextQueueName(
			constants.ApplicationQueueID(externalTestApplicationID+1),
			"PayHandler",
		)
		res := postPull(t, router, map[string]any{"queue": foreign}, "test-api-key")
		if res.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403; body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("malformed queue names are rejected", func(t *testing.T) {
		cases := []struct {
			name  string
			queue string
		}{
			{"result queue", constants.StageResult},
			{"empty handler", stageQueueName("")},
			{"path traversal in handler", stageQueueName("../other")},
			{"whitespace in handler", stageQueueName("Pay Handler")},
			{"application prefix confusion", fmt.Sprintf("app_%d0_h_%s", externalTestApplicationID, constants.StageNext)},
			{"missing suffix", fmt.Sprintf("app_%d_PayHandler", externalTestApplicationID)},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				server, _, router := setupExternalEndpointTest(t)
				queue := &fakeQueue{}
				server.getStageJob = queue.get

				res := postPull(t, router, map[string]any{"queue": tc.queue}, "test-api-key")
				if res.Code != http.StatusForbidden {
					t.Fatalf("status = %d, want 403 for queue %q", res.Code, tc.queue)
				}
			})
		}
	})

	t.Run("invalid api key is unauthorized", func(t *testing.T) {
		server, _, router := setupExternalEndpointTest(t)
		server.getStageJob = (&fakeQueue{}).get

		res := postPull(t, router, map[string]any{"queue": stageQueueName("PayHandler")}, "wrong-key")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", res.Code)
		}
	})

	t.Run("long poll returns a message published after the request started", func(t *testing.T) {
		server, _, router := setupExternalEndpointTest(t)
		queue := &fakeQueue{}
		server.getStageJob = queue.get

		go func() {
			time.Sleep(400 * time.Millisecond)
			queue.publish([]byte(`{"stageId":9}`))
		}()

		started := time.Now()
		res := postPull(t, router, map[string]any{
			"queue":       stageQueueName("PayHandler"),
			"waitSeconds": 5,
		}, "test-api-key")
		elapsed := time.Since(started)

		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
		}
		if elapsed < 300*time.Millisecond {
			t.Fatalf("long poll returned after %s, so it cannot have waited", elapsed)
		}

		var payload pullResponse
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if string(payload.Payload) != `{"stageId":9}` {
			t.Fatalf("payload = %s", payload.Payload)
		}
	})

	t.Run("long poll times out with 204", func(t *testing.T) {
		server, _, router := setupExternalEndpointTest(t)
		server.getStageJob = (&fakeQueue{}).get

		started := time.Now()
		res := postPull(t, router, map[string]any{
			"queue":       stageQueueName("PayHandler"),
			"waitSeconds": 1,
		}, "test-api-key")
		elapsed := time.Since(started)

		if res.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", res.Code)
		}
		if elapsed < 900*time.Millisecond {
			t.Fatalf("timed out after %s, want about 1s", elapsed)
		}
	})

	t.Run("waitSeconds outside the allowed range is rejected", func(t *testing.T) {
		for _, wait := range []int{maxPullWaitSeconds + 1, -1} {
			server, _, router := setupExternalEndpointTest(t)
			server.getStageJob = (&fakeQueue{}).get

			res := postPull(t, router, map[string]any{
				"queue":       stageQueueName("PayHandler"),
				"waitSeconds": wait,
			}, "test-api-key")
			if res.Code != http.StatusBadRequest {
				t.Fatalf("waitSeconds=%d status = %d, want 400", wait, res.Code)
			}
		}
	})
}

func TestGatewayInFlightSweeper(t *testing.T) {
	t.Run("expired in-flight messages are requeued", func(t *testing.T) {
		server, _, router := setupExternalEndpointTest(t)
		server.cfg.GatewayVisibilityTTL = 50 * time.Millisecond
		queue := &fakeQueue{}
		server.getStageJob = queue.get
		queue.publish([]byte(`{"stageId":11}`))

		res := postPull(t, router, map[string]any{"queue": stageQueueName("PayHandler")}, "test-api-key")
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", res.Code, res.Body.String())
		}

		if released := server.sweepExpiredPending(time.Now()); released != 0 {
			t.Fatalf("swept %d messages before expiry, want 0", released)
		}

		if released := server.sweepExpiredPending(time.Now().Add(time.Minute)); released != 1 {
			t.Fatalf("swept %d messages after expiry, want 1", released)
		}

		acked, requeued := queue.counts()
		if acked != 0 || requeued != 1 {
			t.Fatalf("acked=%d requeued=%d, want 0 and 1", acked, requeued)
		}

		server.pendingMu.Lock()
		pending := len(server.pending)
		server.pendingMu.Unlock()
		if pending != 0 {
			t.Fatalf("pending = %d after sweep, want 0", pending)
		}
	})

	t.Run("sweeper runs twice per visibility window", func(t *testing.T) {
		cases := []struct {
			visibility time.Duration
			want       time.Duration
		}{
			{visibility: time.Minute, want: 30 * time.Second},
			{visibility: 10 * time.Second, want: 5 * time.Second},
			{visibility: time.Second, want: minPendingSweepInterval},
			{visibility: 0, want: minPendingSweepInterval},
		}
		for _, tc := range cases {
			if got := pendingSweepInterval(tc.visibility); got != tc.want {
				t.Fatalf("pendingSweepInterval(%s) = %s, want %s", tc.visibility, got, tc.want)
			}
		}
	})

	t.Run("the sweeper goroutine requeues without an explicit call", func(t *testing.T) {
		server, _, router := setupExternalEndpointTest(t)
		server.cfg.GatewayVisibilityTTL = 40 * time.Millisecond
		queue := &fakeQueue{}
		server.getStageJob = queue.get
		queue.publish([]byte(`{"stageId":13}`))

		res := postPull(t, router, map[string]any{"queue": stageQueueName("PayHandler")}, "test-api-key")
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.Code)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go server.cleanupExpired(ctx)

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, requeued := queue.counts(); requeued == 1 {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("sweeper goroutine never requeued the expired message")
	})
}
