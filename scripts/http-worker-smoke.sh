#!/usr/bin/env bash
#
# End-to-end smoke test for the HTTP worker protocol (docs/http-workers.md).
#
# It plays the whole worker loop with curl against a running stack: bootstrap,
# long-poll pull, lease acquire, result, ack — twice for the first stage (a
# retryable failure, then a success) and once for the second. It then asserts the
# pipeline reached Completed and that the first stage recorded one retry.
#
# Usage:
#   make compose-up                      # in another terminal
#   BOOTSTRAP_API_KEY=<key> scripts/http-worker-smoke.sh
#
# The key comes from BOOTSTRAP_API_KEY in .env; the script reads .env when the
# variable is not already exported.
#
set -euo pipefail

API="${PIPELOGIQ_EXTERNAL_URL:-http://localhost:8081}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HANDLER="${SMOKE_HANDLER:-HttpSmokeHandler}"
RUN_ID="${SMOKE_RUN_ID:-$(date +%s)-$$}"

if [[ -z "${BOOTSTRAP_API_KEY:-}" && -f "$ROOT/.env" ]]; then
  BOOTSTRAP_API_KEY="$(grep -E '^BOOTSTRAP_API_KEY=' "$ROOT/.env" | tail -1 | cut -d= -f2- || true)"
fi
KEY="${BOOTSTRAP_API_KEY:-}"

if [[ -z "$KEY" ]]; then
  echo "BOOTSTRAP_API_KEY is not set and .env has no value for it." >&2
  echo "Set it in .env before 'make compose-up' so the API provisions the key." >&2
  exit 2
fi

for tool in curl jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required" >&2; exit 2; }
done

fail() { echo "FAIL: $*" >&2; exit 1; }
step() { printf '\n=== %s\n' "$*"; }

# call METHOD PATH [BODY] — prints the response body, fails on an unexpected status.
call() {
  local method="$1" path="$2" body="${3:-}" expect="${4:-2xx}"
  local response status
  if [[ -n "$body" ]]; then
    response="$(curl -sS -w $'\n%{http_code}' -X "$method" "$API$path" \
      -H "X-API-Key: $KEY" \
      ${SESSION:+-H "X-Worker-Session: $SESSION"} \
      -H 'Content-Type: application/json' -d "$body")"
  else
    response="$(curl -sS -w $'\n%{http_code}' -X "$method" "$API$path" \
      -H "X-API-Key: $KEY" \
      ${SESSION:+-H "X-Worker-Session: $SESSION"})"
  fi
  status="${response##*$'\n'}"
  body="${response%$'\n'*}"
  case "$expect" in
    2xx) [[ "$status" =~ ^2 ]] || fail "$method $path returned $status: $body" ;;
    *)   [[ "$status" == "$expect" ]] || fail "$method $path returned $status, want $expect: $body" ;;
  esac
  printf '%s' "$body"
}

step "Waiting for the external API at $API"
for _ in $(seq 1 60); do
  if curl -fsS "$API/healthz" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS "$API/healthz" >/dev/null || fail "external API never became healthy"

step "Bootstrapping the worker"
SESSION=""
BOOTSTRAP="$(call POST /workers/bootstrap \
  "{\"workerName\":\"http-smoke-$RUN_ID\",\"supportedHandlers\":[\"$HANDLER\"]}")"
SESSION="$(jq -er .workerSessionToken <<<"$BOOTSTRAP")"
WORKER_ID="$(jq -er .workerId <<<"$BOOTSTRAP")"
APP_ID="$(jq -er .application.appId <<<"$BOOTSTRAP")"
PATTERN="$(jq -er .queues.stageNextPattern <<<"$BOOTSTRAP")"
[[ "$PATTERN" == '{appId}_{handler}_StageNext' ]] \
  || fail "unexpected stageNextPattern: $PATTERN"
QUEUE="${APP_ID}_${HANDLER}_StageNext"
echo "worker=$WORKER_ID app=$APP_ID queue=$QUEUE"

step "Creating a two-stage pipeline"
# The first stage retries once with no delay, so the smoke run stays short.
PIPELINE="$(call POST /pipelines/idempotent "$(cat <<JSON
{
  "idempotencyKey": "http-smoke-$RUN_ID",
  "name": "http-worker-smoke",
  "stages": [
    {
      "stageName": "first",
      "stageHandlerName": "$HANDLER",
      "input": "{\"step\":1}",
      "options": { "maxRetries": 2, "retryInterval": 0 }
    },
    {
      "stageName": "second",
      "stageHandlerName": "$HANDLER",
      "input": "{\"step\":2}"
    }
  ]
}
JSON
)")"
PIPELINE_ID="$(jq -er .pipeline.id <<<"$PIPELINE")"
echo "pipelineId=$PIPELINE_ID"

# pull_job — long-polls the queue and exports TOKEN, STAGE_ID, EXECUTION_ID, ATTEMPT.
pull_job() {
  local attempt job
  for attempt in $(seq 1 6); do
    job="$(curl -sS -X POST "$API/jobs/pull" \
      -H "X-API-Key: $KEY" -H "X-Worker-Session: $SESSION" \
      -H 'Content-Type: application/json' \
      -d "{\"queue\":\"$QUEUE\",\"waitSeconds\":20}")"
    if [[ -n "$job" ]]; then
      TOKEN="$(jq -er .token <<<"$job")"
      STAGE_ID="$(jq -er .payload.stageId <<<"$job")"
      EXECUTION_ID="$(jq -er .payload.executionId <<<"$job")"
      ATTEMPT="$(jq -er '.payload.attempt // 1' <<<"$job")"
      return 0
    fi
  done
  fail "no job arrived on $QUEUE after 6 long polls"
}

# run_stage EXPECT_SUCCESS DESCRIPTION — pull, lease, report, ack.
run_stage() {
  local success="$1" description="$2" result_body

  step "$description"
  pull_job
  echo "stageId=$STAGE_ID executionId=$EXECUTION_ID attempt=$ATTEMPT"

  local lease
  lease="$(call POST "/stages/$STAGE_ID/lease/acquire" \
    "{\"executionId\":\"$EXECUTION_ID\",\"workerId\":\"$WORKER_ID\"}")"
  [[ "$(jq -er .acquired <<<"$lease")" == "true" ]] \
    || fail "lease not acquired: $(jq -c . <<<"$lease")"

  if [[ "$success" == "true" ]]; then
    result_body="$(cat <<JSON
{
  "pipelineId": $PIPELINE_ID,
  "stageId": $STAGE_ID,
  "executionId": "$EXECUTION_ID",
  "attempt": $ATTEMPT,
  "isSuccess": true,
  "result": "{\"ok\":true}"
}
JSON
)"
  else
    result_body="$(cat <<JSON
{
  "pipelineId": $PIPELINE_ID,
  "stageId": $STAGE_ID,
  "executionId": "$EXECUTION_ID",
  "attempt": $ATTEMPT,
  "isSuccess": false,
  "result": "upstream is not ready yet",
  "errorCode": "UPSTREAM_ERROR",
  "retryable": true
}
JSON
)"
  fi

  local accepted
  accepted="$(call POST "/stages/$STAGE_ID/result" "$result_body" 202)"
  [[ "$(jq -er .accepted <<<"$accepted")" == "true" ]] \
    || fail "result not accepted: $accepted"

  # Ack only after 202, exactly as docs/http-workers.md requires.
  call POST /jobs/ack "{\"token\":\"$TOKEN\",\"requeue\":false}" >/dev/null
  FIRST_STAGE_ID="${FIRST_STAGE_ID:-$STAGE_ID}"
}

FIRST_STAGE_ID=""
run_stage false "Stage 1, attempt 1: retryable failure"
run_stage true  "Stage 1, attempt 2: success after retry"
run_stage true  "Stage 2: success"

step "Waiting for the pipeline to complete"
STATUS=""
for _ in $(seq 1 60); do
  SNAPSHOT="$(call GET "/pipelines/$PIPELINE_ID")"
  STATUS="$(jq -er .status <<<"$SNAPSHOT")"
  [[ "$STATUS" == "Completed" || "$STATUS" == "Failed" || "$STATUS" == "Cancelled" ]] && break
  sleep 1
done

[[ "$STATUS" == "Completed" ]] || fail "pipeline status = $STATUS, want Completed: $(jq -c '.stages[] | {id,name,status,lastErrorCode}' <<<"$SNAPSHOT")"

RETRY_ATTEMPT="$(jq -er --argjson id "$FIRST_STAGE_ID" \
  '.stages[] | select(.id == $id) | .retryAttempt // 0' <<<"$SNAPSHOT")"
[[ "$RETRY_ATTEMPT" == "1" ]] \
  || fail "first stage retryAttempt = $RETRY_ATTEMPT, want 1"

SECOND_STATUS="$(jq -er --argjson id "$FIRST_STAGE_ID" \
  '[.stages[] | select(.id != $id)] | .[0].status' <<<"$SNAPSHOT")"
[[ "$SECOND_STATUS" == "Completed" ]] \
  || fail "second stage status = $SECOND_STATUS, want Completed"

step "Shutting the worker down"
call POST /workers/shutdown "{\"workerId\":\"$WORKER_ID\",\"reason\":\"smoke complete\"}" >/dev/null

printf '\nPASS: pipeline %s Completed, first stage retried once, both stages Completed\n' "$PIPELINE_ID"
