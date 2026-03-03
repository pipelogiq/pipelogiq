#!/bin/sh
set -eu

LIQUIBASE_ENABLED_VALUE="${LIQUIBASE_ENABLED:-true}"

is_enabled() {
  case "$1" in
    1|true|TRUE|yes|YES|on|ON) return 0 ;;
    *) return 1 ;;
  esac
}

if is_enabled "$LIQUIBASE_ENABLED_VALUE"; then
  LIQUIBASE_CHANGELOG_FILE="${LIQUIBASE_CHANGELOG_FILE:-/app/database/changelog.xml}"
  LIQUIBASE_URL_VALUE="${LIQUIBASE_URL:-}"

  if [ -z "$LIQUIBASE_URL_VALUE" ]; then
    echo "[pipelogiq-app] LIQUIBASE_URL is required when LIQUIBASE_ENABLED=true" >&2
    exit 1
  fi

  if [ ! -f "$LIQUIBASE_CHANGELOG_FILE" ]; then
    echo "[pipelogiq-app] changelog file not found: $LIQUIBASE_CHANGELOG_FILE" >&2
    exit 1
  fi

  LIQUIBASE_CHANGELOG_ARG="$LIQUIBASE_CHANGELOG_FILE"
  case "$LIQUIBASE_CHANGELOG_FILE" in
    /app/*)
      LIQUIBASE_CHANGELOG_ARG="${LIQUIBASE_CHANGELOG_FILE#/app/}"
      ;;
  esac

  echo "[pipelogiq-app] running liquibase migrations"
  set -- --url="$LIQUIBASE_URL_VALUE" --searchPath="/app" --changeLogFile="$LIQUIBASE_CHANGELOG_ARG"

  if [ -n "${LIQUIBASE_USERNAME:-}" ]; then
    set -- "$@" --username="${LIQUIBASE_USERNAME}"
  fi
  if [ -n "${LIQUIBASE_PASSWORD:-}" ]; then
    set -- "$@" --password="${LIQUIBASE_PASSWORD}"
  fi

  liquibase "$@" update
  echo "[pipelogiq-app] liquibase migration completed"
else
  echo "[pipelogiq-app] skipping liquibase migration (LIQUIBASE_ENABLED=$LIQUIBASE_ENABLED_VALUE)"
fi

exec /usr/bin/supervisord -c /etc/supervisord.conf
