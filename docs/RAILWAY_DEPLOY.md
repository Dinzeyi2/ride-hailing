# Deploying to Railway

This guide deploys a **core subset** of the platform to Railway so a frontend
(e.g. a Lovable.dev app) can call it directly over HTTPS:

| Service  | Port (local default) | Covers |
|----------|-----------------------|--------|
| `auth`     | 8081 | Register/login, JWT issuance, RBAC |
| `mobile`   | 8087 | Rider/driver-facing gateway: rides, ratings, favorites, scheduling, documents, etc. |
| `rides`    | 8082 | Ride lifecycle, driver matching, surge pricing |
| `geo`      | 8083 | Driver location tracking / geospatial queries |
| `payments` | 8084 | Stripe charges, wallets, payouts |

Deliberately **not** deployed yet (add later the same way if you need them):
Kong, Istio, NATS, Prometheus/Grafana, self-hosted Sentry, MinIO, and the
remaining 9 microservices (`notifications`, `realtime`, `admin`, `promos`,
`scheduler`, `analytics`, `fraud`, `ml-eta`, `negotiation`). None of the core
5 services require any of them to start — `NATS_ENABLED` defaults to `false`
and the code just logs a warning and disables async events when it's off.

Two things were fixed in this repo to make this deployment topology work
(each service runs as its own independent container, unlike a single-box
docker-compose setup):

1. **`mobile` had no CORS middleware.** A browser-based frontend calling it
   directly would have been blocked. Added `middleware.CORS()`.
2. **JWT verification across separate containers.** The JWT key manager
   (`pkg/jwtkeys`) normally persists rotated signing keys to a local file. On
   Railway each service is its own container with its own ephemeral
   filesystem, so a key `auth` generates isn't visible to `mobile`/`rides`/
   etc. Fixed `pkg/middleware/auth.go` to fall back to the shared
   `JWT_SECRET` when a token's key ID can't be resolved locally — signature
   verification still fully rejects any token not actually signed with that
   secret, so this only fixes cross-container lookup, not security.
   **This means `JWT_SECRET` must be set to the same non-empty value on
   every service**, or auth will silently fail across services.

## 1. Create the Railway project

1. In Railway, **New Project → Deploy from GitHub repo** → select this repo.
   Railway will ask about a service — cancel/skip the auto-detected one, you
   will add the 5 services manually below (or delete it after).
2. **Add a Postgres plugin** (`+ New → Database → PostgreSQL`).
3. **Add a Redis plugin** (`+ New → Database → Redis`).

Note Postgres's plugin variables (Settings → Variables on the Postgres
service): `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE` (default
database name is `railway`). Note Redis's `REDISHOST`, `REDISPORT`,
`REDISPASSWORD`.

## 2. Run migrations once

1. `+ New → Empty Service` (or GitHub repo again) named `migrate`.
2. Settings → Build → **Dockerfile Path**: `deploy/railway/migrate/Dockerfile`.
3. Variables → add `DATABASE_URL` = `${{Postgres.DATABASE_URL}}` (reference
   the Postgres plugin's own connection string variable).
4. Deploy. Check the logs for `Migrations complete` (48 migrations run).
5. Once confirmed, pause or delete this service — it's a one-off job, not a
   long-running service. Re-run it (redeploy) any time you need to apply new
   migrations later.

## 3. Create the 5 app services

For each of `auth`, `mobile`, `rides`, `geo`, `payments`:

1. `+ New → GitHub Repo` → this repo again (Railway lets you deploy the same
   repo as multiple services).
2. Name the service (e.g. `auth`).
3. Settings → Build → **Dockerfile Path**: `deploy/railway/<name>/Dockerfile`
   (e.g. `deploy/railway/auth/Dockerfile`). Root/build context stays the
   repo root — don't change it.
4. Settings → Networking → **Generate Domain** to get a public HTTPS URL.
5. Variables (add these on **every one of the 5 services**, identical
   values, unless noted otherwise):

   ```
   ENVIRONMENT=production
   JWT_SECRET=<a long random string - same value on all 5 services>

   DB_HOST=${{Postgres.PGHOST}}
   DB_PORT=${{Postgres.PGPORT}}
   DB_USER=${{Postgres.PGUSER}}
   DB_PASSWORD=${{Postgres.PGPASSWORD}}
   DB_NAME=${{Postgres.PGDATABASE}}
   DB_SSLMODE=disable

   REDIS_HOST=${{Redis.REDISHOST}}
   REDIS_PORT=${{Redis.REDISPORT}}
   REDIS_PASSWORD=${{Redis.REDISPASSWORD}}

   CORS_ORIGINS=https://your-app.lovable.app,https://your-app-preview.lovable.app
   ```

   `mobile` additionally needs its own DB vars spelled the same way (it reads
   `DB_HOST`/`DB_PORT`/`DB_USER`/`DB_PASSWORD`/`DB_NAME` directly, same
   names as above) — nothing extra to add there, the block above covers it.

   `payments` additionally needs:
   ```
   STRIPE_API_KEY=sk_test_...
   ```

   Leave `PORT` unset — Railway injects it automatically and every service
   already reads `PORT` from the environment.

6. Deploy. Check each service's logs for `Server starting` (or equivalent)
   with no fatal DB/Redis connection errors.

## 4. Verify

```bash
curl https://auth-production-XXXX.up.railway.app/healthz
curl -X POST https://auth-production-XXXX.up.railway.app/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"Passw0rd!","phone_number":"+10000000000","first_name":"Test","last_name":"User","role":"rider"}'
```

Use the JWT from the login/register response as `Authorization: Bearer <token>`
against `mobile`/`rides`/`geo`/`payments` endpoints (exact routes are in
`docs/API.md`).

## 5. Point the Lovable.dev frontend at it

In Lovable, set environment/config values (or hardcode in your API client)
to the Railway-issued URLs, one per service, e.g.:

```
VITE_AUTH_API_URL=https://auth-production-XXXX.up.railway.app
VITE_MOBILE_API_URL=https://mobile-production-XXXX.up.railway.app
```

Call `auth` for register/login (store the returned JWT), then send it as a
Bearer token on every request to `mobile`/`rides`/`geo`/`payments`. Make sure
`CORS_ORIGINS` on every Railway service includes the exact origin Lovable
serves your app from (including the preview-domain origin, if different from
the published one) — the middleware does an exact string match, not a
wildcard subdomain match.

## Known gaps in this minimal deploy

- `mobile` calls out to `NOTIFICATIONS_SERVICE_URL` and
  `PROMOS_SERVICE_URL` for a few specific features (scheduled-ride
  notifications, promo codes). Those env vars are unset in this setup, so
  only those specific calls will fail/time out — everything else on
  `mobile` works normally. Deploy `notifications`/`promos` the same way
  (copy an existing Dockerfile, swap the `cmd/<service>` path) and set
  those URLs once you need those features.
- JWT key **rotation** across containers isn't fully seamless without a
  shared store (Vault, via `JWT_KEYS_VAULT_*`) — the fallback fix above
  covers normal signing/verification correctly as long as `JWT_SECRET` is
  set and identical everywhere, which is sufficient for an MVP.
- No API gateway (Kong) in front of these yet, so the frontend talks to
  each service's own Railway domain directly. Fine for an MVP; add Kong
  later if you want a single entrypoint/rate limiting/auth-at-the-edge.
