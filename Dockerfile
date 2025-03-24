# Build stage
FROM golang:1.21-alpine AS builder
ARG VERSION=dev
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.version=${VERSION}" \
    -o auditor ./cmd/namespace-auditor

# Runtime stage
FROM alpine:3.18
RUN apk --no-cache add ca-certificates
WORKDIR /
COPY --from=builder /app/auditor /auditor
USER nobody:nobody
ENTRYPOINT ["/auditor"]
