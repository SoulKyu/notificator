# Spec: @mentions in alert comments

- Issue: [SoulKyu/notificator#164](https://github.com/SoulKyu/notificator/issues/164)
- Date: 2026-08-02
- Status: planned

## Problem

Comments and the activity feed already exist, but a comment is a message nobody is
told about. `internal/webui/templates/components/notification_settings.templ`
notifies on **new alerts** filtered by severity — there is no push for collaboration
events. Today the only way to say "look at this alert" is to leave the tool (Slack,
phone), which is exactly the context switch the console should remove.

## Goals

1. Typing `@` in the alert-modal comment box offers usernames; the stored comment
   keeps the plain `@username` text (no schema change).
2. Rendered comments highlight `@username` mentions; the one addressed to the
   viewer is visually distinct.
3. A **Mentions** badge next to the Activity tab shows the count of comments
   mentioning the viewer that they have not seen yet. Clicking it opens the
   activity feed filtered to their mentions; each row deep-links to its alert modal
   via the existing `/dashboard/alert/:id` route.
4. If browser notifications are enabled, a new mention of the viewer raises one —
   regardless of the viewer's severity notification filters.
5. Case-insensitive, word-bounded matching: `@bob` must not match `@bobby`.
   Self-mentions never notify the author.

## Non-goals (unchanged from the issue)

- Group/team mentions (`@sre`), mentioning users who never logged in.
- Any delivery channel beyond the existing browser Notification API.
- Editing a comment to add/remove a mention (comments are create/delete only).
- Cross-device read state for the badge (per-browser `localStorage` only).
- Mentions in acknowledgement reasons.

## Corrections to the issue's proposed approach

Verified against the current branch; three of the issue's approach claims don't
match what's actually in the tree, so the plan below routes around them instead of
building on them:

1. **Comment box/list markup is not in `alert_modal_shared.templ`.** The live
   dashboard alert modal's "Comments" tab — the textarea, Post Comment button, and
   the comment list with per-comment delete — lives in
   `internal/webui/templates/components/modal_components.templ:1434-1553`
   (`x-show="currentAlertTab === 'comments'"`, textarea at
   `modal_components.templ:1449-1453` with `x-ref="commentInput"`, comment render at
   `modal_components.templ:1525` via `x-text="comment.content"`, delete button at
   `modal_components.templ:1532`). `alert_modal_shared.templ:454-508`
   (`AlertModalCommentsReadonly`/`AlertModalCommentsWritable`) is a *second*,
   simpler comment renderer used by the statistics resolved-alert modal
   (`x-text="comment.content"` at `alert_modal_shared.templ:467`). Both need the
   highlighting change; only the first needs the autocomplete change (the
   statistics modal is read-only history, no add-comment box).
2. **`scripts/dashboard_modal.templ` has no markup at all** — it is
   `window.dashboardModalMixin`, pure Alpine data/methods (`addComment`,
   `deleteComment`, `refreshComments`). That part of the issue's claim (comment
   *behavior* lives there) is correct; the box/rendering does not.
3. **There is no "existing dashboard refresh cadence" to hook into for a global
   badge.** `internal/webui/templates/layouts/Base.templ`'s only existing poll
   (`connectedUsersDropdown`, :242-274, `setInterval(... 30000)` at :262) is
   admin-gated and fetches `/api/admin/connected-users` — unrelated to
   activity/mentions, so there's no *dashboard-refresh-shaped* loop to hook a
   mentions badge into. The adaptive refresh loop that exists
   (`startAutoRefresh`/`loadDashboardIncremental`,
   `internal/webui/templates/scripts/dashboard_core.templ:597-602`) only runs on
   the dashboard page's Alpine component — Silences, Statistics and Activity don't
   have it, and the top bar with the Activity tab
   (`internal/webui/templates/components/PageNavigator.templ`) renders on all four
   pages (`StatisticsDashboard.templ`, `Activity.templ`, `Silences.templ`,
   `NewDashboard.templ`). The badge therefore needs its own small, page-independent
   poll living in `PageNavigator.templ`, not a hook into the dashboard's loop.
4. **`/api/v1/users/mentionable` doesn't fit the router.** `router.go` has no
   `/api/v1/users` group — only `/api/v1/alerts`, `/api/v1/dashboard`,
   `/api/v1/auth`, `/api/v1/oauth`, `/api/v1/profile`, plus the separate
   `/api/impersonate` and `/api/admin` groups (`router.go:227-323`). The endpoint
   goes on the existing `dashboard` group instead:
   `dashboard.GET("/mentionable-users", handlers.GetMentionableUsers)`, i.e.
   `GET /api/v1/dashboard/mentionable-users`. It reuses
   `BackendClient.ListUsers` (`internal/webui/client/backend_client.go:1069-1102`),
   which already wraps the `AuthService.ListUsers` gRPC RPC
   (`internal/backend/services/services.go:361-399`) — that RPC only validates the
   session (`services.go:363`), it has **no admin/role check**. The admin gate the
   issue was worried about (`canImpersonate`, `impersonation_handlers.go:23-35`)
   is enforced in the *webui handler* `ListUsersForImpersonation`
   (`impersonation_handlers.go:143-147`), not in the RPC — so a second handler that
   skips that one `if !canImpersonate(c)` check and returns a trimmed field set is
   all that's needed. No new gRPC RPC, no `make proto`, no new `BackendClient`
   method.
5. **`search=@<me>` on the existing `matchesActivitySearch` is not sufficient.**
   `matchesActivitySearch` (`internal/webui/handlers/activity_handlers.go:63-71`)
   does `strings.Contains`, so a search for `@bob` would match content containing
   `@bobby` — violating the "must not match a substring of a longer username"
   acceptance criterion outright. Mention detection needs its own word-boundary
   matcher (see below).
6. **`notification_service.templ`'s `shouldNotify`/`showNotification` path can't
   be reused unmodified.** `shouldNotify`
   (`internal/webui/templates/scripts/notification_service.templ:410-443`) gates on
   `preferences.enabledSeverities` and `matchesFilterPreset`, and
   `showNotificationImmediate`
   (`notification_service.templ:669-736`) is built around alert fields
   (`fingerprint`, `severity`, `source`). A mention event has none of that and must
   *bypass* severity filtering per the issue's own requirement. The plan adds a
   sibling method instead of stretching the alert-shaped one over a different
   event type.

## Approach

### 1. Backend: mention detection on the existing activity feed

`internal/webui/handlers/activity_handlers.go`

Add a pure, testable matcher next to `matchesActivitySearch`:

```go
// matchesMention reports whether content contains a case-insensitive,
// word-bounded @username mention (so "@bob" does not match "@bobby" or an
// email like "x@bob.com").
func matchesMention(content, username string) bool {
	if username == "" {
		return false
	}
	pattern := `(?i)(^|[^A-Za-z0-9_.\-])@` + regexp.QuoteMeta(username) + `([^A-Za-z0-9_.\-]|$)`
	return regexp.MustCompile(pattern).MatchString(content)
}
```

(compiled per call — `GetActivity` processes at most 200 events, not a hot path;
no package-level cache needed). Needs a new `"regexp"` import in
`activity_handlers.go` (not currently imported, `activity_handlers.go:1-17`).

In `GetActivity` (`activity_handlers.go:74-164`), after the existing `kinds` filter
block (`activity_handlers.go:143-151`) and before building `available`
(`activity_handlers.go:153-158`), add an opt-in filter:

```go
if c.Query("mentionsUser") == "1" {
	me := ""
	if u := middleware.GetEffectiveUser(c); u != nil {
		me = u.Username
	}
	kept := feed[:0]
	for _, ev := range feed {
		if me != "" && ev.Kind == "comment" && ev.Username != me && matchesMention(ev.Content, me) {
			kept = append(kept, ev)
		}
	}
	feed = kept
}
```

`ev.Username != me` excludes comments the viewer authored — this is also how
"self-mentions do not notify the author" is satisfied: the feed the badge and
notification path both read from never contains the author's own comments, so
there's nothing separate to suppress downstream. `ev.Kind == "comment"` keeps
mentions in acknowledgement reasons out of scope, matching the issue's explicit
non-goal, even though `ActivityEvent.Kind` already allows `ack`/`silence`/etc.
(`internal/webui/models/activity.go:11`).

Test: add `TestMatchesMention` to `internal/webui/handlers/activity_handlers_test.go`
(same table-driven shape as the existing `TestMatchesActivitySearch`,
`activity_handlers_test.go:51-76`), covering: exact match, case-insensitivity,
`@bob` vs `@bobby` (must not match), `@bob` vs an email-shaped
`user@bob.com` (must not match — the leading-boundary check), and no match on
empty username.

### 2. Backend: minimal mentionable-users endpoint

New handler, e.g. in `internal/webui/handlers/impersonation_handlers.go` (it
already owns `ListUsersForImpersonation`, the closest sibling):

```go
// GetMentionableUsers returns a minimal user list for @mention autocomplete.
// GET /api/v1/dashboard/mentionable-users
func GetMentionableUsers(c *gin.Context) {
	sessionID := middleware.GetSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse("Not authenticated"))
		return
	}
	users, _, err := backendClient.ListUsers(sessionID, 1000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse("Failed to list users"))
		return
	}
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		out = append(out, gin.H{"id": u.ID, "username": u.Username})
	}
	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{"users": out}))
}
```

Wire it in `router.go` inside the existing `dashboard` group (next to the other
`dashboard.GET(...)` lines, e.g. after `router.go:304`):
`dashboard.GET("/mentionable-users", handlers.GetMentionableUsers)`. It sits
behind `dashboard.Use(authMiddleware.RequireAuth())` (`router.go:255`) — any
authenticated user, deliberately not admin-gated, since any team member can be
`@`-mentioned.

### 3. Frontend: `@` autocomplete in the comment box

`internal/webui/templates/components/modal_components.templ:1449-1453` — the
`<textarea x-model="newCommentContent" x-ref="commentInput" ...>`.

Add a small Alpine sub-state (`x-data` merge on the existing tab's wrapper div, or
a dedicated `x-data="mentionAutocomplete()"` on a wrapping `<div>` around the
textarea) with:
- `mentionOpen`, `mentionQuery`, `mentionCandidates` (filtered from a
  `window.__mentionableUsers` cache fetched once from
  `GET /api/v1/dashboard/mentionable-users` and reused by every modal instance —
  same "fetch once, filter client-side" shape as
  `impersonationDropdown.loadUsers()`/`filterUsers()` in
  `internal/webui/templates/layouts/Base.templ:210-232`, not a literal call to it
  since the data shape differs).
- `@input="onCommentInput($event)"` on the textarea: after `newCommentContent`
  updates, look backward from the caret (`$event.target.selectionStart`) for an
  unterminated `@token`; if found, set `mentionQuery` and open a suggestion list.
- The list renders as a simple absolutely-positioned panel directly below the
  textarea (no caret-pixel tracking library — this is a plain multi-line textarea
  with the trigger almost always near the bottom, so anchoring to the textarea's
  bottom edge via `getBoundingClientRect()` is enough; do not add a
  caret-coordinates dependency for this).
- Selecting a suggestion splices `@username` into `newCommentContent` in place of
  the partial token and closes the list. Plain text in, nothing new stored.

Only the live modal (`modal_components.templ`) gets this — the statistics
resolved-alert comment view (`alert_modal_shared.templ:484-508`,
`AlertModalCommentsWritable`) is also "writable" in principle but per the issue's
scope this ships for the primary alert modal; the shared writable component can
gain it later with the same pattern if wanted (no changes needed there for v1
since it delegates to the same `addComment()`/`newCommentContent` the dashboard
mixin provides).

### 4. Frontend: mention highlighting in rendered comments

Both comment renderers currently use `x-text` (escapes everything, no markup
possible):
- `modal_components.templ:1525` — `<p ... x-text="comment.content"></p>`
- `alert_modal_shared.templ:467` — `<p ... x-text="comment.content"></p>`

Switch both to `x-html` bound to a new global helper, e.g.
`window.renderCommentHtml(content, viewerUsername)`, defined once in
`internal/webui/templates/scripts/dashboard_utilities.templ` (next to the other
small formatting helpers like `getCurrentUser`/`canDeleteComment`,
`dashboard_utilities.templ:355-382`) and referenced from both templates as
`x-html="renderCommentHtml(comment.content, getCurrentUser()?.username)"`
(`alert_modal_shared.templ`'s simpler modal doesn't have `getCurrentUser()` in
scope — for that one, pass `comment.content` and let the helper compare against
`window.currentUser?.username` read internally, so both call sites work without
threading extra Alpine state through a shared component's string-built
expressions).

The helper:
1. HTML-escapes `content` (manual `textContent`-via-`document.createElement`
   trick or a small escape map — this codebase has no HTML sanitizer dependency
   to reach for, and escaping four characters by hand is the "one line" rung of
   the ladder).
2. Runs the same `@token` pattern as the backend matcher
   (`/(?:^|[^\w.-])@([A-Za-z0-9_.\-]+)/g`) over the escaped text and wraps each
   match in `<span class="mention">@token</span>`, adding a `mention-self` class
   when `token.toLowerCase() === (viewerUsername || '').toLowerCase()`.
3. Does **not** validate `token` against the mentionable-users list — an
   `@anything`-shaped token gets the neutral `mention` chip style even if no such
   user exists (matches "detected by matching against the current user's
   username" — detection for *notification* purposes is server-side and precise;
   client-side highlighting is cosmetic and fine to be permissive).

Add two Tailwind-friendly utility classes for `.mention` (subtle
blue/rounded chip, matching the existing `bg-blue-100 text-blue-800` accent used
for the `comment` kind badge in `Activity.templ:353`) and `.mention-self`
(stronger accent, e.g. `bg-amber-100 text-amber-900` — visually distinct from a
mention of someone else) in `internal/webui/static/css` input (wherever the
project's Tailwind source lives) or as inline utility classes built by the JS
helper directly (simpler — no new CSS file, just template-literal the classes
into the generated `<span>`).

### 5. Frontend: Mentions badge in the top bar

`internal/webui/templates/components/PageNavigator.templ` — wrap the nav in
`x-data="mentionsBadge()"` and add a badge button next to the Activity tab
(`PageNavigator.templ:71-90`), styled like the existing tab pills.

New Alpine component, defined inline in a `<script>` block at the bottom of
`PageNavigator.templ` (same "component function colocated with its markup"
pattern `Activity.templ:261-403` already uses for `activityPage()`):

```js
function mentionsBadge() {
	return {
		count: 0,
		userId: null,
		poll: null,
		async init() {
			try {
				const r = await fetch('/api/v1/auth/me', { credentials: 'include' });
				const p = await r.json();
				if (p.success) this.userId = p.data.user.id;
			} catch (e) {}
			await this.refresh();
			this.poll = setInterval(() => this.refresh(), 30000);
		},
		storageKey(suffix) { return 'notificator_mentions_' + suffix + '_' + (this.userId || 'anon'); },
		lastSeenAt() { return parseInt(localStorage.getItem(this.storageKey('seen')) || '0', 10); },
		async refresh() {
			try {
				const r = await fetch('/api/v1/dashboard/activity?mentionsUser=1&windowMinutes=10080', { credentials: 'include' });
				const p = await r.json();
				if (!p.success) return;
				const events = p.data.events || [];
				const since = this.lastSeenAt();
				this.count = events.filter(e => new Date(e.createdAt).getTime() > since).length;
				this.notifyNew(events);
			} catch (e) {}
		},
		// Fire a browser notification once per never-notified mention id,
		// independent of the badge's "seen" cutoff (opening the feed clears the
		// badge but must not re-arm notifications for the same mentions).
		notifyNew(events) {
			if (!window.notificationService || typeof window.notificationService.showMentionNotification !== 'function') return;
			const key = this.storageKey('notified');
			let notified = [];
			try { notified = JSON.parse(localStorage.getItem(key) || '[]'); } catch (e) {}
			const now = Date.now();
			const seven = 7 * 24 * 60 * 60 * 1000;
			const notifiedIds = new Set(notified.filter(n => now - n.t < seven).map(n => n.id));
			const fresh = events.filter(e => !notifiedIds.has(e.id));
			fresh.forEach(e => window.notificationService.showMentionNotification(e));
			const merged = notified.filter(n => now - n.t < seven).concat(fresh.map(e => ({ id: e.id, t: now })));
			localStorage.setItem(key, JSON.stringify(merged));
		},
		open() {
			localStorage.setItem(this.storageKey('seen'), Date.now().toString());
			this.count = 0;
			window.location.href = '/activity?mentions=1';
		}
	};
}
```

Rendered on all four pages that include `PageNavigator`
(`StatisticsDashboard.templ`, `Activity.templ`, `Silences.templ`,
`NewDashboard.templ`), 30s poll matching the existing `Activity.templ:282` and
`Base.templ:262` cadences. `window.notificationService` only exists when
`NewDashboard.templ:998`'s `@scripts.NotificationService()` has run — i.e. only
while the dashboard page is open — so `notifyNew`'s guard means mention *browser
notifications* only fire while the dashboard tab is open, same limitation the
existing new-alert notifications already have (`NotificationService` is never
initialized on Silences/Statistics/Activity today either). The **badge count**,
by contrast, updates on all four pages since it doesn't depend on
`NotificationService`.

`windowMinutes=10080` (7 days) is deliberately larger than the activity page's
own default (`windowMinutes: 60`, `Activity.templ:267`) — the badge must not lose
track of a mention posted hours ago just because the poll uses the same short
window the live activity table does.

### 6. Frontend: browser notification for a new mention

`internal/webui/templates/scripts/notification_service.templ` — add a new method
next to `showNotificationImmediate` (`notification_service.templ:668-736`),
reusing its sound/permission/`Notification()` machinery but skipping
`shouldNotify`'s severity/preset gate entirely, per goal 4:

```js
showMentionNotification(event) {
	if (!this.preferences.browserNotificationsEnabled || !this.permissionGranted) {
		return;
	}
	if (!this.canShowNotification()) {
		this.notificationQueue.push({ __mention: true, event });
		setTimeout(() => this.processNotificationQueue(), 10000);
		return;
	}
	this.recordNotification();
	this.playNotificationSound('info');
	const title = `${event.username} mentioned you`;
	const body = (event.content || '').trim();
	try {
		const notification = new Notification(title, {
			body: body,
			icon: this.getNotificationIcon('info'),
			tag: 'mention-' + event.id,
			data: { alertKey: event.alertKey }
		});
		notification.onclick = () => {
			window.focus();
			window.location.href = `/dashboard/alert/${event.alertKey}`;
			notification.close();
		};
	} catch (error) {
		console.error('Failed to show mention notification:', error);
	}
}
```

`processNotificationQueue` (`notification_service.templ:652-666`) currently
assumes every queued item is an alert and calls `showNotificationImmediate`
unconditionally — extend its loop to branch on `item.__mention` and call
`showMentionNotification(item.event)` instead, so a mention queued behind the
5/minute rate limit (`canShowNotification`, `notification_service.templ:632-644`)
still gets shown once a slot frees up, rather than being silently dropped by a
naive push into the alert-shaped queue.

### 7. Frontend: Activity page reads `?mentions=1`

`internal/webui/templates/pages/Activity.templ` — `activityPage()`
(`Activity.templ:261-403`):
- `init()` (`Activity.templ:273-277`): read
  `new URLSearchParams(window.location.search).get('mentions') === '1'` into a new
  `mentionsOnly` field; when true, default `windowMinutes = 10080` before the
  first `load()` (so a mention from yesterday isn't hidden by the normal 60-minute
  default).
- `load()` (`Activity.templ:286-314`): when `mentionsOnly`, add
  `params.set('mentionsUser', '1')` and mark `localStorage` seen (same
  `storageKey('seen')` computation as `mentionsBadge()`, keyed by the same
  `notificator_mentions_seen_<userId>` — fetch `/api/v1/auth/me` once in `init()`
  here too, or read `window.currentUserId` if the page already exposes it) so
  returning to the badge shows 0 without waiting for the next 30s poll.
- Add a small "Showing your mentions — Clear" chip above the filter bar when
  `mentionsOnly` is true, clicking clears the query param (`history.replaceState`)
  and reloads with normal defaults — cheap escape hatch back to the full feed.

## Risks / limitations

- **200-event cap.** `GetActivity` fetches at most 200 recent events total
  (`activity_handlers.go:95`, `backendClient.GetRecentActivity(sessionID, since, 200)`)
  before any filtering. On a very high-traffic team, a mention could in principle
  fall outside the 200 most recent activity events across *all* kinds and go
  uncounted. Low risk in practice (the cap drops the oldest events first, so a
  fresh mention is never the one dropped), not solved by this change — flagging it
  as a known ceiling, not a blocker.
- **Notification only fires while the dashboard tab is open**, because
  `NotificationService` is only initialized there (`NewDashboard.templ:998`). This
  matches the status quo for ordinary alert notifications; not a regression, but
  worth calling out since the issue's acceptance criteria imply "browser
  notification fires" without that caveat.
- **Client-side highlighting is permissive** (any `@token`-shaped text gets a
  mention chip, not just real usernames) — a deliberate simplification to avoid
  threading the mentionable-users list into every comment-render call site;
  correctness for *notifications* (the part with real consequences) is
  server-side and exact.

## Validation

- `go build ./...` and `go vet ./...` from repo root.
- `go test ./internal/webui/handlers/... -run 'TestMatchesMention|TestMatchesActivitySearch|TestBuildActivityFeed'`
  — new + existing activity tests green.
- `make webui-templates` after editing every `.templ` file touched above
  (`modal_components.templ`, `alert_modal_shared.templ`, `dashboard_utilities.templ`,
  `notification_service.templ`, `PageNavigator.templ`, `Activity.templ`) — never
  hand-edit the generated `*_templ.go` files.
- `make test` (docker-compose full stack) manual pass:
  1. As user `alice`, comment `hi @bob check this` on an alert. As `bob` (second
     browser/session), confirm: badge increments within 30s, `/activity?mentions=1`
     shows the row, clicking it opens the right alert modal and the badge drops to
     0.
  2. With `bob`'s severity notification preferences excluding this alert's
     severity, confirm the mention still raises a browser notification (requires
     `bob`'s dashboard tab open and browser notification permission granted).
  3. Comment `@bobby` on an alert bob is not mentioned in by that exact match;
     confirm no badge/notification for `bob`.
  4. As `alice`, comment `@alice note to self`; confirm no badge/notification for
     `alice`.
  5. Type `@` in the comment box, confirm the suggestion dropdown lists real
     usernames from `GET /api/v1/dashboard/mentionable-users`.

## Verification ledger

- Comment box/list markup location (`modal_components.templ` vs.
  `alert_modal_shared.templ`) — grepped `canDeleteComment`/`comment.content`
  across `internal/webui/templates/`, read both files directly.
- `dashboard_modal.templ` is JS-only, no HTML — read the file, confirmed 0
  `<div>`/`<template>`/`<textarea>` occurrences via grep.
- `Base.templ`'s only poll (`connectedUsersDropdown`, :242-274) is admin-gated
  and unrelated to activity/mentions; dashboard's adaptive loop is
  page-scoped — read `Base.templ` in full and `dashboard_core.templ:597-602`;
  grepped `PageNavigator` usage across `pages/*.templ`.
- `/api/v1/users` group does not exist; existing groups enumerated — read
  `router.go:180-323` in full.
- `AuthService.ListUsers` gRPC has no admin check (only session validation) —
  read `services.go:361-399`; `BackendClient.ListUsers` wrapper already exists —
  read `backend_client.go:1069-1102`.
- `ListUsersForImpersonation`'s admin gate is handler-side (`canImpersonate`), not
  RPC-side — read `impersonation_handlers.go:23-35,143-175`.
- `matchesActivitySearch` uses `strings.Contains` (substring match) — read
  `activity_handlers.go:63-71`.
- `ActivityEvent` field shape (`id`, `alertKey`, `kind`, `username`, `content`,
  `createdAt`) — read `internal/webui/models/activity.go`.
- `shouldNotify`/`showNotificationImmediate` are alert-shaped and gate on
  severity/preset — read `notification_service.templ:410-443,668-736`.
- `/dashboard/alert/:id` page route exists — grepped `router.go:380`.
- `Comment` JSON shape (`id`, `username`, `userId`, `content`, `createdAt`) — read
  `internal/webui/models/dashboard.go:267-276`; confirmed `GetAlertDetails`
  populates it from `backendClient.GetComments` at `dashboard_handlers.go:1427-1430`.
- `GET /api/v1/auth/me` response shape (`data.user.{id,username,email}`) — read
  `handlers.go:252-266` and `models/api_response.go:17-22`.
- Comment content cap is 1000 chars, stored as-is (no server-side mention
  parsing today) — read `dashboard_handlers.go:1525-1586`.
