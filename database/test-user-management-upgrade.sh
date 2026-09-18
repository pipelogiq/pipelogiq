#!/bin/sh
set -eu

# Only disposable Docker resources are used; no application database is read.
postgres_image="${POSTGRES_IMAGE:-postgres:16-alpine}"
liquibase_image="${LIQUIBASE_IMAGE:-pipelogiq-migrate-test-$$}"
# The baseline intentionally uses the previously shipped engine; upgrades use the release image.
baseline_liquibase_image="${BASELINE_LIQUIBASE_IMAGE:-liquibase/liquibase:4.25}"
built_migration_image=false
test_name="pipelogiq-users-upgrade-$$"
database_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
test_log=$(mktemp)
baseline_count=$(awk '/<changeSet id="add user activation status"/ { print count; exit } /<changeSet / { count++ }' "${database_dir}/changelog.xml")
test -n "${baseline_count}"

cleanup() {
  if [ "${built_migration_image}" = true ]; then
    docker image rm "${liquibase_image}" >/dev/null 2>&1 || true
  fi
  docker rm -f "${test_name}" >/dev/null 2>&1 || true
  docker network rm "${test_name}" >/dev/null 2>&1 || true
  rm -f "${test_log}"
}
trap cleanup EXIT INT TERM

if [ -z "${LIQUIBASE_IMAGE:-}" ]; then
  built_migration_image=true
  docker build -f "${database_dir}/../infra/docker/Dockerfile.migrate" \
    -t "${liquibase_image}" "${database_dir}/.." >/dev/null
fi

docker network create "${test_name}" >/dev/null
docker run --rm -d --name "${test_name}" --network "${test_name}" \
  -p 127.0.0.1::5432 -e POSTGRES_DB=users_upgrade \
  -e POSTGRES_USER=plq_test -e POSTGRES_PASSWORD=plq_test \
  "${postgres_image}" >/dev/null

attempt=0
until docker exec "${test_name}" pg_isready -U plq_test -d users_upgrade >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  test "${attempt}" -lt 30 || { echo 'Test PostgreSQL did not become ready' >&2; exit 1; }
  sleep 1
done

sql() {
  docker exec -i "${test_name}" psql -v ON_ERROR_STOP=1 -U plq_test -d users_upgrade "$@"
}
liquibase() {
  migration_image="$1"
  shift
  docker run --rm --network "${test_name}" \
    -v "${database_dir}:/liquibase/changelog:ro" "${migration_image}" \
    --url="jdbc:postgresql://${test_name}:5432/users_upgrade" \
    --username=plq_test --password=plq_test --changelog-file=changelog/changelog.xml "$@"
}

liquibase "${baseline_liquibase_image}" update-count --count="${baseline_count}" >"${test_log}" 2>&1 || { cat "${test_log}"; exit 1; }
sql >/dev/null <<'SQL'
INSERT INTO "user" (first_name, email, password, role, token_version) VALUES
  ('Admin', 'admin@example.test', 'test-only', ' SuperAdmin ', 4),
  ('Operator', 'operator@example.test', 'test-only', 'User', 7),
  ('Unknown', 'unknown@example.test', 'test-only', 'unexpected-role', 2),
  ('Duplicate', ' ADMIN@example.test ', 'test-only', 'Viewer', 1);
INSERT INTO application (name) VALUES ('Migration fixture');
INSERT INTO user_application (user_id, application_id)
  SELECT u.id, a.id FROM "user" u CROSS JOIN application a WHERE u.email='operator@example.test';
-- A previously changed password must preserve the real account's access.
UPDATE "user" SET password='test-replaced-hash' WHERE email='ww@gmail.com';
-- Renaming a demo identity does not make its unchanged password safe.
UPDATE "user" SET email='renamed-demo@example.invalid' WHERE email='jegor@gmail.com';
SQL

# A collision must stop the upgrade, never silently merge/delete accounts.
if liquibase "${liquibase_image}" update >"${test_log}" 2>&1; then
  echo 'Migration accepted duplicate normalized user emails' >&2
  exit 1
fi
grep -q 'User emails must be unique' "${test_log}" || { cat "${test_log}"; exit 1; }
fixture_count=$(sql -At <<'SQL'
SELECT count(*) FROM "user" WHERE email LIKE '%example.test%';
SQL
)
test "${fixture_count}" = 4
sql -c 'DELETE FROM "user" WHERE first_name = '\''Duplicate'\''' >/dev/null
liquibase "${liquibase_image}" update >"${test_log}" 2>&1 || { cat "${test_log}"; exit 1; }

verified=$(sql -At <<'SQL'
SELECT
  (SELECT count(*) FROM "user" WHERE is_active AND email LIKE '%example.test') = 3 AND
  (SELECT count(*) FROM "user" WHERE NOT is_active AND token_version=2 AND email IN ('renamed-demo@example.invalid','leo@gmail.com')) = 2 AND
  (SELECT is_active AND token_version=1 FROM "user" WHERE email='ww@gmail.com') AND
  (SELECT role='admin' AND token_version=5 FROM "user" WHERE email='admin@example.test') AND
  (SELECT role='operator' AND token_version=8 FROM "user" WHERE email='operator@example.test') AND
  (SELECT role='viewer' AND token_version=3 FROM "user" WHERE email='unknown@example.test') AND
  (SELECT count(*) FROM user_application ua JOIN "user" u ON u.id=ua.user_id
    JOIN application a ON a.id=ua.application_id
    WHERE u.email='operator@example.test' AND a.name='Migration fixture') = 1 AND
  (SELECT count(*) FROM pg_indexes WHERE indexname IN
    ('uq_user_email_normalized','idx_audit_log_action_ts_id','idx_audit_log_user_action_ts_id')) = 3;
SQL
)
test "${verified}" = t || { echo "Upgrade invariants failed: ${verified}" >&2; exit 1; }

liquibase "${liquibase_image}" update >"${test_log}" 2>&1 || { cat "${test_log}"; exit 1; }
fixture_versions=$(sql -At <<'SQL'
SELECT sum(token_version) FROM "user" WHERE email LIKE '%example.test';
SQL
)
test "${fixture_versions}" = 16
if sql -c "INSERT INTO \"user\" (first_name,email,password,role) VALUES ('Duplicate',' ADMIN@example.test ','test-only','viewer')" >"${test_log}" 2>&1; then
  echo 'Normalized email unique index did not reject duplicate' >&2
  exit 1
fi
grep -q 'uq_user_email_normalized' "${test_log}" || { cat "${test_log}"; exit 1; }
echo 'User upgrade: role normalization, session revocation, membership preservation, duplicates, demo credentials and re-apply passed'

# Include every PostgreSQL regression, including names without an underscore.
test_port=$(docker port "${test_name}" 5432/tcp | sed 's/.*://')
cd "${database_dir}/../apps/go"
export PIPELOGIQ_TEST_DATABASE_URL="postgres://plq_test:plq_test@127.0.0.1:${test_port}/users_upgrade?sslmode=disable"
go test -race -tags integration ./internal/store -run Postgres -count=1 -v
go test -race -tags 'pro integration' ./internal/pro -run Postgres -count=1 -v
