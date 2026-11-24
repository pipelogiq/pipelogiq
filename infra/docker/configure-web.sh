#!/bin/sh
set -eu

# Keep immutable templates so restarting with a different prefix is safe.
templates=${1:-/opt/pipelogiq/web-templates}
web_root=${2:-/usr/share/nginx/html}
nginx_config=${3:-/etc/nginx/conf.d/default.conf}
base=${PIPELOGIQ_BASE_PATH:-}
if [ -n "$base" ] && ! printf '%s\n' "$base" | LC_ALL=C grep -Eq '^(/[a-zA-Z0-9_-]+)+$'; then
  echo 'Invalid PIPELOGIQ_BASE_PATH: use /pipelogiq or an empty value (no trailing slash)' >&2
  exit 1
fi

root_location=''
if [ -n "$base" ]; then
  root_location="location = $base { return 308 $base/\$is_args\$args; } location / { return 404; }"
fi
sed -e "s|__PIPELOGIQ_BASE_PATH__|$base|g" \
    -e "s|__PIPELOGIQ_ROOT_LOCATION__|$root_location|g" \
    "$templates/nginx.conf" > "$nginx_config"
sed -e "s|<base href=\"/\" />|<base href=\"$base/\" />|" \
    -e "s|name=\"pipelogiq-base-path\" content=\"\"|name=\"pipelogiq-base-path\" content=\"$base\"|" \
    "$templates/index.html" > "$web_root/index.html"
