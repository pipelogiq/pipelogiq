#!/usr/bin/env bash
# Fails when an external API route exists in code but not in the OpenAPI contract.
# The contract is what the SDK and integrators build against, so a route that is
# not in it is either undocumented or should not be public.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
routes=$(grep -oE 'router\.(Get|Post|Put|Delete)\("[^"]+"' "$ROOT/apps/go/internal/api/external.go" \
  | sed -E 's/.*\("//; s/"$//' | grep -vE '^/(healthz|readyz)$' | sort -u)
paths=$(grep -oE '^  /[^:]+' "$ROOT/docs/openapi.external.yaml" | sed 's/^  //' | sort -u)
missing=$(comm -23 <(echo "$routes") <(echo "$paths") || true)
if [ -n "$missing" ]; then
  echo "External routes missing from docs/openapi.external.yaml:" >&2
  echo "$missing" >&2
  exit 1
fi
echo "OpenAPI covers every external route ($(echo "$routes" | wc -l | tr -d ' ') paths)."
