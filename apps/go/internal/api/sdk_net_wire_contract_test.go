package api

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"

	"pipelogiq/internal/types"
)

// Fixtures are exported by the real .NET API client (ApiHardeningTests).
// An integration run can set PIPELOGIQ_SDK_CONTRACT_FIXTURE in both test processes
// to check the current client against these exact server request types.
func TestDotNetSDKWireContracts(t *testing.T) {
	path := os.Getenv("PIPELOGIQ_SDK_CONTRACT_FIXTURE")
	if path == "" {
		path = "testdata/sdk-net-wire-contracts.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var requests []struct {
		Method string
		Path   string
		Body   json.RawMessage
	}
	if err = json.Unmarshal(raw, &requests); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 26 {
		t.Fatalf("operations=%d, want 26", len(requests))
	}
	for _, request := range requests {
		request := request
		t.Run(request.Method+" "+request.Path, func(t *testing.T) {
			parsed, err := url.Parse(request.Path)
			if err != nil {
				t.Fatal(err)
			}
			path := parsed.Path
			var target any = &struct{}{}
			strict := true
			switch {
			case request.Method == "GET":
			case path == "/pipelines":
				target = &types.PipelineCreateRequest{}
			case path == "/pipelines/idempotent":
				target = &types.IdempotentPipelineCreateRequest{}
			case path == "/pipelines/by-idempotency-key":
				target = &types.PipelineIdempotencyLookupRequest{}
			case path == "/logs":
				target = &types.LogRequest{}
			case path == "/workers/bootstrap":
				target = &types.WorkerBootstrapRequest{}
			case path == "/workers/heartbeat":
				target = &types.WorkerHeartbeatRequest{}
			case path == "/workers/events":
				target = &types.WorkerEventsRequest{}
			case path == "/workers/shutdown":
				target = &types.WorkerShutdownRequest{}
			case path == "/jobs/pull":
				target = &pullRequest{}
			case path == "/jobs/ack":
				target = &ackRequest{}
			case strings.Contains(path, "/lease/"):
				target = &types.StageLeaseRequest{}
			case strings.HasSuffix(path, "/stages"):
				target = &appendStagesSDKRequest{}
			case strings.HasPrefix(path, "/stages/") && strings.HasSuffix(path, "/resume"):
				target = &types.ResumeStageRequest{}
			case strings.HasSuffix(path, "/result"):
				target = &types.StageResultMessage{}
				strict = false
			case request.Method == "PUT" && strings.HasPrefix(path, "/schedules/"):
				target = &types.ScheduleUpsertRequest{}
			case strings.HasSuffix(path, "/trigger"):
				target = &types.ScheduleTriggerRequest{}
			}
			decoder := json.NewDecoder(bytes.NewReader(request.Body))
			if strict {
				decoder.DisallowUnknownFields()
			}
			if err = decoder.Decode(target); err != nil {
				t.Fatalf("SDK body rejected by server DTO: %v; %s", err, request.Body)
			}
			if result, ok := target.(*types.StageResultMessage); ok && len(result.AppendedStages) > 0 {
				if !json.Valid([]byte(result.AppendedStages[0].Input)) {
					t.Fatal("appended stage input is not serialized JSON")
				}
			}
		})
	}
}
