FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download && go mod tidy
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o ingest-service ./cmd/server

FROM alpine:3.19
RUN addgroup -S appgroup && adduser -S appuser -G appgroup && \
    mkdir -p /app/uploads && chown appuser:appgroup /app/uploads
COPY --from=builder /app/ingest-service /ingest-service
RUN chown appuser:appgroup /ingest-service
USER appuser
EXPOSE 8083
HEALTHCHECK --interval=15s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost:8083/health || exit 1
ENTRYPOINT ["/ingest-service"]
