#!/bin/sh
set -eu

# Migrations no longer run here. They are applied by the pipelogiq-migrate image
# before this container starts (Compose depends_on / Kubernetes Job), so a failed
# migration stops the rollout instead of putting the API into a restart loop.
if [ "${LIQUIBASE_ENABLED:-false}" = "true" ]; then
  echo "[pipelogiq-app] LIQUIBASE_ENABLED is no longer honoured here; run the pipelogiq-migrate image before starting the app" >&2
fi

exec /usr/bin/supervisord -c /etc/supervisord.conf
