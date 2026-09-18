FROM node:24-alpine AS web-builder
WORKDIR /app
COPY apps/web/package*.json ./
RUN npm ci
COPY apps/web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.8-alpine AS api-builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG PIPELOGIQ_VERSION=v0.5.0
ARG PIPELOGIQ_COMMIT=unknown
ARG PIPELOGIQ_BUILD_DATE=unknown
ARG GO_BUILD_TAGS=""
ARG PIPELOGIQ_LICENSE_PUBKEY=""
WORKDIR /src
COPY apps/go/go.mod ./
COPY apps/go/go.sum ./
RUN set -eux; \
  ok=0; \
  for i in 1 2 3 4 5; do \
    if go mod download; then ok=1; break; fi; \
    echo "go mod download failed (attempt $i), retrying..."; \
    sleep $((i * 2)); \
  done; \
  test "$ok" -eq 1
COPY apps/go ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -tags "${GO_BUILD_TAGS}" \
  -ldflags "-s -w -X pipelogiq/internal/license.PublicKeyBase64=${PIPELOGIQ_LICENSE_PUBKEY} -X pipelogiq/internal/version.Version=${PIPELOGIQ_VERSION} -X pipelogiq/internal/version.Commit=${PIPELOGIQ_COMMIT} -X pipelogiq/internal/version.Date=${PIPELOGIQ_BUILD_DATE}" \
  -o /out/pipelogiq-api ./cmd/api

# Web dashboard + API in a single container: nginx proxies /api/ and /ws to the
# co-located API. Migrations are a separate image (Dockerfile.migrate) that runs
# before this one starts. The container runs as an unprivileged user; nginx
# listens on 8080, so publish that port.
FROM nginx:1.30.5-alpine
ARG PIPELOGIQ_VERSION=v0.5.0
ARG PIPELOGIQ_COMMIT=unknown
ARG PIPELOGIQ_BUILD_DATE=unknown
RUN apk add --no-cache supervisor ca-certificates curl

LABEL org.opencontainers.image.version="${PIPELOGIQ_VERSION}"
LABEL org.opencontainers.image.revision="${PIPELOGIQ_COMMIT}"
LABEL org.opencontainers.image.created="${PIPELOGIQ_BUILD_DATE}"

COPY --from=web-builder /app/dist /usr/share/nginx/html
COPY --from=api-builder /out/pipelogiq-api /app/pipelogiq-api
COPY infra/docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY infra/docker/supervisord.conf /etc/supervisord.conf
COPY infra/docker/pipelogiq-app-entrypoint.sh /usr/local/bin/pipelogiq-app-entrypoint.sh

RUN chmod +x /usr/local/bin/pipelogiq-app-entrypoint.sh /app/pipelogiq-api \
  && mkdir -p /var/cache/nginx /var/run/nginx /var/log/supervisor \
  && chown -R nginx:nginx /var/cache/nginx /var/run/nginx /var/log/nginx /var/log/supervisor /etc/nginx/conf.d /usr/share/nginx/html
# The image runs as nginx, which cannot write the default pid path. Match the
# whole directive rather than one spelling of it: the base image writes
# "pid        /run/nginx.pid;", so a literal /var/run pattern silently misses
# and nginx dies on start with EACCES.
RUN sed -i -E 's#^pid[[:space:]]+[^;]+;#pid /tmp/nginx.pid;#' /etc/nginx/nginx.conf \
  && grep -q '^pid /tmp/nginx.pid;' /etc/nginx/nginx.conf
USER nginx
ENV APP_VERSION="${PIPELOGIQ_VERSION}"
ENV APP_COMMIT="${PIPELOGIQ_COMMIT}"
ENV APP_BUILD_DATE="${PIPELOGIQ_BUILD_DATE}"

EXPOSE 8080 8081 8088 9091
ENTRYPOINT ["/usr/local/bin/pipelogiq-app-entrypoint.sh"]
