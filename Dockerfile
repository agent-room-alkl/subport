# syntax=docker/dockerfile:1

# 1) Build the Vue frontend -> /web/dist
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2) Build the Go binary (static, pure Go — no CGO)
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/subport ./cmd/subport

# 3) Runtime. Claude's sessionKey -> OAuth fallback needs a browser-fingerprint
# capable HTTP client when the native Go request is challenged by Cloudflare.
FROM python:3.13-slim-bookworm
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir curl_cffi==0.16.3 \
    && useradd --create-home --uid 10001 app
WORKDIR /app
COPY --from=build /out/subport /app/subport
COPY --from=web /web/dist /app/web/dist
COPY deploy/sql /app/deploy/sql
COPY scripts/claude_session_exchange.py /app/scripts/claude_session_exchange.py
USER app
ENV SUBPORT_ADDR=:8080 \
    SUBPORT_WEB=/app/web/dist \
    SUBPORT_PYTHON=/usr/local/bin/python \
    SUBPORT_CLAUDE_EXCHANGE_SCRIPT=/app/scripts/claude_session_exchange.py
EXPOSE 8080
ENTRYPOINT ["/app/subport"]
