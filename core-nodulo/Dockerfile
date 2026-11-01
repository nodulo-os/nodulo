# syntax=docker/dockerfile:1

FROM golang:1.27.3-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Mantém o download de módulos em cache enquanto go.mod/go.sum não mudarem.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG MAIN_PACKAGE=.
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" \
    -o /out/api "${MAIN_PACKAGE}"

FROM alpine:latest AS runner

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -u 10001 -G app -h /nonexistent app \
    && mkdir -p /data/nodulo/ingestion \
    && chown -R app:app /data/nodulo

WORKDIR /app

COPY --from=builder --chown=app:app /out/api /app/api

ENV PORT=8080
EXPOSE 8080

USER app:app

# Confere o endpoint de saúde da API.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider "http://127.0.0.1:${PORT}/api/health" || exit 1

ENTRYPOINT ["/app/api"]
