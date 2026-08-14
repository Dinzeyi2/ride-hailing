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

# Build argument for service name. Railway does not automatically turn a
# runtime SERVICE_NAME variable into a Docker build argument, so keep a safe
# default for auto-detected deployments. Override it with
# `--build-arg SERVICE_NAME=rides` (or use deploy/railway/<service>/Dockerfile)
# when building another service.
ARG SERVICE_NAME=auth

# Fail with a useful message if a bad service name is supplied, then build it.
RUN test -n "${SERVICE_NAME}" \
    && test -f "./cmd/${SERVICE_NAME}/main.go" \
    || (echo >&2 "Invalid SERVICE_NAME '${SERVICE_NAME}'. Expected a directory under cmd/ (for example: auth, rides, geo, payments, or mobile)."; exit 1)
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/service "./cmd/${SERVICE_NAME}"

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
