# One-domain Railway deployment for the five core APIs. The gateway is the
# only public listener; service processes bind to private loopback ports.
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git build-base
WORKDIR /app
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download
COPY . .

# Alpine's /bin/sh does not exit when an intermediate command in a for-loop
# fails. Build the largest binary first, fail explicitly on every target, and
# verify the complete runtime set so a partial image can never be published.
RUN set -eux; \
    mkdir -p /out; \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mobile ./cmd/mobile; \
    for service in auth rides payments railway-gateway; do \
      CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o "/out/${service}" "./cmd/${service}" || exit 1; \
    done; \
    CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/geo ./cmd/geo; \
    for service in mobile auth rides geo payments railway-gateway; do test -x "/out/${service}"; done
RUN GOBIN=/out go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.1

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=builder /out/ ./bin/
COPY db/migrations ./db/migrations
COPY deploy/railway/all-in-one/start.sh ./start.sh
RUN chmod +x ./start.sh && chown -R app:app /app
USER app

# Railway supplies PORT. start.sh reserves it for railway-gateway.
CMD ["./start.sh"]
