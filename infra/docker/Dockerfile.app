FROM node:20-alpine AS web-builder
WORKDIR /app
COPY apps/web/package*.json ./
RUN npm ci
COPY apps/web/ ./
RUN npm run build

FROM golang:1.24-alpine AS api-builder
ARG PIPELOGIQ_VERSION=dev
ARG PIPELOGIQ_COMMIT=unknown
ARG PIPELOGIQ_BUILD_DATE=unknown
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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags "-s -w -X pipelogiq/internal/version.Version=${PIPELOGIQ_VERSION} -X pipelogiq/internal/version.Commit=${PIPELOGIQ_COMMIT} -X pipelogiq/internal/version.Date=${PIPELOGIQ_BUILD_DATE}" \
  -o /out/pipelogiq-api ./cmd/api

FROM liquibase/liquibase:4.25 AS liquibase

# Web dashboard + API in a single container.
# nginx proxies /api/ and /ws to the co-located API at localhost:8080.
# Runs Liquibase migrations on startup before starting services.
FROM nginx:alpine
ARG PIPELOGIQ_VERSION=dev
ARG PIPELOGIQ_COMMIT=unknown
ARG PIPELOGIQ_BUILD_DATE=unknown
RUN apk add --no-cache supervisor ca-certificates curl bash openjdk17-jre

LABEL org.opencontainers.image.version="${PIPELOGIQ_VERSION}"
LABEL org.opencontainers.image.revision="${PIPELOGIQ_COMMIT}"
LABEL org.opencontainers.image.created="${PIPELOGIQ_BUILD_DATE}"

COPY --from=web-builder /app/dist /usr/share/nginx/html
COPY --from=api-builder /out/pipelogiq-api /app/pipelogiq-api
COPY --from=liquibase /liquibase /opt/liquibase

COPY database /app/database
COPY infra/docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY infra/docker/supervisord.conf /etc/supervisord.conf
COPY infra/docker/pipelogiq-app-entrypoint.sh /usr/local/bin/pipelogiq-app-entrypoint.sh

RUN chmod +x /usr/local/bin/pipelogiq-app-entrypoint.sh /app/pipelogiq-api
ENV PATH="/opt/liquibase:${PATH}"
ENV APP_VERSION="${PIPELOGIQ_VERSION}"
ENV APP_COMMIT="${PIPELOGIQ_COMMIT}"
ENV APP_BUILD_DATE="${PIPELOGIQ_BUILD_DATE}"

EXPOSE 80 8081
ENTRYPOINT ["/usr/local/bin/pipelogiq-app-entrypoint.sh"]
