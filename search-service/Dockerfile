# Use full Go image (not alpine) so CGO is available for -race in tests
FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o search-service ./cmd/server

FROM alpine:3.19
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
COPY --from=builder /app/search-service /search-service
RUN chown appuser:appgroup /search-service
USER appuser
EXPOSE 8082
HEALTHCHECK --interval=15s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost:8082/health || exit 1
ENTRYPOINT ["/search-service"]
