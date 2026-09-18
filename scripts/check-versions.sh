#!/usr/bin/env bash
# Check source/release metadata against VERSION, optionally requiring an exact tag.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$ROOT/scripts/check-versions.py" "$@"
