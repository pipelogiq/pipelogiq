#!/bin/sh
set -eu

# Applies historical changeSets before the reliability additions,
# inserts pre-upgrade rows, then applies the current changelog and verifies the
# additive defaults. Override the images when an internal registry is required.
postgres_image="${POSTGRES_IMAGE:-postgres:16-alpine}"
liquibase_image="${LIQUIBASE_IMAGE:-pipelogiq-migrate-test-$$}"
# The baseline intentionally uses the previously shipped engine; upgrades use the release image.
baseline_liquibase_image="${BASELINE_LIQUIBASE_IMAGE:-liquibase/liquibase:4.25}"
built_migration_image=false
test_suffix="$$"
network_name="pipelogiq-migration-test-network-${test_suffix}"
postgres_name="pipelogiq-migration-test-postgres-${test_suffix}"
database_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
baseline_change_set_count=$(awk '/<changeSet id="add pipeline idempotency metadata"/ { print count; exit } /<changeSet / { count++ }' "${database_dir}/changelog.xml")
test -n "${baseline_change_set_count}"

cleanup() {
  if [ "${built_migration_image}" = true ]; then
    docker image rm "${liquibase_image}" >/dev/null 2>&1 || true
  fi
  docker rm -f "${postgres_name}" >/dev/null 2>&1 || true
  docker network rm "${network_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

if [ -z "${LIQUIBASE_IMAGE:-}" ]; then
  built_migration_image=true
  docker build -f "${database_dir}/../infra/docker/Dockerfile.migrate" \
    -t "${liquibase_image}" "${database_dir}/.." >/dev/null
fi

docker network create "${network_name}" >/dev/null
docker run --rm -d \
  --name "${postgres_name}" \
  --network "${network_name}" \
  -e POSTGRES_DB=pipelogiq_migration_test \
  -e POSTGRES_USER=pipelogiq_test \
  -e POSTGRES_PASSWORD=pipelogiq_test \
  "${postgres_image}" >/dev/null

attempt=0
until docker exec "${postgres_name}" pg_isready -U pipelogiq_test -d pipelogiq_migration_test >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "${attempt}" -ge 30 ]; then
    echo "temporary PostgreSQL did not become ready" >&2
    exit 1
  fi
  sleep 1
done

run_liquibase() {
  migration_image="$1"
  shift
  docker run --rm \
    --network "${network_name}" \
    -v "${database_dir}:/liquibase/changelog:ro" \
    "${migration_image}" \
    --url="jdbc:postgresql://${postgres_name}:5432/pipelogiq_migration_test" \
    --username=pipelogiq_test \
    --password=pipelogiq_test \
    --changelog-file=changelog/changelog.xml \
    "$@"
}

run_liquibase "${baseline_liquibase_image}" update-count --count="${baseline_change_set_count}" >/dev/null

docker exec "${postgres_name}" psql \
  -v ON_ERROR_STOP=1 \
  -U pipelogiq_test \
  -d pipelogiq_migration_test \
  -c "
DO \$\$
DECLARE
  pipeline_id_value integer;
  stage_id_value integer;
BEGIN
  INSERT INTO pipeline (name, status, is_completed)
  VALUES ('legacy-pipeline', 'NotStarted', false)
  RETURNING id INTO pipeline_id_value;

  INSERT INTO stage (name, status, pipeline_id)
  VALUES ('legacy-stage', 'NotStarted', pipeline_id_value)
  RETURNING id INTO stage_id_value;

  INSERT INTO stage_options (stage_id, max_retries, retry_interval)
  VALUES (stage_id_value, 2, 5);

  INSERT INTO pipeline_context_item (key, value, value_type, pipeline_id)
  VALUES ('legacy-key', 'legacy-value', 'System.String', pipeline_id_value);
END
\$\$;
" >/dev/null

run_liquibase "${liquibase_image}" update >/dev/null

verified=$(docker exec "${postgres_name}" psql \
  -v ON_ERROR_STOP=1 \
  -U pipelogiq_test \
  -d pipelogiq_migration_test \
  -Atc "
SELECT
  (SELECT count(*) FROM pipeline
    WHERE name = 'legacy-pipeline'
      AND idempotency_key IS NULL
      AND request_hash IS NULL) = 1
  AND
  (SELECT count(*) FROM stage
    WHERE name = 'legacy-stage'
      AND execution_attempt = 0
      AND execution_id IS NULL
      AND lease_owner IS NULL
      AND last_error_code IS NULL) = 1
  AND
  (SELECT count(*) FROM pipeline_context_item
    WHERE key = 'legacy-key'
      AND is_sensitive = false) = 1
  AND
  (SELECT count(*) FROM pg_indexes
    WHERE indexname = 'uq_pipeline_application_idempotency_key') = 1;
")

if [ "${verified}" != "t" ]; then
  echo "reliability migration verification failed: ${verified}" >&2
  exit 1
fi

echo "reliability migration upgrade test passed"
