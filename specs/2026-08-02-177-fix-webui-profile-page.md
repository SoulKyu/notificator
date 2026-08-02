# Spec: stop fabricating `/profile` data and fix the broken Copy-ID button

- Issue: [SoulKyu/notificator#177](https://github.com/SoulKyu/notificator/issues/177)
- Date: 2026-08-02
- Status: planned

## Problem

`internal/webui/handlers/profile_handlers.go:16-63` (`ProfilePage`) invents
every date and counter it renders instead of reading real data:

```go
CreatedAt:     time.Now().AddDate(0, -3, -15),                    // :45  "Member Since": always 3.5 months ago
LastLogin:     &[]time.Time{time.Now().Add(-2 * time.Hour)}[0],   // :46  "Last Login": always 2h ago
EmailVerified: user.Email != "",                                  // :47  green "Verified" badge means "has an email string"
...
SessionInfo: pages.SessionInfo{
    CreatedAt: time.Now().Add(-30 * time.Minute),                 // :51  "Session Started": always 30m ago
    ExpiresAt: time.Now().Add(7 * 24 * time.Hour),                // :52  "Expires": always "in 7 days"
},
Stats: pages.UserStats{TotalAlerts: 156, ...},                    // :54-59 hardcoded demo numbers, currently unrendered
```

Separately, `internal/webui/templates/pages/Profile.templ:242` puts
`userId: '{ data.User.ID }'` inside the page's `<script>` block. templ does
not interpolate `{ }` expressions inside `<script>` elements (confirmed
against the working precedent below), so every browser receives the literal
JS source `userId: '{ data.User.ID }'`. Clicking the "ID" button copies that
literal string, and there is no visible feedback either way (`showToast` is
set at `Profile.templ:241,246,248,258,260` but no element in the template
reads it).

Real data exists for the *account* dates, and only for those:

- `proto/auth.proto:110-119` (`User` message) already carries `created_at`
  (field 4) and `last_login` (field 5).
- `internal/backend/models/models.go:13-27` (`models.User`) has real
  `CreatedAt` and `LastLogin *time.Time` columns.
- `internal/backend/services/services.go:217-246` (`ValidateSession`) builds
  its `authpb.User` from the real `user` row loaded by `GetUserBySession`
  (`internal/backend/database/gorm_db.go:297-306`, which selects the whole
  row) but only ever sets `Id/Username/Email/CreatedAt(/Timezone)` —
  `LastLogin` is silently dropped even though the proto and the model both
  carry it, and even though the same file already populates it correctly two
  functions later (`services.go:350-352`, `:395`).
- `internal/webui/client/backend_client.go:51-58` (`client.User`) drops
  `CreatedAt`/`LastLogin` entirely when unmarshalling `ValidateSession`'s
  response (`:298-302`), even though `resp.User.CreatedAt` is already on the
  wire and simply never read.

`ProfilePage` sources its `user` from `middleware.GetCurrentUserFromContext`
(`profile_handlers.go:17`), which is populated by `RedirectIfNotAuth`
(`internal/webui/middleware/auth.go:86-121`) calling
`backendClient.ValidateSession` (`internal/webui/router.go:381` routes
`/profile` through `protectedPages`, which uses `RedirectIfNotAuth`). So
fixing `ValidateSession`'s unmarshalling is enough to fix everything
`ProfilePage` renders about the user — no new RPC round trip needed on that
path.

### The "Verified" badge has no truth to read

There is no email-verification flow in this product. `models.User.EmailVerified`
(`models.go:26`) is written in exactly one place — OAuth signup
(`internal/backend/database/oauth_db.go:23,73,98`) — from
`OAuthUserInfo.EmailVerified`, which each provider parser fills as follows
(`internal/backend/services/oauth_service.go`):

| Parser | Line | Value | Real verification signal? |
|---|---|---|---|
| GitHub | `:229` | `githubUser.Email != ""` | **No** — the exact "has an email string" heuristic issue #177 calls a lie, just moved to signup time |
| Google | `:257` | `googleUser.EmailVerified` | Yes — the provider's real `email_verified` claim |
| Microsoft | `:288` | `true` | **No** — hardcoded for every Microsoft user |
| Generic OIDC | `:325-330` | `email_verified` claim if the provider sends a bool, else the zero value `false` | Only when the provider sends it |

Classic username/password accounts never write the column at all, so they
keep the GORM default `false` (`models.go:26`).

So a badge "backed by the real `EmailVerified` flag" would still be a lie for
GitHub and Microsoft users, and would only *disappear* for the classic
accounts that are the majority. Sourcing it honestly would mean fixing two
provider parsers (GitHub's `/user` response has no verified-email field at
all — it needs a second call to `/user/emails`; Microsoft Graph exposes none),
which is a different feature than "stop lying on `/profile`".

Issue #177 AC4 allows either backing the badge with a real flag or removing
it. **This spec removes it.** That keeps the page honest without inventing a
verification feature, and it drops the entire proto/service/client plumbing
that would otherwise exist only to carry `email_verified` to a badge that
cannot tell the truth. `EmailVerified` stays in the DB model untouched for a
future real verification flow; it just stops being rendered.

### Out of reach: session timing

The session "Started"/"Expires"/"Duration" numbers have no backend session
lookup to source from at all today (`GetUserBySession`,
`internal/backend/database/gorm_db.go:297-306`, never selects
`sessions.created_at`/`expires_at`, and there is no `GetSessionByID`
method). Building one would mean either adding a second DB round trip to
`ValidateSession` (called on *every* authenticated request, not just
`/profile`) or wiring a new RPC end-to-end for a single page. Both are
disproportionate to a "stop lying" issue. The session's real 7-day lifetime
is already fixed and known (`services.go:165,1777`,
`internal/webui/middleware/session.go:24`), and the codebase already has a
precedent for stashing a real, request-time timestamp straight into the
webui's own session cookie: `ImpersonationStartedAt`
(`middleware/session.go:17,82,121-129`, `int64` Unix timestamp, nil-safe
getter). This spec reuses that exact pattern for a `session_started_at` key
set at login, instead of adding backend plumbing.

### Dead code nearby

`internal/webui/handlers/profile_handlers.go:65-101` (`GetProfileData`) has
its own hardcoded stats (`acknowledged_alerts: 42`, `comments: 17`,
`color_preferences: 3`) but is **not wired to any route**
(`grep -rn "GetProfileData" internal/webui/` outside its own definition
returns nothing, and `internal/webui/router.go` has no route pointing at
it — the real `/api/v1/profile` endpoint is
`authProtected.GET("/profile", handlers.GetCurrentUser)`, a different,
non-fabricating handler at `internal/webui/handlers/handlers.go:252-266`).
`GetProfileData` is dead code and out of scope for this issue.

## Goals

1. "Member Since" and "Last Login" show the account's real `created_at` /
   `last_login`.
2. The "Verified" badge is **removed** (`Profile.templ:109-116`) — no flag in
   this codebase actually means "this email was verified" (see above), so
   issue #177 AC4's "or removed" option is taken.
3. "Session Started" / "Expires" / "Duration" show a real, request-derived
   session start time (or are hidden if one isn't available), never a
   `time.Now()`-relative fabrication.
4. The hardcoded `UserStats` block (`TotalAlerts: 156` etc.) is deleted —
   it is already unrendered dead weight, not wired to anything real.
5. The "ID" button copies the actual user ID and shows visible success
   feedback.

## Non-goals

- **No proto change at all.** With the badge gone, `created_at` (field 4) and
  `last_login` (field 5) already carry everything `/profile` needs, so
  `proto/auth.proto` and `internal/backend/proto/**` are untouched — no
  `make proto` run, no regenerated `.pb.go` in the diff.
- **No email-verification feature.** `models.User.EmailVerified` and the
  OAuth parsers that fill it (`oauth_service.go:229,257,288,325-330`) are
  left exactly as they are; they simply stop being rendered. Making that flag
  trustworthy (GitHub `/user/emails`, a classic-auth verification mail flow)
  is a separate issue.
- `GetProfileData` (`profile_handlers.go:65-101`) and its own hardcoded
  stats — unrouted dead code, not reachable from `/profile` or anywhere
  else. Left untouched.
- The `GetProfile` RPC/`BackendClient.GetProfile` wrapper
  (`services.go:250-276`, `backend_client.go:340-374`) — also unrouted/
  never called (`grep -rn "\.GetProfile(" internal/webui/` only turns up
  its own definition and an unrelated raw `authClient.GetProfile` health
  check at `backend_client.go:143`). `ProfilePage` never needs it since
  `ValidateSession` already carries everything required. Not extended.
- No new database session-lookup RPC (see rationale above) — the session
  card is powered by the webui's own login-time cookie value instead.
- No design changes to the OAuth-provider badge, Quick Actions, or
  "Change Password" (still a stub `alert(...)`, unrelated to this issue).

## Approach

### 1. Backend: populate `LastLogin` in `ValidateSession`

`internal/backend/services/services.go:233-246`, add the nil-check that the
same file already uses for `LastLogin` at `:350-352`:

```go
resp := &authpb.ValidateSessionResponse{
	Valid:   true,
	Message: "Session is valid",
	User: &authpb.User{
		Id:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		CreatedAt: timestamppb.New(user.CreatedAt),
	},
}
if user.LastLogin != nil {
	resp.User.LastLogin = timestamppb.New(*user.LastLogin)
}
if user.Timezone != nil {
	resp.User.Timezone = *user.Timezone
}
return resp, nil
```

That is the only backend change in this spec. `GetProfile` is intentionally
**not** touched (see Non-goals) — it's dead code and changing it doesn't
affect anything `/profile` renders.

### 2. WebUI client: stop dropping `CreatedAt`/`LastLogin`

`internal/webui/client/backend_client.go:51-58`, extend `User` (the file
already imports `time`, line 9):

```go
type User struct {
	ID            string     `json:"id"`
	Username      string     `json:"username"`
	Email         string     `json:"email"`
	OAuthProvider *string    `json:"oauth_provider,omitempty"`
	OAuthID       *string    `json:"oauth_id,omitempty"`
	Timezone      *string    `json:"timezone,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	LastLogin     *time.Time `json:"last_login,omitempty"`
}
```

In `ValidateSession` (`backend_client.go:277-315`), read the new fields off
`resp.User` at `:298-302` (the file already imports `timestamppb`, line 14;
`*Timestamp.AsTime()` is the same idiom as
`internal/webui/services/color_service.go:180-181`):

```go
user := &User{
	ID:        resp.User.Id,
	Username:  resp.User.Username,
	Email:     resp.User.Email,
	CreatedAt: resp.User.CreatedAt.AsTime(),
}
if resp.User.LastLogin != nil {
	t := resp.User.LastLogin.AsTime()
	user.LastLogin = &t
}
```

(keep the existing `OauthProvider`/`OauthId`/`Timezone` blocks at
`:304-312` unchanged).

`resp.User.CreatedAt` is always set by `ValidateSession` (§1 keeps the
unconditional `timestamppb.New(user.CreatedAt)`), so `.AsTime()` on this path
cannot yield a zero/1970 date.

### 3. Real session start time via the existing cookie-session pattern

`internal/webui/middleware/session.go`, mirror `ImpersonationStartedAt`
(`:17,82,121-129`) with a new key in the existing `const` block (`:14-18`)
and a getter next to `GetImpersonationStartedAt` (`:120-129`):

```go
const SessionStartedAt = "session_started_at"
```

```go
// GetSessionStartedAt returns when the current webui session was created,
// or nil for sessions established before this key existed.
func GetSessionStartedAt(c *gin.Context) *time.Time {
	if startedAt := GetSessionValue(c, SessionStartedAt); startedAt != nil {
		if ts, ok := startedAt.(int64); ok {
			t := time.Unix(ts, 0)
			return &t
		}
	}
	return nil
}
```

Set it at both places that currently write `session_id` (nothing else reads
or writes `session_id`; confirmed via
`grep -rn 'SetSessionValue(c, "session_id"' internal/webui/`). Each site
keeps its own error style — they are **not** the same:

- `internal/webui/handlers/handlers.go:126-130` (`Login`, JSON API), right
  after the existing `session_id` block:
  ```go
  if err := middleware.SetSessionValue(c, middleware.SessionStartedAt, time.Now().Unix()); err != nil {
      c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to create session"))
      return
  }
  ```
- `internal/webui/handlers/oauth_handlers.go:124-128` (`OAuthCallback`,
  browser redirect), right after its `session_id` block — this handler logs
  and redirects rather than returning JSON, and already imports `time`
  (line 9):
  ```go
  if err := middleware.SetSessionValue(c, middleware.SessionStartedAt, time.Now().Unix()); err != nil {
      log.Printf("Failed to set session start time: %v", err)
      c.Redirect(http.StatusFound, "/login?error=session_failed")
      return
  }
  ```

Sessions that predate this change simply won't have the key; `ProfilePage`
must treat that as "no session timing available" (see below), not crash.

### 4. `ProfilePage` handler: derive everything, fabricate nothing

`internal/webui/handlers/profile_handlers.go:16-63`, replace the whole
`profileData` construction:

```go
func ProfilePage(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	sessionID := middleware.GetSessionID(c)

	authMethod := middleware.GetSessionValue(c, "auth_method")
	var oauthProvider *string
	if authMethodStr, ok := authMethod.(string); ok && strings.HasPrefix(authMethodStr, "oauth:") {
		provider := strings.TrimPrefix(authMethodStr, "oauth:")
		if user.OAuthProvider == nil {
			oauthProvider = &provider
		} else {
			oauthProvider = user.OAuthProvider
		}
	} else {
		oauthProvider = user.OAuthProvider
	}

	sessionInfo := pages.SessionInfo{SessionID: sessionID}
	if startedAt := middleware.GetSessionStartedAt(c); startedAt != nil {
		expiresAt := startedAt.Add(7 * 24 * time.Hour) // ponytail: mirrors the 7-day session lifetime in services.go Login (:165,:1777) and the cookie MaxAge (middleware/session.go:24); extend together if that policy changes
		sessionInfo.CreatedAt = startedAt
		sessionInfo.ExpiresAt = &expiresAt
	}

	profileData := pages.ProfileData{
		User: pages.ProfileUser{
			ID:            user.ID,
			Username:      user.Username,
			Email:         user.Email,
			OAuthProvider: oauthProvider,
			OAuthID:       user.OAuthID,
			CreatedAt:     user.CreatedAt,
			LastLogin:     user.LastLogin,
		},
		SessionInfo: sessionInfo,
	}

	templ.Handler(pages.Profile(profileData)).ServeHTTP(c.Writer, c.Request)
}
```

No more `time.Now()` arithmetic, no `EmailVerified`, no `Stats:` block.
`time` is still imported (line 6) for the `7 * 24 * time.Hour` expiry.

### 5. `Profile.templ`: struct changes, badge removal, nil-safe session card, fixed Copy-ID

Edit `internal/webui/templates/pages/Profile.templ`, then run
`make webui-templates` (never hand-edit `Profile_templ.go`).

**Structs** (`:9-37`): drop `Stats`/`UserStats` entirely (confirmed
unrendered and referenced nowhere else via
`grep -rn "UserStats\|ProfileData{" internal/webui/`), drop
`ProfileUser.EmailVerified` (`:23`, its only reader is the badge removed
below — `grep -rn "EmailVerified" internal/webui/` returns exactly those two
sites), and make `SessionInfo`'s timestamps pointers:

```go
type ProfileData struct {
	User        ProfileUser
	SessionInfo SessionInfo
}

type ProfileUser struct {
	ID            string
	Username      string
	Email         string
	OAuthProvider *string
	OAuthID       *string
	CreatedAt     time.Time
	LastLogin     *time.Time
}

type SessionInfo struct {
	SessionID string
	CreatedAt *time.Time
	ExpiresAt *time.Time
}
```

**"Verified" badge** (`:109-116`): delete the whole
`if data.User.EmailVerified { ... }` block. The enclosing
`<div class="mt-3 flex items-center space-x-2">` (`:95-117`) stays and keeps
rendering the OAuth-provider / "Classic Auth" badge (`:96-107`), which is
sourced from real data and is unchanged.

**Session card** (`:170-193`): guard the started/expires/duration block on
`CreatedAt != nil`, drop it otherwise (Session ID is always real and always
shown):

```go
<div class="space-y-3">
	<div>
		<dt class="text-sm font-medium text-gray-500 dark:text-gray-400">Session ID</dt>
		<dd class="mt-1 text-xs text-gray-900 dark:text-white font-mono truncate">{ data.SessionInfo.SessionID }</dd>
	</div>
	if data.SessionInfo.CreatedAt != nil {
		<div>
			<dt class="text-sm font-medium text-gray-500 dark:text-gray-400">Session Started</dt>
			<dd class="mt-1 text-sm text-gray-900 dark:text-white">{ templates.FormatDate(*data.SessionInfo.CreatedAt) }</dd>
		</div>
		<div>
			<dt class="text-sm font-medium text-gray-500 dark:text-gray-400">Expires</dt>
			<dd class="mt-1 text-sm text-gray-900 dark:text-white">{ templates.FormatDate(*data.SessionInfo.ExpiresAt) }</dd>
		</div>
		<div class="pt-3 border-t border-gray-200 dark:border-gray-700">
			<div class="flex items-center justify-between">
				<span class="text-sm text-gray-500 dark:text-gray-400">Duration</span>
				<span class="text-sm font-medium text-gray-900 dark:text-white">
					{ templates.FormatDuration(time.Since(*data.SessionInfo.CreatedAt)) }
				</span>
			</div>
		</div>
	}
</div>
```

`CreatedAt` and `ExpiresAt` are set together in §4 or not at all, so the
`*data.SessionInfo.ExpiresAt` deref inside the guard is safe.
`templates.FormatDate`/`FormatDuration` are `internal/webui/templates/utils.go:20,24`
and keep taking values, not pointers — only the call sites deref.

**Copy-ID fix** (`:44,120-132,238-269`): templ *does* interpolate `{ }`
expressions inside a regular HTML attribute (unlike `<script>` text) — this
is already a shipped, working pattern in this codebase at
`StatisticsDashboard.templ:43`:
`x-data={ "{ ...statisticsDashboardPage(), ...statisticsViewsMixin(), currentUserId: '" + data.User.ID + "' }" }`,
and `filter_components.templ:7`. User IDs are 32-char lowercase
alphanumeric (`internal/backend/models/models.go:326-336`,
`generateRandomString(32)` over `"abcdefghijklmnopqrstuvwxyz0123456789"`,
assigned by `BeforeCreate` at `models.go:36-41` for OAuth signups too since
`oauth_db.go:17-24` never sets an ID), so naive single-quote wrapping
(matching the existing convention above, no escaping helper) is safe.
Change the root `<div>` (`:44`):

```
<div class="min-h-screen bg-gray-50 dark:bg-dark-bg-primary py-8" x-data={ "profilePage('" + data.User.ID + "')" }>
```

Give the "ID" button (`:122-131`) the same icon-swap feedback the alert
modal's Copy-link button already uses
(`internal/webui/templates/components/modal_components.templ:1120-1131`,
driven by `alertLinkCopied` set in
`internal/webui/templates/scripts/dashboard_modal.templ:151-156`) instead of
the currently-unbound `showToast`:

```html
<button 
	@click="copyUserId()" 
	class="inline-flex items-center px-3 py-2 border border-gray-300 dark:border-gray-600 shadow-sm text-sm leading-4 font-medium rounded-md text-gray-700 dark:text-gray-200 bg-white dark:bg-dark-bg-tertiary hover:bg-gray-50 dark:hover:bg-dark-bg-secondary focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 transition-colors"
	title="Copy User ID"
>
	<svg x-show="!idCopied" class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
		<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
	</svg>
	<svg x-show="idCopied" style="display: none;" class="h-4 w-4 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
		<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/>
	</svg>
	<span class="ml-2" x-text="idCopied ? 'Copied' : 'ID'"></span>
</button>
```

And the script (`:238-269`), take `userId` as a constructor argument instead
of interpolating it inside `<script>`, and flip `idCopied` instead of the
unbound `showToast`:

```js
function profilePage(userId) {
	return {
		idCopied: false,
		userId: userId,

		copyUserId() {
			navigator.clipboard.writeText(this.userId).then(() => {
				this.idCopied = true;
				setTimeout(() => { this.idCopied = false; }, 2000);
			}).catch(err => {
				console.error('Failed to copy:', err);
				const input = document.createElement('input');
				input.value = this.userId;
				document.body.appendChild(input);
				input.select();
				document.execCommand('copy');
				document.body.removeChild(input);
				this.idCopied = true;
				setTimeout(() => { this.idCopied = false; }, 2000);
			});
		},

		showChangePassword() {
			// TODO: Implement change password modal
			alert('Change password functionality coming soon!');
		}
	}
}
```

`handleLogoutResponse` (`:271-275`) is unrelated and stays as is.

### Files touched

- `internal/backend/services/services.go` — `ValidateSession` (+3 lines)
- `internal/webui/client/backend_client.go` — `User` struct, `ValidateSession`
- `internal/webui/middleware/session.go` — new const + getter
- `internal/webui/handlers/handlers.go` — `Login`
- `internal/webui/handlers/oauth_handlers.go` — `OAuthCallback`
- `internal/webui/handlers/profile_handlers.go` — `ProfilePage`
- `internal/webui/templates/pages/Profile.templ` (+ regenerated
  `Profile_templ.go`)

No `proto/`, no `internal/backend/proto/`, no `internal/backend/models/`,
no `internal/backend/services/oauth_service.go`.

## Risks & trade-offs

- **The "Verified" badge disappears for everyone**, including the Google and
  generic-OIDC users for whom the flag *was* real. That is the deliberate
  trade of removing a badge that was wrong for GitHub (`email != ""`),
  wrong for Microsoft (hardcoded `true`), and absent for classic accounts —
  a badge that is right for one provider out of four is worse than no badge.
  The DB column survives, so a later real verification flow can bring it
  back honestly.
- **Sessions created before this ships** have no `session_started_at`
  cookie key. `ProfilePage` degrades correctly (hides the started/expires/
  duration block, per the goal of never inventing data) rather than
  crashing, but those users won't see session timing until they log out
  and back in. Self-healing, not worth special-casing further.
- **The 7-day expiry is a mirrored constant**, not a value read back from
  the `sessions` row — three copies now agree (`services.go:165,1777`,
  `middleware/session.go:24`, `profile_handlers.go`). It is truthful today
  (verified against `sessions.expires_at`), and the `ponytail:` comment in §4
  names the upgrade path; making it authoritative means the session-lookup
  RPC this spec explicitly declines to build. Issue AC3's "no hardcoded
  constants in `profile_handlers.go`" is met in spirit — nothing is
  *fabricated* — but not in letter.
- **`GetEffectiveUser`** (`internal/webui/middleware/auth.go:176-190`, used
  for impersonation-aware data loading) constructs a bare
  `&client.User{ID, Username}` without the new fields when impersonating.
  This is irrelevant to `/profile`, which always calls
  `GetCurrentUserFromContext` (the real, authenticated user), never
  `GetEffectiveUser` — an admin impersonating someone still sees their own
  real profile, not the impersonated target's, which is correct and
  unchanged behavior.

## Validation

1. `make webui-templates` — regenerate `Profile_templ.go` from the edited
   `.templ` (never hand-edit the generated file).
2. `go build ./...` and `go test ./...` — confirms the backend/webui changes
   compile together and the existing
   `internal/backend/services/update_timezone_test.go:70-78` `ValidateSession`
   assertion (which only checks `Timezone`) still passes unaffected.
3. `git diff --stat` — must show **no** files under `proto/` or
   `internal/backend/proto/`; if it does, something reintroduced the
   `email_verified` plumbing this spec removed.
4. `make test` (docker-compose full rebuild) and manually, in a browser:
   - Register a fresh classic user, log in, open `/profile`: "Member
     Since" and "Last Login" show real, current timestamps (not ~3.5
     months / ~2h old), matching `users.created_at` / `users.last_login`
     in Postgres.
   - "Session Started"/"Expires"/"Duration" show real values matching the
     actual login time and a 7-day expiry (cross-check against
     `sessions.created_at`/`expires_at`).
   - Open `/profile` with a session created *before* the change (or delete
     the cookie key): the Session ID row still renders, the
     Started/Expires/Duration rows are absent, nothing panics.
   - No "Verified" badge on any account, OAuth or classic; the
     provider/"Classic Auth" badge still renders.
   - Click the "ID" button, inspect the clipboard: it contains the actual
     user ID (matches the "User ID" field shown on the page), and the
     button shows the checkmark/"Copied" feedback for ~2s.
   - View page source on `/profile`: the `<script>` block no longer
     contains the literal text `{ data.User.ID }`.

## Verification ledger

All references below re-derived on this branch at the current HEAD.

- Fabrication lines (`profile_handlers.go:45,46,47,51,52,54-59`) and the
  Copy-ID `<script>` bug (`Profile.templ:242`), plus the unbound `showToast`
  writes (`:241,246,248,258,260`): read both files directly.
- **Badge premise, re-derived after the previous QA fail**: `grep -rn
  "EmailVerified" internal/backend/ --glob '!*.pb.go'` → 11 hits; read
  `oauth_service.go:210-340` in full. GitHub `:229` is
  `githubUser.Email != ""` (a heuristic, not a provider flag); Microsoft
  `:288` is a literal `true`; only Google `:257` reads a real
  `email_verified` claim, and generic OIDC `:325-330` reads one when the
  provider sends a bool. Write sites are `oauth_db.go:23,73,98` only —
  classic signup never writes the column, so it keeps the
  `gorm:"default:false"` at `models.go:26`. Its only webui reader is
  `Profile.templ:23,109` (`grep -rn "EmailVerified" internal/webui/`), so
  deleting the badge deletes the last consumer.
- `models.User.CreatedAt/LastLogin` real fields: `models.go:13-27`.
- proto `User.created_at` (4) / `last_login` (5) already present, no new
  field needed: read `proto/auth.proto:110-119`.
- `ValidateSession` currently drops `LastLogin`, and the same file already
  has the nil-check idiom: read `internal/backend/services/services.go:217-246`
  in full plus `:350-352`, `:395`.
- `client.User` drops `CreatedAt`/`LastLogin`; `time` (`:9`) and
  `timestamppb` (`:14`) already imported: read
  `internal/webui/client/backend_client.go:1-20,51-58,277-315` in full.
- `/profile` is served through `RedirectIfNotAuth` → `ValidateSession`,
  confirming that path alone is sufficient: read
  `internal/webui/router.go:375-382` and
  `internal/webui/middleware/auth.go:86-190` in full.
- `GetProfile` (service + wrapper) is unrouted/uncalled dead code:
  `grep -rn "\.GetProfile(" internal/webui/` (only its own definition and
  the unrelated raw health-check call at `backend_client.go:143`);
  `grep -rn "GetProfileData" internal/webui/` (only its own definition, no
  route). Real `/api/v1/profile` route: `router.go:206` →
  `handlers.go:252-266`.
- No existing DB session-by-id lookup exists (forces the cookie-based
  approach): `grep -n "func (gdb \*GormDB)" internal/backend/database/gorm_db.go`
  and read `GetUserBySession` at `:297-306`.
- Real 7-day session lifetime: `grep -n "expiresAt" internal/backend/services/services.go`
  (`:165,1777`) and `internal/webui/middleware/session.go:24` (cookie
  `MaxAge: 86400 * 7`).
- Cookie-timestamp precedent (`ImpersonationStartedAt`) and the const block
  it lives in: read `internal/webui/middleware/session.go:12-18,32-35,75-129`
  in full; `SetSessionValue` returns `error` (`:32-36`).
- Exactly two `session_id` write sites, with **different** error styles
  (JSON vs redirect — the reason §3 spells both out):
  `grep -rn 'SetSessionValue(c, "session_id"' internal/webui/` →
  `handlers.go:126-130`, `oauth_handlers.go:124-128`; read both handlers in
  full. `oauth_handlers.go` already imports `time` (line 9) and `log`.
- `UserStats`/`Stats` fully unrendered and safe to delete:
  `grep -rn "UserStats\|ProfileData{" internal/webui/`.
- Session card markup and its non-pointer `FormatDate`/`FormatDuration`
  calls: read `Profile.templ:162-196`; helpers at
  `internal/webui/templates/utils.go:20,24`.
- No test exercises `ProfilePage`/`ProfileData`/`Profile.templ`; the one
  `ValidateSession` test only asserts `Timezone`:
  `grep -rln "ValidateSession" internal/backend/services/*_test.go
  internal/webui/client/*_test.go internal/webui/middleware/*_test.go` and
  read `update_timezone_test.go:70-78`.
- Attribute-interpolation precedent: read `StatisticsDashboard.templ:43` and
  `filter_components.templ:7`.
- Copy-link icon-swap feedback precedent: read
  `internal/webui/templates/components/modal_components.templ:1120-1131` and
  `internal/webui/templates/scripts/dashboard_modal.templ:151-156`.
- User IDs are 32-char lowercase alphanumeric, including for OAuth signups
  (safe for naive single-quote embedding): read `models.go:36-41` (`BeforeCreate`),
  `models.go:326-336` (`GenerateID`), `oauth_db.go:17-24` (never sets an ID).
- `.AsTime()` convention: `internal/webui/services/color_service.go:180-181`.
