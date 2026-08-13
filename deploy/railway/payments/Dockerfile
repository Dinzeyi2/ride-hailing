# Railway build for the payments service.
# In the Railway dashboard, set this service's "Dockerfile Path" to
# deploy/railway/payments/Dockerfile and leave the build context at the repo root.
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/service ./cmd/payments

FROM alpine:latest

RUN apk --no-cache add ca-certificates
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /home/appuser

COPY --from=builder /app/service .

USER appuser

# Railway injects PORT at runtime; the service reads it from the environment
# and writes its JWT key file under the working directory (owned by appuser).
CMD ["./service"]
