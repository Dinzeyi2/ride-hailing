# Multi-stage Dockerfile for Go services
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Copy the replaced dependency so go mod download can see it
COPY third_party ./third_party

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build argument for service name
ARG SERVICE_NAME

# Fail fast with a clear message instead of the cryptic "no Go files in
# /app/cmd" error you get from `go build ./cmd/` when SERVICE_NAME is unset
# (e.g. a Railway service pointed at this Dockerfile without a build arg).
RUN test -n "$SERVICE_NAME" || (echo "ERROR: SERVICE_NAME build arg is not set. Pass --build-arg SERVICE_NAME=<service> (e.g. auth, rides, geo, payments, mobile) or use one of the deploy/railway/<service>/Dockerfile files instead." >&2 && exit 1)
RUN test -d "cmd/$SERVICE_NAME" || (echo "ERROR: cmd/$SERVICE_NAME does not exist. Check the SERVICE_NAME build arg." >&2 && exit 1)

# Build the service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/service ./cmd/${SERVICE_NAME}

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /home/appuser

# Copy the binary from builder
COPY --from=builder /app/service .

# Run as non-root user
USER appuser

# Expose port
EXPOSE 8080

# Run the service
CMD ["./service"]
