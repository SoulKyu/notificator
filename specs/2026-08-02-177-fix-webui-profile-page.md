# Spec: stop fabricating `/profile` data and fix the broken Copy-ID button

- Issue: [SoulKyu/notificator#177](https://github.com/SoulKyu/notificator/issues/177)
- Date: 2026-08-02
- Status: planned

## Problem

`internal/webui/handlers/profile_handlers.go:39-60` (`ProfilePage`) invents
every date and counter it renders instead of reading real data:

```go
CreatedAt:     time.Now().AddDate(0, -3, -15),          // "Member Since": always 3.5 months ago
LastLogin:     &[]time.Time{time.Now().Add(-2 * time.Hour)}[0], // "Last Login": always 2h ago
EmailVerified: user.Email != "",                          // green "Verified" badge means "has an email string"
...
SessionInfo: pages.SessionInfo{
    CreatedAt: time.Now().Add(-30 * time.Minute),          // "Session Started": always 30m ago
    ExpiresAt: time.Now().Add(7 * 24 * time.Hour),          // "Expires": always "in 7 days"
},
Stats: pages.UserStats{TotalAlerts: 156, ...},              // hardcoded demo numbers, currently unrendered
```

Separately, `internal/webui/templates/pages/Profile.templ:242` puts
`userId: '{ data.User.ID }'` inside the page's `<script>` block. templ does
not interpolate `{ }` expressions inside `<script>` elements (confirmed
against the working precedent below), so every browser receives the literal
JS source `userId: '{ data.User.ID }'`. Clicking the "ID" button copies that
literal string, and there is no visible feedback either way (`showToast` is
set but no element in the template reads it).

The real data already exists:
- `proto/auth.proto:110-119` (`User` message) has `created_at` (field 4) and
  `last_login` (field 5) already.
- `internal/backend/models/models.go:13-27` (`models.User`) has real
  `CreatedAt`, `LastLogin *time.Time`, and `EmailVerified bool` columns.
  `EmailVerified` is genuinely meaningful for OAuth users — GitHub/Google/
  Microsoft logins set it from the provider's own verified-email flag
  (`internal/backend/services/oauth_service.go:229,257,288`, wired through
  `internal/backend/database/oauth_db.go:16-24`) — it is only always-`false`
  for classic username/password accounts, which have no verification flow.
- `internal/backend/services/services.go:217-247` (`ValidateSession`) and
  `:250-276` (`GetProfile`) both already build an `authpb.User` from the real
  `user` row but only ever set `Id/Username/Email/CreatedAt(/Timezone)` —
  `LastLogin` and email-verified state are silently dropped even though the
  proto and the model both carry them.
- `internal/webui/client/backend_client.go:51-58` (`client.User`) drops
  `CreatedAt`/`LastLogin` entirely when unmarshalling `ValidateSession`'s
  response (`:298-314`), even though `resp.User.CreatedAt` is already
  present on the wire and simply never read.

`ProfilePage` sources its `user` from `middleware.GetCurrentUserFromContext`
(`profile_handlers.go:17`), which is populated by `RedirectIfNotAuth`
(`internal/webui/middleware/auth.go:86-121`) calling
`backendClient.ValidateSession` (`internal/webui/router.go:381` routes
`/profile` through `protectedPages`, which uses `RedirectIfNotAuth`). So
fixing `ValidateSession`'s unmarshalling is enough to fix everything
`ProfilePage` renders about the user — no new RPC round trip needed on that
path.

The session "Started"/"Expires"/"Duration" numbers have no backend session
lookup to source from at all today (`GetUserBySession`,
`internal/backend/database/gorm_db.go:297-306`, never selects
`sessions.created_at`/`expires_at`, and there is no `GetSessionByID`
method). Building one would mean either adding a second DB round trip to
`ValidateSession` (called on *every* authenticated request, not just
`/profile`) or wiring a new RPC end-to-end for a single page. Both are
disproportionate to a "stop lying" issue. The session's real 7-day lifetime
is already fixed and known (`services.go:165,1777`,
`internal/webui/middleware/session.go:25`), and the codebase already has a
precedent for stashing a real, request-time timestamp straight into the
webui's own session cookie: `ImpersonationStartedAt`
(`middleware/session.go:82,121-129`, `int64` Unix timestamp, nil-safe
getter). This spec reuses that exact pattern for a `session_started_at` key
set at login, instead of adding backend plumbing.

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
2. The "Verified" badge reflects the real `EmailVerified` flag from the
   backend, not `user.Email != ""`.
3. "Session Started" / "Expires" / "Duration" show a real, request-derived
   session start time (or are hidden if one isn't available), never a
   `time.Now()`-relative fabrication.
4. The hardcoded `UserStats` block (`TotalAlerts: 156` etc.) is deleted —
   it is already unrendered dead weight, not wired to anything real.
5. The "ID" button copies the actual user ID and shows visible success
   feedback.

## Non-goals

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
- No new `google.protobuf` message shape changes beyond the one new `User`
  field.

## Approach

### 1. Proto: add `email_verified` to `User`

`proto/auth.proto:110-119`, inside `message User { ... }`, after
`string timezone = 8;`:

```proto
  bool email_verified = 9;
```

Run `make proto` (`scripts/generate_proto.sh` — requires `protoc`,
`protoc-gen-go`, `protoc-gen-go-grpc` on `PATH`) to regenerate
`internal/backend/proto/auth/auth.pb.go` and `auth_grpc.pb.go`. The script
wipes and regenerates `internal/backend/proto/{auth,alert}` wholesale, but
`alert.proto` is unchanged so only the new `EmailVerified` field (and
associated getters/marshal code) should show up in `git diff`.

### 2. Backend: populate `LastLogin` and `EmailVerified` in `ValidateSession`

`internal/backend/services/services.go:233-246`, extend the response the
same way `LastLogin` is already populated elsewhere in this file (e.g. the
nil-check pattern at `services.go:350-351`):

```go
resp := &authpb.ValidateSessionResponse{
	Valid:   true,
	Message: "Session is valid",
	User: &authpb.User{
		Id:            user.ID,
		Username:      user.Username,
		Email:         user.Email,
		CreatedAt:     timestamppb.New(user.CreatedAt),
		EmailVerified: user.EmailVerified,
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

`GetProfile` is intentionally **not** touched (see Non-goals) — it's dead
code and changing it doesn't affect anything `/profile` renders.

### 3. WebUI client: stop dropping `CreatedAt`/`LastLogin`/`EmailVerified`

`internal/webui/client/backend_client.go:51-58`, extend `User`:

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
	EmailVerified bool       `json:"email_verified"`
}
```

`ValidateSession` (`backend_client.go:298-302`), read the new fields off
`resp.User` (already imports `timestamppb`, whose `*Timestamp` has
`.AsTime()` — same idiom as `internal/webui/services/color_service.go:180-181`):

```go
user := &User{
	ID:            resp.User.Id,
	Username:      resp.User.Username,
	Email:         resp.User.Email,
	CreatedAt:     resp.User.CreatedAt.AsTime(),
	EmailVerified: resp.User.EmailVerified,
}
if resp.User.LastLogin != nil {
	t := resp.User.LastLogin.AsTime()
	user.LastLogin = &t
}
```

(keep the existing `OauthProvider`/`OauthId`/`Timezone` blocks that follow
unchanged).

### 4. Real session start time via the existing cookie-session pattern

`internal/webui/middleware/session.go`, mirror `ImpersonationStartedAt`
(`:18,82,121-129`) with a new key and getter:

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

Set it at both places that currently set `session_id` (nothing else reads
or writes `session_id`; confirmed via `grep -n 'SetSessionValue(c, "session_id"'
internal/webui/`):

- `internal/webui/handlers/handlers.go:126-130` (`Login`), right after the
  existing `SetSessionValue(c, "session_id", result.SessionID)` block:
  ```go
  if err := middleware.SetSessionValue(c, middleware.SessionStartedAt, time.Now().Unix()); err != nil {
      c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to create session"))
      return
  }
  ```
- `internal/webui/handlers/oauth_handlers.go:124-128` (`OAuthCallback`),
  same insertion, right after its `session_id` block (this handler already
  imports `time` — line 9).

Sessions that predate this change simply won't have the key; `ProfilePage`
must treat that as "no session timing available" (see below), not crash.

### 5. `ProfilePage` handler: derive everything, fabricate nothing

`internal/webui/handlers/profile_handlers.go:16-62`, replace the whole
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
		expiresAt := startedAt.Add(7 * 24 * time.Hour) // ponytail: mirrors the 7-day session lifetime in services.go Login (:165,:1777); extend together if that policy changes
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
			EmailVerified: user.EmailVerified,
		},
		SessionInfo: sessionInfo,
	}

	templ.Handler(pages.Profile(profileData)).ServeHTTP(c.Writer, c.Request)
}
```

No more `time.Now()` arithmetic, no `Stats:` block.

### 6. `Profile.templ`: struct changes, nil-safe session card, fixed Copy-ID

Edit `internal/webui/templates/pages/Profile.templ`, then run
`make webui-templates` (never hand-edit `Profile_templ.go`).

**Structs** (`:9-37`): drop `Stats`/`UserStats` entirely (confirmed
unrendered and referenced nowhere else via
`grep -rn "UserStats\|ProfileData{" internal/webui/`), make
`SessionInfo`'s timestamps pointers:

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
	EmailVerified bool
}

type SessionInfo struct {
	SessionID string
	CreatedAt *time.Time
	ExpiresAt *time.Time
}
```

**Session card** (`:162-196`): guard the started/expires/duration block on
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

**Copy-ID fix** (`:44,120-132,238-271`): templ *does* interpolate `{ }`
expressions inside a regular HTML attribute (unlike `<script>` text) — this
is already a shipped, working pattern in this codebase at
`StatisticsDashboard.templ:43`:
`x-data={ "{ ...statisticsDashboardPage(), ...statisticsViewsMixin(), currentUserId: '" + data.User.ID + "' }" }`,
and `filter_components.templ:7`. User IDs are 32-char lowercase
alphanumeric (`internal/backend/models/models.go:326-336`,
`generateRandomString(32)` over `"abcdefghijklmnopqrstuvwxyz0123456789"`),
so naive single-quote wrapping (matching the existing convention above,
no escaping helper) is safe. Change the root `<div>`:

```
<div class="min-h-screen bg-gray-50 dark:bg-dark-bg-primary py-8" x-data={ "profilePage('" + data.User.ID + "')" }>
```

Give the "ID" button the same icon-swap feedback the alert modal's
Copy-link button already uses (`internal/webui/templates/components/modal_components.templ:1121-1130`,
driven by `alertLinkCopied` set in `dashboard_modal.templ:151-156`) instead
of the currently-unbound `showToast`:

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

And the script (`:238-271`), take `userId` as a constructor argument
instead of interpolating it inside `<script>`, and flip `idCopied` instead
of the unbound `showToast`:

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

function handleLogoutResponse(event) {
	if (event.detail.successful) {
		window.location.href = '/';
	}
}
```

### Files touched

- `proto/auth.proto` (+1 field)
- `internal/backend/proto/auth/*.pb.go`, `*_grpc.pb.go` — regenerated, not
  hand-edited
- `internal/backend/services/services.go` — `ValidateSession`
- `internal/webui/client/backend_client.go` — `User` struct, `ValidateSession`
- `internal/webui/middleware/session.go` — new const + getter
- `internal/webui/handlers/handlers.go` — `Login`
- `internal/webui/handlers/oauth_handlers.go` — `OAuthCallback`
- `internal/webui/handlers/profile_handlers.go` — `ProfilePage`
- `internal/webui/templates/pages/Profile.templ` (+ regenerated
  `Profile_templ.go`)

## Risks & trade-offs

- **Sessions created before this ships** have no `session_started_at`
  cookie key. `ProfilePage` degrades correctly (hides the started/expires/
  duration block, per the goal of never inventing data) rather than
  crashing, but those users won't see session timing until they log out
  and back in. Self-healing, not worth special-casing further.
- **`make proto` regenerates both `auth` and `alert` proto packages**
  wholesale (`scripts/generate_proto.sh` does `rm -rf` then regenerate).
  `alert.proto` is untouched, so its generated output should be byte-for-
  byte identical; verify with `git diff --stat internal/backend/proto/`
  showing no unexpected files before committing.
- **`GetEffectiveUser`** (`internal/webui/middleware/auth.go:174-190`, used
  for impersonation-aware data loading) constructs a bare
  `&client.User{ID, Username}` without the new fields when impersonating.
  This is irrelevant to `/profile`, which always calls
  `GetCurrentUserFromContext` (the real, authenticated user), never
  `GetEffectiveUser` — an admin impersonating someone still sees their own
  real profile, not the impersonated target's, which is correct and
  unchanged behavior.
- **`EmailVerified` is real but not `= true` for classic accounts** —
  registering a fresh username/password user will show "not verified"
  (badge hidden), which is accurate (no verification flow exists) rather
  than the old always-"has an email" heuristic. This is a visible behavior
  change for the majority of accounts (classic auth), by design.

## Validation

1. `make proto` — regenerate, then `git diff --stat internal/backend/proto/`
   to confirm only the new `EmailVerified` field changed.
2. `make webui-templates` — regenerate `Profile_templ.go` from the edited
   `.templ` (never hand-edit the generated file).
3. `go build ./...` and `go test ./...` — confirms the proto/backend/webui
   changes compile together and the existing
   `internal/backend/services/update_timezone_test.go` `ValidateSession`
   assertion (which only checks `Timezone`) still passes unaffected.
4. `make test` (docker-compose full rebuild) and manually, in a browser:
   - Register a fresh classic user, log in, open `/profile`: "Member
     Since" and "Last Login" show real, current timestamps (not ~3.5
     months / ~2h old); "Verified" badge is absent (classic accounts have
     no verification flow).
   - Log in via an OAuth provider whose email is provider-verified: badge
     shows "Verified".
   - "Session Started"/"Expires"/"Duration" show real values matching the
     actual login time and a 7-day expiry.
   - Click the "ID" button, inspect the clipboard: it contains the actual
     user ID (matches the "User ID" field shown on the page), and the
     button shows the checkmark/"Copied" feedback for ~2s.
   - Open browser dev tools on `/profile`, view page source: the
     `<script>` block no longer contains the literal text
     `{ data.User.ID }`.

## Verification ledger

- Fabrication lines (45-52/54-59) and Copy-ID `<script>` bug (Profile.templ:242): read both files on this branch directly.
- templ non-interpolation inside `<script>` + the working attribute-interpolation alternative: read `StatisticsDashboard.templ:43` and `filter_components.templ:7` (shipped precedent), matches prior session's `templ-workflow` memory note.
- proto `User.created_at`/`last_login` already present: `grep -n "message User" -A 25 proto/auth.proto`.
- `models.User.CreatedAt/LastLogin/EmailVerified` real fields, and `EmailVerified` meaningfully set for OAuth only: read `internal/backend/models/models.go:13-27`, `internal/backend/services/oauth_service.go:229,257,288`, `internal/backend/database/oauth_db.go:16-24`; grepped `EmailVerified` across `internal/backend/`.
- `ValidateSession`/`GetProfile` currently drop `LastLogin`/email-verified state: read `internal/backend/services/services.go:217-276` in full.
- `client.User` drops `CreatedAt`/`LastLogin`: read `internal/webui/client/backend_client.go:51-58,277-374` in full.
- `/profile` is served through `RedirectIfNotAuth` → `ValidateSession`, confirming that path alone is sufficient: read `internal/webui/router.go:375-382` and `internal/webui/middleware/auth.go:86-190` in full.
- `GetProfile` (service + wrapper) is unrouted/uncalled dead code: `grep -rn "\.GetProfile(" internal/webui/` (only its own definition and the unrelated raw health-check call at `backend_client.go:143`); `grep -rn "GetProfileData" internal/webui/` (only its own definition, no route).
- `GetProfileData`'s real replacement route is `handlers.GetCurrentUser`, not itself: read `internal/webui/router.go:206` and `internal/webui/handlers/handlers.go:252-266`.
- No existing DB session-by-id lookup exists (forces the cookie-based approach): `grep -n "func (gdb \*GormDB)" internal/backend/database/gorm_db.go` and read `GetUserBySession` at `:297-306`.
- Real 7-day session lifetime, used to justify the mirrored constant: `grep -n "expiresAt" internal/backend/services/services.go` (`:165,1777`) and `internal/webui/middleware/session.go:25`.
- Cookie-based-timestamp precedent (`ImpersonationStartedAt`) exists and is safe to mirror: read `internal/webui/middleware/session.go` in full.
- Exactly two `session_id` write sites, both getting the new key: `grep -rn 'SetSessionValue(c, "session_id"' internal/webui/`; read `internal/webui/handlers/handlers.go:91-160` and `internal/webui/handlers/oauth_handlers.go:66-155` in full.
- `UserStats`/`Stats` fully unrendered and safe to delete: `grep -rn "UserStats\|ProfileData{" internal/webui/`.
- No test exercises `ProfilePage`/`ProfileData`/`Profile.templ`; the one `ValidateSession` test only asserts `Timezone`: `grep -rln "ValidateSession" internal/backend/services/*_test.go internal/webui/client/*_test.go internal/webui/middleware/*_test.go` and read `update_timezone_test.go:70-78`.
- Copy-link icon-swap feedback precedent: read `internal/webui/templates/components/modal_components.templ:1110-1130` and `internal/webui/templates/scripts/dashboard_modal.templ:151-156`.
- User IDs are 32-char lowercase alphanumeric (safe for naive single-quote embedding): read `internal/backend/models/models.go:326-336`.
- `timestamppb`/`.AsTime()` already imported and idiomatic in both edited Go files: grepped `timestamppb` in `services.go` and `backend_client.go`; read `internal/webui/services/color_service.go:180-181` for the `.AsTime()` convention.
