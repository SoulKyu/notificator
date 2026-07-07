# Configuration

Configuration is loaded by Viper (`config/config.go`, `LoadConfigWithViper`), wired up in
`cmd/root.go:initConfig`. Precedence, highest to lowest:

1. **Command-line flags** (e.g. `--db-type`, `--grpc-listen`)
2. **Environment variables** (`NOTIFICATOR_*` prefix, plus a few legacy names)
3. **`config.json`** (searched in `$HOME/.config/notificator`, `./config`, `.`)
4. **Go struct defaults** (`DefaultConfig()`)

`ENVIRONMENT_VARIABLES.md` in the repo root is the full env-var reference; this page is the map.

## Env-var scheme

`NOTIFICATOR_` + the JSON config path in upper snake case (dots → underscores):
`backend.grpc_listen` → `NOTIFICATOR_BACKEND_GRPC_LISTEN`. Viper's `AutomaticEnv` binds most
scalar fields automatically. A few legacy/plain names are also honored:
`DATABASE_URL`, `DB_HOST`/`DATABASE_HOST`, `BACKEND_ADDRESS`, and the whole `OAUTH_*` family.

## Config sections (`config.Config`, `config/config.go:15`)

| Section | Purpose |
|---------|---------|
| `alertmanagers[]` | Alertmanager endpoints (name, url, auth, headers, oauth) — see below |
| `backend` | `grpc_listen`, `grpc_client`, `http_listen`, `database{…}` |
| `backend.database` | `type` (`sqlite`/`postgres`), host/port/name/user/password/ssl_mode, `sqlite_path` |
| `webui` | `playground` toggle (dev landing page) |
| `oauth` | OAuth portal config (nilable) — see [OAuth](#oauth) |
| `sentry` | Sentry enrichment (nilable) — see [Sentry](#sentry) |
| `admin` | `impersonation_allowed_users[]` — who may impersonate |
| `resolved_alerts`, `statistics` | TTL / retention knobs (see [backend](backend.md#database)) |
| `polling` | Alertmanager poll interval / sync interval |
| `gui`, `notifications`, `column_widths` | ⚠️ **desktop-only, dead** — see [architecture](architecture.md#build-variants) |

## Multi-Alertmanager & multi-tenant (Mimir/Cortex)

Alertmanagers are **not** bound generically by Viper — they're read in a manual loop over
`alertmanagers.0` … `alertmanagers.9` (`config.go:267`). Each entry becomes a `Client` in the
`MultiClient` (`internal/alertmanager/client.go`), and results are tagged with the source name.
`FetchAllAlerts` tolerates partial failures — it only errors if *every* Alertmanager fails.

**Multi-tenancy is just custom HTTP headers**, injected by a `customHeaderRoundTripper` — there
is no Mimir-specific code path. Two ways to set them:

- **Per instance:** `NOTIFICATOR_ALERTMANAGERS_<N>_HEADERS="X-Scope-OrgID=prod-tenant"`
  (comma-separated `Key=Val` pairs).
- **Global:** `METRICS_PROVIDER_HEADERS="X-Scope-OrgID=your-tenant"`, merged into every
  Alertmanager that doesn't already set the header (via `cfg.MergeHeaders()`).

> ⚠️ `MergeHeaders()` is called in `cmd/backend.go` **and** `cmd/webui.go` startup paths — but
> note it only fills headers not already set per-instance.

Per-instance auth also supports basic auth (`username`/`password`), bearer `token`, and a
proxy-auth mode (`oauth.proxy_mode`) for Alertmanagers behind an oauth2-proxy.

## OAuth {#oauth}

`OAuthPortalConfig` (`config/oauth_config.go`): `enabled`, `disable_classic_auth`,
`redirect_url`, `session_key`, `providers{}`, `group_sync{}`, `security{}`.
`loadOAuthProvidersFromEnv` auto-configures **github / google / microsoft / okta** from
`OAUTH_<PROVIDER>_CLIENT_ID` / `_SECRET` (+ optional scopes/URLs), with sensible default
endpoints and group mappings baked in. `Validate()` refuses to start if `session_key` is still
the insecure default or if no enabled provider has credentials.

Group sync (`OAUTH_GROUP_SYNC_*`) maps OAuth groups → roles with a cache (default 1h TTL) and a
`default_role` (e.g. `viewer`). See `docs/oauth/` for provider setup walkthroughs and
`docs/oauth/examples/config-examples.json`.

> **Reminder:** roles are computed and stored, but **no RPC actually enforces them** today
> (see [backend](backend.md#auth)).

## Sentry {#sentry}

`SentryConfig{Enabled, BaseURL, GlobalToken}` (`NOTIFICATOR_SENTRY_*`). Per-user Sentry personal
tokens are stored **AES-256-GCM encrypted** (`internal/backend/database/sentry_db.go`); at query
time the WebUI resolves the user's own token → the admin `GlobalToken` fallback → none, and
enriches alerts from Sentry issue URLs found in annotations/labels.

> ⚠️ **Security gotcha:** the encryption key comes from `NOTIFICATOR_ENCRYPTION_KEY`, but falls
> back to a **hardcoded dev key** if unset — and this var is **not** documented in
> `ENVIRONMENT_VARIABLES.md` or `.env.example`. Set it in any real deployment that stores Sentry
> tokens, or those tokens are encrypted with a publicly-known key.

## Session secret

`NOTIFICATOR_SESSION_SECRET` (documented in `.env.example`, generate with `openssl rand -hex 32`)
signs the WebUI session cookie. If unset, the WebUI uses a **random per-process secret**, so
sessions don't survive a restart (see [webui](webui.md#auth)).
