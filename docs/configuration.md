# Configuration

All Findus settings are read from **environment variables** at startup. You can supply them directly, via a `.env` file beside `docker-compose.yml`, or through your container orchestrator's secret management.

Copy [`.env.example`](../.env.example) as a starting point.

---

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `FINDUS_PORT` | `8080` | TCP port the HTTP server listens on |
| `FINDUS_DATA_DIR` | `./data` | Root directory for the SQLite database and `images/` folder. Set to `/data` inside the official container. |
| `FINDUS_BASE_URL` | `http://localhost:8080` | **Canonical public URL** — no trailing slash. Embedded verbatim in every QR code as `{BASE_URL}/q/{token}`. Must match what users type in the browser. |
| `FINDUS_JWT_SECRET` | *(empty)* | HS256 signing secret for session JWTs. If empty, Findus auto-generates a secret and persists it as `$DATA_DIR/.jwt_secret`. Rotating this value invalidates all active sessions. |
| `FINDUS_COOKIE_SECURE` | `true` if `FINDUS_BASE_URL` is `https://…`, else `false` | Marks cookies as `Secure`, gives them the `__Host-` prefix and sends HSTS. Breaks login on plain HTTP. |
| `FINDUS_TRUST_PROXY` | `false` | Take the client IP for rate limits from the **last** `X-Forwarded-For` entry. Set `true` only behind a reverse proxy that sets this header and when Findus is not reachable directly. With two proxies in a row (e.g. CDN → nginx) the inner one has to pass on the real client IP. |
| `FINDUS_SETUP_TOKEN` | *(random)* | Required to create the first (admin) account. If empty, a one-time setup link is written to the log at startup. |
| `FINDUS_LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, or `error` |
| `FINDUS_LOG_FORMAT` | `text` | `text` — human-readable `key=value` lines. `json` — structured JSON (recommended for log aggregators and Docker logging drivers). |

---

## HTTPS and reverse proxies

Findus does not handle TLS itself. Terminate HTTPS at a reverse proxy and forward plain HTTP:

**Step 1** — Configure your proxy (Caddy, nginx, Traefik…) to forward to `http://localhost:8080` (or the container's hostname/port).

**Step 2** — Set `FINDUS_BASE_URL` to the **https** URL:

```env
FINDUS_BASE_URL=https://findus.example.com
```

**Step 3** — Secure cookies and HSTS switch on automatically with an `https://` base URL. Do not set `FINDUS_COOKIE_SECURE=false`.

**Step 4** — Let the proxy set `X-Forwarded-For` (Caddy does by default; nginx: `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`) and set:

```env
FINDUS_TRUST_PROXY=true
```

Without it, all visitors share the proxy's IP and therefore one rate limit. Keep the Findus port unreachable from outside: the Compose files bind it to `127.0.0.1`.

---

## Public hosting

- Keep the registration mode on **admin only** or **invite**. Every account can read the whole inventory including attachments, so **open** registration publishes it to anyone.
- Failed logins are limited to 10 per client and 50 per username in 15 minutes, registrations to 10 per client and hour.
- Logging out, changing the password, deactivating a user or changing a role ends all of that user's sessions. A password change or deactivation also revokes the MCP token.

> **Why `FINDUS_BASE_URL` matters:** every QR code encodes the URL `{FINDUS_BASE_URL}/q/{token}`. If this is wrong, scanning a code opens the wrong host or scheme. This is the most common misconfiguration for LAN or HTTPS deployments.

### Subpath deployment

If you mount Findus at a subpath (e.g. `https://example.com/findus/`), set `FINDUS_BASE_URL` to include that path and ensure your reverse proxy strips or preserves it consistently. The SPA router and QR resolver both rely on this prefix.

---

## JWT secret handling

| Scenario | Behavior |
|---|---|
| `FINDUS_JWT_SECRET` is set | Used directly as the HS256 signing key |
| `FINDUS_JWT_SECRET` is empty | On first startup, a random 32-byte secret is generated and written to `$DATA_DIR/.jwt_secret`. Subsequent startups load it from that file. |

**Backup note:** if you rely on the auto-generated secret, include `$DATA_DIR/.jwt_secret` in your backup process. Restoring the database without restoring the secret file will invalidate all existing sessions (users can simply log in again — no data is lost).

---

## Docker Compose variants

### Production (`docker-compose.yml`)

Pulls `ghcr.io/bueckerlars/findus:latest` from GitHub Container Registry.

Includes a one-shot `findus-data-init` service that `chown`s the volume to `65532:65532` (the nonroot user in the distroless image) before the app container starts.

### Development (`docker-compose.yml.dev`)

Builds the image from `Dockerfile` in this checkout. Useful for testing changes before tagging a release:

```bash
docker compose -f docker-compose.yml.dev up --build
```

Both Compose files mount a named Docker volume at `/data` and use the same environment variable names.

---

## Minimal production `.env`

```env
# Public URL — must match what users type in the browser
FINDUS_BASE_URL=https://findus.example.com

# Behind a reverse proxy that sets X-Forwarded-For
FINDUS_TRUST_PROXY=true

# Recommended: set an explicit secret so it survives container recreations
# generate with: openssl rand -hex 32
FINDUS_JWT_SECRET=

# Structured logs for Docker log driver / aggregator
FINDUS_LOG_FORMAT=json
```
