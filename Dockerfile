FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o auditor .

FROM alpine:3.18
COPY --from=builder /app/auditor /auditor
ENTRYPOINT ["/auditor"]
