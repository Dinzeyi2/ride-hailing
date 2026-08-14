# Deploying to Railway

This guide deploys the frontend-facing **core platform** to Railway so a
Lovable.dev app can call one public HTTPS domain. The repository's
`railway.json` selects the all-in-one image automatically.

| Service  | Port (local default) | Covers |
|----------|-----------------------|--------|
| `auth`     | 8081 | Register/login, JWT issuance, RBAC |
| `mobile`   | 8087 | Rider/driver-facing gateway: rides, ratings, favorites, scheduling, documents, etc. |
| `rides`    | 8082 | Ride lifecycle, driver matching, surge pricing |
| `geo`      | 8083 | Driver location tracking / geospatial queries |
| `payments` | 8084 | Stripe charges, wallets, payouts |

The `mobile` process already contains most extended product modules, including
ratings, scheduling, safety, documents, support, chat, loyalty, pooling,
delivery, corporate rides, fraud and pricing. Deliberately **not** started as
separate processes in the one-service deployment:
Kong, Istio, NATS, Prometheus/Grafana, self-hosted Sentry, MinIO, and the
remaining 9 microservices (`notifications`, `realtime`, `admin`, `promos`,
`scheduler`, `analytics`, `fraud`, `ml-eta`, `negotiation`). None of the core
5 services require any of them to start — `NATS_ENABLED` defaults to `false`
and the code just logs a warning and disables async events when it's off.

Two existing application-level fixes make both the single-container and
separate-service deployment topologies work:

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

## Recommended: one Railway application service

You do **not** have to create five Railway application services for an MVP.
`deploy/railway/all-in-one/Dockerfile` builds the five core binaries and a small
gateway. They run in one container on private loopback ports, while the gateway
listens on Railway's public `PORT` and exposes these stable prefixes:

| Public prefix | Upstream service | Example |
|---------------|------------------|---------|
| `/auth` | Auth | `POST /auth/api/v1/auth/login` |
| `/mobile` | Mobile feature API | `GET /mobile/api/v1/ride-types/available` |
| `/rides` | Ride lifecycle | `POST /rides/api/v1/rides` |
| `/geo` | Driver location | `POST /geo/api/v1/geo/location` |
| `/payments` | Payments | `GET /payments/api/v1/wallet` |

The prefix is removed before the request reaches its service. This gives the
frontend one base URL without changing the existing APIs.

## 1. Create the Railway project

1. In Railway, **New Project → Deploy from GitHub repo** → select this repo.
   Railway reads `railway.json` and builds
   `deploy/railway/all-in-one/Dockerfile`. Do not set a Dockerfile path or a
   `SERVICE_NAME` build argument for this deployment.
2. **Add a PostGIS-enabled PostgreSQL service** from Railway's template
   marketplace. Do not use a plain PostgreSQL image: later geography migrations
   create `GEOMETRY` columns and spatial indexes.
3. **Add a Redis plugin** (`+ New → Database → Redis`).

Note Postgres's plugin variables (Settings → Variables on the Postgres
service): `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE` (default
database name is `railway`). Note Redis's `REDISHOST`, `REDISPORT`,
`REDISPASSWORD`.

## 2. Configure variables

Railway does not automatically expose one service's variables to another.
Open the **application service → Variables → Add Reference**, select the
Postgres service, and add its `DATABASE_URL`. Do not type the literal example
value as plain text: it must be a Railway variable reference. Migrations run
automatically at startup, before any API process is launched.

```dotenv
DATABASE_URL=${{Postgres.DATABASE_URL}}

REDIS_HOST=${{Redis.REDISHOST}}
REDIS_PORT=${{Redis.REDISPORT}}
REDIS_PASSWORD=${{Redis.REDISPASSWORD}}

ENVIRONMENT=production
JWT_SECRET=<one-long-random-secret>
CORS_ORIGINS=https://your-app.lovable.app,https://your-preview-domain
STRIPE_API_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
```

Only the Postgres `DATABASE_URL` reference is required for database setup; the
container derives `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, and `DB_NAME`
for the five Go processes. As an alternative, the startup script also accepts
individual `PG*` or `DB_*` variables and constructs the migration URL.

If the logs say `PostgreSQL is not linked to this application`, the reference
is missing from the **application service**, even if the Postgres service itself
shows a `DATABASE_URL` variable.

Older deployments may report `Dirty database version 5`. The previous version
of migration 5 also tried to install optional `postgis_topology` and
`pg_stat_statements` components. Those optional components have been removed;
core PostGIS remains required by the geography schema. Startup automatically
repairs **only** that known dirty version by resetting its migration marker to
version 4 and rerunning the corrected migration. Unknown dirty versions are
never forced automatically because doing so could hide a partially applied
schema change. If the retry says the `postgis` extension is unavailable, replace
the plain database with a PostGIS-enabled Railway template and update the
`DATABASE_URL` reference.

Do not set `PORT`; Railway supplies it. Generate a public domain under
**Settings → Networking**, then deploy.

## 3. Call it from Lovable

Set one frontend variable:

```dotenv
VITE_API_URL=https://your-backend.up.railway.app
```

Examples:

```javascript
const API = import.meta.env.VITE_API_URL;

await fetch(`${API}/auth/api/v1/auth/login`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password }),
});

await fetch(`${API}/rides/api/v1/rides`, {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`,
  },
  body: JSON.stringify(rideRequest),
});
```

Check the deployment with `curl https://your-backend.up.railway.app/healthz`.
It returns HTTP 200 only when all five processes are healthy.

## Why separate services may still be better later

One service is cheaper and simpler, and is now the recommended MVP setup.
Separate Railway services provide independent scaling, deploys, logs, crash
isolation and resource limits. In the all-in-one container, one deployment
contains five database pools and a restart affects every API. Move to the
separate layout when traffic or team size makes those trade-offs worthwhile.

## Alternative: five independently scalable Railway services

### Create the 5 app services

For each of `auth`, `mobile`, `rides`, `geo`, `payments`:

1. `+ New → GitHub Repo` → this repo again (Railway lets you deploy the same
   repo as multiple services).
2. Name the service (e.g. `auth`).
3. Settings → Build → **Dockerfile Path**: `deploy/railway/<name>/Dockerfile`
   (e.g. `deploy/railway/auth/Dockerfile`). Root/build context stays the
   repo root — don't change it.
   This explicit Dockerfile path is the recommended approach. If you instead
   use the root `Dockerfile`, configure the Docker build argument
   `SERVICE_NAME=<name>` in Railway's build settings; setting it only in the
   Variables tab is not sufficient. With no build argument, the root
   `Dockerfile` intentionally defaults to `auth`.
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

### Verify the separate deployment

```bash
curl https://auth-production-XXXX.up.railway.app/healthz
curl -X POST https://auth-production-XXXX.up.railway.app/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"Passw0rd!","phone_number":"+10000000000","first_name":"Test","last_name":"User","role":"rider"}'
```

Use the JWT from the login/register response as `Authorization: Bearer <token>`
against `mobile`/`rides`/`geo`/`payments` endpoints (exact routes are in
`docs/API.md`).

### Point Lovable.dev at the separate deployment

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

## Known gaps in these MVP deployments

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
- The all-in-one option uses the included lightweight prefix gateway, not Kong.
  The separate option exposes one Railway domain per service. Add Kong later if
  you need centralized edge authentication, advanced routing or gateway-level
  rate limiting.
