#!/usr/bin/env bash
# Every place the server version is written must agree: README badge, CHANGELOG
# heading, OpenAPI info.version and compose pinning. Drift here is what a client
# notices first.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
changelog=$(grep -m1 -oE '^## \[[0-9][^]]*\]' "$ROOT/CHANGELOG.md" | tr -d '[]# ')
readme=$(grep -m1 -oE 'Status: Preview v[0-9][^]]*' "$ROOT/README.md" | sed 's/Status: Preview v//')
openapi=$(grep -m1 -oE '^  version: "[^"]+"' "$ROOT/docs/openapi.external.yaml" | sed 's/.*"\(.*\)"/\1/')
quickstart=$(grep -m1 -oE 'PIPELOGIQ_VERSION=v[0-9][^ ]*' "$ROOT/docs/quickstart.md" | sed 's/PIPELOGIQ_VERSION=v//')
status=0
for pair in "README:$readme" "OpenAPI:$openapi" "quickstart:$quickstart"; do
  name=${pair%%:*}; value=${pair#*:}
  if [ "$value" != "$changelog" ]; then
    echo "version drift: CHANGELOG says $changelog, $name says $value" >&2; status=1
  fi
done
[ $status -eq 0 ] && echo "versions agree: $changelog"
exit $status
