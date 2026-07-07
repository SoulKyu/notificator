# Notification System

There are **two unrelated notification systems** in the tree. Know which one you're touching.

| System | Where | Status |
|--------|-------|--------|
| **Browser notifications** (visual + sound, in the WebUI) | `scripts/notification_service.templ` + `components/notification_settings.templ` + `handlers/notification_handlers.go` + backend gRPC + DB | **LIVE — the real system** |
| **Desktop notifier** (Fyne OS tray + audio devices) | `internal/notifier/notifier.go`, `internal/audio/*` | **DEAD — unwired, delete-me** |

This page documents the live browser system and the dead one only enough to remove it. Never edit
`*_templ.go` — see [operations](operations.md#codegen). The live system was hardened by a
dedicated audit (see the `fix(notifications):` history); the behavior below is the current state.

## Browser notifications (supported)

The client object `window.NotificationService`
(`internal/webui/templates/scripts/notification_service.templ:6`) is injected into the dashboard
via `@scripts.NotificationService()` and driven by the dashboard's alert lifecycle (see
[dashboard](dashboard.md#notification-hooks)).

### Permission and load ordering

`init(userID)` (`notification_service.templ:20`) runs from the dashboard's `loadCurrentUser()`,
which is now **awaited** in `dashboard_core.templ init()` before SSE and the first data load — so
the seen-set is seeded before live updates are processed. `init` loads saved preferences, reads
`Notification.permission` (**does not auto-prompt**), and loads the `seenAlerts` set from
`localStorage` (`notificator_seen_alerts_<userID>`, 24h TTL). `Notification.requestPermission()`
fires only on a user gesture: the Settings → Notifications link or the dismissible banner
(`enableNotifications()`, `dashboard_core.templ`). The banner's dismissed state is stored under a
**per-user** key, and its dismiss button carries an `aria-label`.

### What triggers a notification

Alerts flow through one choke point, `applyIncrementalUpdate(update, source)`
([dashboard](dashboard.md#live-updates-sse-and-the-client-side-merge)), which ends by calling
`processNewAlerts(this.alerts, this.filters, userID)` (`notification_service.templ`):

1. On the initial full load, `initializeSeenAlerts()` **seeds** the seen-set. It now **merges**
   (union) into the existing set rather than replacing it, and is seeded **once per session**
   (gated on `seenAlertsInitialized` at the `dashboard_data.templ` call site) — so filter changes
   and pagination no longer forget or re-notify already-seen alerts.
2. `detectNewAlerts()` diffs live alerts against `seenAlerts`.
3. New alerts are filtered through the user's **current dashboard filters** *and* the user's
   **global hidden-alerts list** — globally hidden alerts are marked seen but never notified.
4. Survivors are shown with a 500ms stagger; all newly-seen fingerprints are marked seen.

`shouldNotify(alert)` gates on: browser notifications enabled, permission granted, and the alert's
normalized severity being in `enabledSeverities`. `critical-daytime` is normalized to `critical`
so it can notify (and gets `requireInteraction`).

**Resolved / flapping alerts re-notify.** When the SSE stream reports an alert removed (a genuine
Alertmanager resolve), `applyIncrementalUpdate` calls `forgetAlerts()` to evict it from the
seen-set — so a later re-fire of the same fingerprint notifies again. This eviction is scoped to
**SSE-sourced removals only** (`source === 'sse'`); the *poll* path's `removedAlerts` conflate
resolve with filter-out and therefore never evict. See
[dashboard](dashboard.md#live-updates-sse-and-the-client-side-merge).

### What it shows

`showNotificationImmediate()`: title `Alert: <name>`, body = summary (or `<SEVERITY> alert from
<source>`), a per-severity PNG icon from `static/images/`, `tag: fingerprint` (OS-level dedup),
and `requireInteraction: true` for `critical`/`critical-daytime`. Clicking focuses the window and
opens the alert detail modal (or navigates to `/dashboard/alert/<fingerprint>`).

### Severity, sound, and spam control

- **Severity** — `enabledSeverities` defaults to `['critical', 'warning']`; `info` is off by
  default. `information`→`info` and `critical-daytime`→`critical` are normalized for the check.
- **Sound** — `playNotificationSound()` creates a fresh `Audio` per notification at fixed volume
  0.7, gated by `soundNotificationsEnabled`. Files in `static/sounds/`.
- **Spam control** — dedup via the `seenAlerts` set + 24h `localStorage` TTL (now applied on
  **every** write in `markAsSeen`, not just at page load, so storage stays bounded); a 5/minute
  rate limit with a retry queue; a 500ms batch stagger; OS-level `tag` dedup; and **cross-tab**
  dedup via a per-user `BroadcastChannel` (`notificator_seen_alerts_<userID>`) that shares
  newly-seen fingerprints so other tabs don't re-notify (feature-detected; browsers without
  `BroadcastChannel` fall back to per-tab behavior). A **catch-up pass** runs on tab refocus so
  alerts that fired while the tab was hidden are still evaluated.

### Preferences persistence

Model `NotificationPreference` (`internal/backend/models/notification_preference.go`): one row per
user (`browser_notifications_enabled` default false, `enabled_severities` JSONB default
`[critical, warning]`, `sound_notifications_enabled` default true). Path:
`GET/POST /api/v1/notifications/preferences` (`handlers/notification_handlers.go`) →
`backendClient` gRPC → `services.go` → `database/notification_db.go`.

Hardened behavior:
- **Saves send all three fields** — the client no longer omits `sound_notifications_enabled`, so
  enabling notifications never silently disables sound.
- The DB layer **updates explicit columns** (`Model(&existing).Select(...).Updates`) instead of a
  full-row `Save`, so a partial payload can't wipe untouched fields.
- Not-found is detected via `errors.Is(err, gorm.ErrRecordNotFound)`.
- `GetNotificationPreferences` **surfaces genuine DB errors** (Success:false / HTTP 500) instead
  of returning a fake "success + defaults" response; a true not-found still yields defaults.
- The Settings modal uses `window.notificationService` as the **single source of truth** — it
  copies the service's preferences only once they've actually loaded (`preferencesLoaded` guard),
  otherwise falls back to its own fetch, avoiding the "overwrite with defaults" race.

## Desktop notifier (deprecated — delete candidate)

`internal/notifier/notifier.go` (~556 lines) is a **completely separate** pipeline for the removed
Fyne desktop GUI: OS tray notifications, escalation detection, per-alert cooldown,
`SeverityRules`, and device-aware audio via `internal/audio/*`. Its config is
`config.NotificationConfig` / `Config.Notifications`.

**Confirmed dead:** nothing constructs a `Notifier`; `internal/audio` is imported only by
`notifier.go`; `fyne.io/fyne/v2` in `go.mod` exists **only** for `notifier.go`; the desktop
command is gone (only the `nogui` error stub remains). See
[architecture](architecture.md#build-variants). `internal/notifier/`, `internal/audio/`,
`config.NotificationConfig` / `Config.Notifications`, and the `fyne` dependency can be removed
together in one cleanup PR.

## Gotchas {#gotchas}

- **Two `NotificationConfig`-ish types:** the dead `notifier.NotificationConfig` vs. the live
  DB-backed `models.NotificationPreference`. Don't confuse them when grepping.
- **Seen-set eviction is SSE-only by design** — the poll path deliberately does not evict, because
  its `removedAlerts` conflate resolve with filter/silence/ack/pagination. Don't add eviction
  there without a backend signal distinguishing the two.
- **Cross-tab dedup is best-effort** — `BroadcastChannel` only; unsupported browsers (or private
  windows in some browsers) still notify per-tab.
- **No shared/preloaded audio element** — a new `Audio` object is created per notification; if
  sound "doesn't play", check the browser autoplay-policy console warning first.
- **Minor, not yet addressed:** each rate-limited notification schedules its own queue-drain timer
  (redundant); a rejected notification can still consume a rate slot (the guard runs after
  `recordNotification`); and `getNotificationSound` has no `success` entry (falls back to info).
