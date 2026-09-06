#!/usr/bin/env bash
#
# End-to-end smoke test for schedules (docs/schedules.md).
#
# Registers an "@every 10s" schedule whose single stage is served by an HTTP
# worker, then asserts that three runs fired, each created its own pipeline, and
# every pipeline reached Completed.
#
# Usage:
#   make compose-up                      # in another terminal
#   BOOTSTRAP_API_KEY=<key> scripts/schedules-smoke.sh
#
set -euo pipefail

API="${PIPELOGIQ_EXTERNAL_URL:-http://localhost:8081}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HANDLER="${SMOKE_HANDLER:-ScheduleSmokeHandler}"
RUN_ID="${SMOKE_RUN_ID:-$(date +%s)-$$}"
SCHEDULE="smoke-$RUN_ID"
WANT_RUNS="${SMOKE_WANT_RUNS:-3}"

if [[ -z "${BOOTSTRAP_API_KEY:-}" && -f "$ROOT/.env" ]]; then
  BOOTSTRAP_API_KEY="$(grep -E '^BOOTSTRAP_API_KEY=' "$ROOT/.env" | tail -1 | cut -d= -f2- || true)"
fi
KEY="${BOOTSTRAP_API_KEY:-}"

if [[ -z "$KEY" ]]; then
  echo "BOOTSTRAP_API_KEY is not set and .env has no value for it." >&2
  exit 2
fi

for tool in curl jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required" >&2; exit 2; }
done

fail() { echo "FAIL: $*" >&2; cleanup; exit 1; }
step() { printf '\n=== %s\n' "$*"; }

SESSION=""
cleanup() {
  # Always archive the schedule: a leftover @every 10s schedule would keep
  # creating pipelines on a shared dev stack long after the test ended.
  curl -sS -o /dev/null -X POST "$API/schedules/$SCHEDULE/archive" \
    -H "X-API-Key: $KEY" 2>/dev/null || true
}
trap cleanup EXIT

call() {
  local method="$1" path="$2" body="${3:-}" expect="${4:-2xx}"
  local response status
  if [[ -n "$body" ]]; then
    response="$(curl -sS -w $'\n%{http_code}' -X "$method" "$API$path" \
      -H "X-API-Key: $KEY" ${SESSION:+-H "X-Worker-Session: $SESSION"} \
      -H 'Content-Type: application/json' -d "$body")"
  else
    response="$(curl -sS -w $'\n%{http_code}' -X "$method" "$API$path" \
      -H "X-API-Key: $KEY" ${SESSION:+-H "X-Worker-Session: $SESSION"})"
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
  curl -fsS "$API/healthz" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "$API/healthz" >/dev/null || fail "external API never became healthy"

step "Bootstrapping the HTTP worker"
BOOTSTRAP="$(call POST /workers/bootstrap \
  "{\"workerName\":\"schedule-smoke-$RUN_ID\",\"supportedHandlers\":[\"$HANDLER\"]}")"
SESSION="$(jq -er .workerSessionToken <<<"$BOOTSTRAP")"
WORKER_ID="$(jq -er .workerId <<<"$BOOTSTRAP")"
APP_ID="$(jq -er .application.appId <<<"$BOOTSTRAP")"
QUEUE="${APP_ID}_${HANDLER}_StageNext"
echo "worker=$WORKER_ID queue=$QUEUE schedule=$SCHEDULE"

# The body is built with jq so no amount of shell quoting can mangle the nested
# JSON string that carries the placeholders.
STAGE_INPUT='{"scheduledFor":"{{scheduledFor}}","runId":"{{runId}}"}'
UPSERT_BODY="$(jq -n \
  --arg name "$SCHEDULE" \
  --arg handler "$HANDLER" \
  --arg input "$STAGE_INPUT" \
  '{
     kind: "Cron",
     cronExpression: "@every 10s",
     timeZone: "UTC",
     overlapPolicy: "Skip",
     catchupPolicy: "None",
     definition: {
       name: $name,
       stages: [ { stageName: "tick", stageHandlerName: $handler, input: $input } ]
     }
   }')"

step "Registering an @every 10s schedule"
CREATED="$(call PUT "/schedules/$SCHEDULE" "$UPSERT_BODY" 201)"
echo "scheduleId=$(jq -er .id <<<"$CREATED") version=$(jq -er .definitionVersion <<<"$CREATED")"
[[ "$(jq -er .nextRunAt <<<"$CREATED")" != "null" ]] || fail "the schedule was not planned"

step "Re-applying the same definition must be a no-op"
REAPPLIED="$(call PUT "/schedules/$SCHEDULE" "$UPSERT_BODY" 200)"
[[ "$(jq -er .definitionVersion <<<"$REAPPLIED")" == "$(jq -er .definitionVersion <<<"$CREATED")" ]] \
  || fail "an unchanged definition bumped definitionVersion"

# serve_one pulls at most one job and completes it. Returns 1 when the queue was
# empty, so the caller can keep polling until the schedule has fired enough times.
serve_one() {
  local job token stage exec_id attempt
  job="$(curl -sS -X POST "$API/jobs/pull" \
    -H "X-API-Key: $KEY" -H "X-Worker-Session: $SESSION" \
    -H 'Content-Type: application/json' \
    -d "{\"queue\":\"$QUEUE\",\"waitSeconds\":15}")"
  [[ -n "$job" ]] || return 1

  token="$(jq -er .token <<<"$job")"
  stage="$(jq -er .payload.stageId <<<"$job")"
  exec_id="$(jq -er .payload.executionId <<<"$job")"
  attempt="$(jq -er '.payload.attempt // 1' <<<"$job")"

  # The placeholders must have been substituted before the stage was dispatched.
  local input
  input="$(jq -er '.payload.input' <<<"$job")"
  [[ "$input" != *'{{scheduledFor}}'* ]] || fail "scheduledFor was not substituted: $input"
  [[ "$input" != *'{{runId}}'* ]] || fail "runId was not substituted: $input"

  local lease
  lease="$(call POST "/stages/$stage/lease/acquire" \
    "{\"executionId\":\"$exec_id\",\"workerId\":\"$WORKER_ID\"}")"
  if [[ "$(jq -er .acquired <<<"$lease")" != "true" ]]; then
    call POST /jobs/ack "{\"token\":\"$token\",\"requeue\":false}" >/dev/null
    return 0
  fi

  local result_body
  result_body="$(jq -n \
    --argjson stageId "$stage" \
    --arg executionId "$exec_id" \
    --argjson attempt "$attempt" \
    '{stageId: $stageId, executionId: $executionId, attempt: $attempt, isSuccess: true, result: "{\"ok\":true}"}')"
  call POST "/stages/$stage/result" "$result_body" 202 >/dev/null
  call POST /jobs/ack "{\"token\":\"$token\",\"requeue\":false}" >/dev/null
  return 0
}

step "Serving stages until $WANT_RUNS runs have fired"
DEADLINE=$((SECONDS + 180))
CREATED_RUNS=0
while (( SECONDS < DEADLINE )); do
  serve_one || true
  RUNS="$(call GET "/schedules/$SCHEDULE/runs?limit=50")"
  CREATED_RUNS="$(jq -r '[.runs[] | select(.outcome == "Created")] | length' <<<"$RUNS")"
  echo "created runs so far: $CREATED_RUNS"
  (( CREATED_RUNS >= WANT_RUNS )) && break
done

(( CREATED_RUNS >= WANT_RUNS )) \
  || fail "only $CREATED_RUNS runs fired in 180s, want $WANT_RUNS: $(jq -c '.runs' <<<"$RUNS")"

step "Draining any stage still in flight"
for _ in $(seq 1 5); do serve_one || break; done

step "Asserting every run created its own pipeline and completed"
DEADLINE=$((SECONDS + 90))
while (( SECONDS < DEADLINE )); do
  RUNS="$(call GET "/schedules/$SCHEDULE/runs?limit=50")"
  PIPELINE_IDS="$(jq -r '[.runs[] | select(.outcome == "Created") | .pipelineId] | .[]' <<<"$RUNS")"
  DISTINCT="$(sort -u <<<"$PIPELINE_IDS" | grep -c . || true)"
  TOTAL="$(grep -c . <<<"$PIPELINE_IDS" || true)"
  [[ "$DISTINCT" == "$TOTAL" ]] || fail "$TOTAL runs share only $DISTINCT pipelines: a tick fired twice"

  COMPLETED=0
  for pid in $PIPELINE_IDS; do
    status="$(jq -er .status <<<"$(call GET "/pipelines/$pid")")"
    [[ "$status" == "Completed" ]] && COMPLETED=$((COMPLETED + 1))
  done
  echo "completed pipelines: $COMPLETED of $TOTAL"
  (( COMPLETED >= WANT_RUNS )) && break

  serve_one || true
done

(( COMPLETED >= WANT_RUNS )) \
  || fail "only $COMPLETED pipelines reached Completed, want $WANT_RUNS"

step "Checking the run history is well formed"
jq -e --argjson want "$WANT_RUNS" '
  ([.runs[] | select(.outcome == "Created")] | length) >= $want
  and ([.runs[] | select(.outcome == "Created") | .trigger] | all(. == "Cron" or . == "Catchup"))
  and ([.runs[] | select(.outcome == "Created") | .lagSeconds] | all(. >= 0))
  and ([.runs[] | .scheduledFor] | (length == (unique | length)))
' <<<"$RUNS" >/dev/null || fail "run history is malformed: $(jq -c '.runs' <<<"$RUNS")"

step "Pause, resume, and trigger"
[[ "$(jq -er .status <<<"$(call POST "/schedules/$SCHEDULE/pause")")" == "Paused" ]] \
  || fail "pause did not take effect"
RESUMED="$(call POST "/schedules/$SCHEDULE/resume")"
[[ "$(jq -er .status <<<"$RESUMED")" == "Active" ]] || fail "resume did not take effect"
[[ "$(jq -er .nextRunAt <<<"$RESUMED")" != "null" ]] || fail "resume did not re-plan the cursor"

MANUAL="$(call POST "/schedules/$SCHEDULE/trigger" '{}' 201)"
[[ "$(jq -er .trigger <<<"$MANUAL")" == "Manual" ]] || fail "manual trigger was not labelled Manual"
echo "manual run $(jq -er .runId <<<"$MANUAL") created pipeline $(jq -er .pipelineId <<<"$MANUAL")"

step "Archiving"
[[ "$(jq -er .status <<<"$(call POST "/schedules/$SCHEDULE/archive")")" == "Archived" ]] \
  || fail "archive did not take effect"
call POST "/schedules/$SCHEDULE/trigger" '{}' 409 >/dev/null

call POST /workers/shutdown "{\"workerId\":\"$WORKER_ID\",\"reason\":\"smoke complete\"}" >/dev/null

printf '\nPASS: %s fired %s runs, each with its own Completed pipeline\n' "$SCHEDULE" "$CREATED_RUNS"
