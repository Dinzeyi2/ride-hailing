# One-off Railway service used to run database migrations against the
# Railway Postgres plugin. Deploy it, run it once (it exits immediately
# after migrating), then set it to sleep/remove it - it's not a long-running
# service.
#
# In the Railway dashboard: create a service from this Dockerfile path
# (deploy/railway/migrate/Dockerfile), set DATABASE_URL to reference the
# Postgres plugin (${{Postgres.DATABASE_URL}}), deploy, check the logs for
# "Migrations complete", then pause/delete the service.
FROM golang:1.24-alpine

RUN apk add --no-cache git

RUN go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.1

WORKDIR /app
COPY db/migrations ./db/migrations

# DATABASE_URL must be a standard postgres:// URL. Railway's Postgres plugin
# variable already includes sslmode where needed.
CMD ["sh", "-c", "migrate -path db/migrations -database \"$DATABASE_URL\" up && echo 'Migrations complete'"]
