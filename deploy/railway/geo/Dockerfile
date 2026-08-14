# Railway build for the geo service.
# In the Railway dashboard, set this service's "Dockerfile Path" to
# deploy/railway/geo/Dockerfile and leave the build context at the repo root.
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git make build-base

WORKDIR /app

COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download

COPY . .

# H3's Go bindings contain C sources and require CGO.
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -o /app/service ./cmd/geo

FROM alpine:latest

RUN apk --no-cache add ca-certificates
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /home/appuser

COPY --from=builder /app/service .

USER appuser

# Railway injects PORT at runtime; the service reads it from the environment
# and writes its JWT key file under the working directory (owned by appuser).
CMD ["./service"]
