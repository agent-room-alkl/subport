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

# 3) Minimal runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/subport /app/subport
COPY --from=web /web/dist /app/web/dist
COPY deploy/sql /app/deploy/sql
USER app
ENV SUBPORT_ADDR=:8080 \
    SUBPORT_WEB=/app/web/dist
EXPOSE 8080
ENTRYPOINT ["/app/subport"]
