package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"pipelogiq/internal/types"
)

// Creation must persist events even while RabbitMQ is unavailable. The scheduler
// is the only publisher so every delivery receives the ordinary execution fence.
func TestEventCreationUsesDurableFencedScheduler(t *testing.T) {
	for _, idempotent := range []bool{false, true} {
		name := "legacy-create"
		if idempotent {
			name = "idempotent-create"
		}
		t.Run(name, func(t *testing.T) {
			server, db, router := setupIdempotencyEndpointTest(t)
			request := idempotencyPipelineRequest("event:receipt:123", "receipt-event")
			request["apiKey"] = idempotencyTestAPIKey
			request["stages"].([]map[string]any)[0]["isEvent"] = true
			path, expectedStatus := "/pipelines", http.StatusOK
			if idempotent {
				path, expectedStatus = "/pipelines/idempotent", http.StatusCreated
			}
			response := serveIdempotencyJSON(t, router, http.MethodPost, path, idempotencyTestAPIKey, request)
			if response.Code != expectedStatus {
				t.Fatalf("create returned %d: %s", response.Code, response.Body.String())
			}
			var pipeline *types.PipelineResponse
			if idempotent {
				pipeline = decodeIdempotentCreateResponse(t, response).Pipeline
				repeated := serveIdempotencyJSON(t, router, http.MethodPost, path, idempotencyTestAPIKey, request)
				duplicate := decodeIdempotentCreateResponse(t, repeated)
				if !duplicate.WasExisting || duplicate.Pipeline.ID != pipeline.ID {
					t.Fatalf("duplicate event create: %+v", duplicate)
				}
			} else if err := json.Unmarshal(response.Body.Bytes(), &pipeline); err != nil {
				t.Fatal(err)
			}
			assertPipelineCount(t, db, 1)
			if pipeline == nil || pipeline.IsEvent == nil || !*pipeline.IsEvent {
				t.Fatalf("event metadata missing: %+v", pipeline)
			}
			if pipeline.Status != types.PipelineStatusNotStarted {
				t.Fatalf("API must leave dispatch to the scheduler, status=%s", pipeline.Status)
			}
			message, err := server.store.GetStageToExecute(context.Background())
			if err != nil || message == nil {
				t.Fatalf("event dispatch: message=%+v err=%v", message, err)
			}
			if message.ExecutionID == "" || message.Attempt != 1 || message.PipelineID == nil || *message.PipelineID != pipeline.ID {
				t.Fatalf("event execution metadata: %+v", message)
			}
			if message.AppID != idempotencyTestApplicationID || message.StageHandlerName != "EnsureClaimHandler" || message.Input == "" {
				t.Fatalf("event handler payload changed: %+v", message)
			}
			if next, err := server.store.GetStageToExecute(context.Background()); err != nil || next != nil {
				t.Fatalf("event was scheduled twice: %+v, %v", next, err)
			}
			completed, err := server.store.UpdateStageResult(context.Background(), types.StageResultMessage{
				PipelineID: message.PipelineID, StageID: message.StageID,
				ExecutionID: message.ExecutionID, Attempt: message.Attempt,
				IsSuccess: true, Result: "event processed",
			})
			if err != nil || completed == nil || completed.Status != types.PipelineStatusCompleted {
				t.Fatalf("event completion: %+v, %v", completed, err)
			}
		})
	}
}
