# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy Go SDK (build context MUST be repo root)
COPY go-sdk /build/go-sdk

# Copy example service
COPY examples/go-http-service /build/examples/go-http-service

# Build the binary; go.mod replace points to local go-sdk
WORKDIR /build/examples/go-http-service
RUN go mod tidy && CGO_ENABLED=0 go build -o /go-http-service .

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates
WORKDIR /app

COPY --from=builder /go-http-service /app/go-http-service

ENV PORT=8088
EXPOSE 8088

CMD ["/app/go-http-service"]
