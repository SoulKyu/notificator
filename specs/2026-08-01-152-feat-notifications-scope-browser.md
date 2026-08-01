# Spec: scope browser/sound notifications to a saved filter preset

- Issue: [SoulKyu/notificator#152](https://github.com/SoulKyu/notificator/issues/152)
- Date: 2026-08-01
- Status: planned

## Problem

The only notification filter today is severity (`enabledSeverities`, gated in
`shouldNotify()` at `internal/webui/templates/scripts/notification_service.templ:240`).
On a shared Alertmanager (multi-team, multi-tenant Mimir), the on-call
engineer for team A gets a browser popup + sound for every `critical` in the
estate, including alerts that belong to other teams. The practical failure
mode: people turn browser notifications off entirely and miss the one that
was theirs.

Users already express "which alerts are mine" precisely, as a **saved filter
preset** (alertmanagers / severities / statuses / teams / alertNames). That
scope isn't reused by the notification path.

There is a second, undocumented coupling worth naming: `processNewAlerts`
(`internal/webui/templates/scripts/dashboard_data.templ:454`) already ANDs
new-alert notifications against `this.filters` — the dashboard's *live,
currently-applied* view filters — via `alertMatchesFilters()`
(`notification_service.templ:351`). The settings modal even documents this
("Notifications respect your current dashboard filters",
`internal/webui/templates/components/notification_settings.templ:160`). That
coupling is exactly the anti-pattern this feature must not deepen: it means
notification scope silently changes whenever the operator changes what
they're looking at, so getting scoped notifications today means never
browsing outside your own filter. The new preset-based scope must be
**independent of the live view filters** so an operator can browse
everything while being pinged only for their scope — this is the point of
the feature, not an edge case of it.

## Goals

1. Notification settings gets a preset picker: "Notify me only for alerts
   matching: `[ All alerts ▾ ]`", populated from the user's filter presets
   (including shared ones), next to the existing severity checkboxes.
2. Default `All alerts` (empty/null selection) → byte-for-byte today's
   behaviour.
3. Selecting a preset scopes browser notification + sound to alerts that
   match that preset's `filter_data` **and** pass the severity gate (ANDed;
   severity stays the quick knob). This scope is independent of the live
   dashboard `filters` / current view — it does not touch
   `processNewAlerts`'s existing `alertMatchesFilters(alert, currentFilters)`
   call, it adds a second, separate check.
4. The selection persists server-side (survives reload and re-login).
5. If the selected preset is later deleted, the setting falls back to `All
   alerts` without a JS error, and the dropdown reflects the fallback.

## Non-goals (per issue)

- Server-side notification delivery (email, Slack, webhooks).
- Per-preset sound choice, quiet hours, scheduling.
- Changing what the dashboard table displays.
- Selecting multiple presets at once.
- Matching the preset's free-text `search` field. The existing list-based
  matchers (`alertMatchesFilters` client-side, `alertPassesAlertLevelFilters`
  server-side at `internal/webui/handlers/dashboard_handlers.go:403`) both
  deliberately treat search as a separate, fuzzy concern from label-list
  filtering; the preset scope reuses their semantics (empty list = no
  constraint) and stays list-only for the same reason.

## Approach

### Proto + backend (round-trip the field, no new RPC)

- `proto/alert.proto`: add `string notification_filter_preset_id = 8;` to
  `NotificationPreference` (`proto/alert.proto:480`) and
  `string notification_filter_preset_id = 4;` to
  `SaveNotificationPreferencesRequest` (`proto/alert.proto:467`).
  Regenerate with `make proto` — never hand-edit `*.pb.go`.
- `internal/backend/models/notification_preference.go`: add
  `NotificationFilterPresetID string \`gorm:"type:varchar(36)" json:"notification_filter_preset_id,omitempty"\`
  ` (nullable/empty = all alerts). Schema change is picked up by the
  existing `AutoMigrate()` (`internal/backend/database/gorm_db.go:114`) —
  no hand-written migration.
- `internal/backend/services/services.go`: thread the field through
  `GetNotificationPreferences` (~line 2520) and
  `SaveNotificationPreferences` (~line 2566), same shape as
  `EnabledSeverities` today. No ownership/existence check against
  `filter_presets` server-side — the dropdown only ever offers presets the
  session can already see (own + shared), so an invalid/foreign ID can't
  reach the save call through the UI; the deleted-preset case is handled by
  the client falling back to "All alerts" (Goal 5), not by backend
  validation.
- `internal/webui/client/backend_client.go`: add the field to the
  `NotificationPreferences` struct (`:1440`) and thread it through
  `GetNotificationPreferences`/`SaveNotificationPreferences` (`:1447`,
  `:1485`), mirroring `EnabledSeverities`.
- `internal/webui/handlers/notification_handlers.go`: pass
  `notification_filter_preset_id` through `GetNotificationPreferences`
  (`:15`) and `SaveNotificationPreferences` (`:44`) request/response JSON,
  same pattern as the existing three fields.

### Pure, testable matcher (new static JS, not templ-inline)

The repo has no JS test runner and no `static/js/` directory today — all
notification logic lives inline in `<script>` blocks inside `.templ` files,
which can't be `require`d by a test. The issue explicitly asks for the
matcher to be "a pure function so it is unit-testable", so:

- New `internal/webui/static/js/notification-matcher.js` (plain JS, served
  automatically — `r.Static("/static", ...)` at `internal/webui/router.go:159`
  already covers the whole static tree, no router change needed). Exports
  one function: `matchesPreset(alert, filterData)`, adapting field names
  (`filterData.alert_names` → the `alertNames` shape `alertMatchesFilters`
  already expects) and reusing the exact same list semantics as
  `alertMatchesFilters` (`notification_service.templ:351`) — alertmanagers,
  severities, statuses, teams, alertNames, empty list = no constraint.
  This is new code only for the field-name adapter; the matching semantics
  are copied from, not reinvented from, the existing function.
- Load it via one `<script src="/static/js/notification-matcher.js">` tag
  before `notification_service.templ`'s script (wherever `NotificationService()`
  is rendered, e.g. `internal/webui/templates/pages/NewDashboard.templ`).
- `shouldNotify(alert)` (`notification_service.templ:240`) gets one more
  check after the severity gate: if
  `this.preferences.notificationFilterPresetId` is set and its cached
  `filter_data` is loaded, `window.NotificationMatcher.matchesPreset(alert,
  filterData)` must also be true.
- Test: `internal/webui/static/js/notification-matcher.test.js` using
  Node's built-in test runner (`node --test`, `node:assert`) — zero new
  dependencies. Covers match / no-match / empty-filter-data cases per the
  acceptance criteria. Add `"test": "node --test internal/webui/static/js"`
  to `package.json`'s `scripts`.

### Frontend state + UI

- `internal/webui/templates/scripts/notification_service.templ` is the
  actual source of truth for `this.preferences` (`shouldNotify()` at `:240`
  reads off it directly) and must gain `notificationFilterPresetId` first:
  - `loadPreferences()` (`:84`-`:105`) reconstructs `this.preferences` from
    an explicit field allowlist — add
    `notificationFilterPresetId: result.data.notification_filter_preset_id || null`
    to it, or the field is silently dropped on every page load/reload
    (breaks Goal 4).
  - `savePreferences(preferences)` (`:108`-`:136`) POSTs an explicit-allowlist
    body — add `notification_filter_preset_id: preferences.notificationFilterPresetId`
    to it. Without this, `dashboard_core.templ`'s `enableNotifications()`
    (`:600`-`:616`, the "Enable Notifications" banner) calls `savePreferences`
    with `window.notificationService.preferences` as-is and would silently
    reset an already-configured preset scope back to "All alerts" as a
    side effect of granting browser permission.
- `internal/webui/templates/scripts/dashboard_settings.templ`:
  - `loadNotificationPreferences()` (`:890`) copies from
    `window.notificationService.preferences` (documented as the "Single
    source of truth") rather than fetching independently — once
    `notification_service.templ`'s `loadPreferences()` carries the field
    (above), add `notificationFilterPresetId` to both the primary copy and
    the fallback-fetch branch here, mirroring how `enabledSeverities` is
    already read.
  - `saveNotificationPreferences()` (`:924`) gains `notificationFilterPresetId`
    in its POST body, same as `enabledSeverities`.
  - New `loadNotificationPresetFilterData()`: fetch
    `GET /api/v1/dashboard/filter-presets?include_shared=true` (already
    exists, used today by `dashboard_filter_presets.templ:79`) once when the
    Notifications settings tab is opened, to populate the `<select>` options
    and cache the `filter_data` of whichever preset is selected. Re-fetch
    only on selection change, not per-alert — no extra request per alert per
    the issue's explicit constraint.
  - If the previously-saved `notificationFilterPresetId` isn't present in
    the fetched list (deleted preset), reset to `null`/`All alerts` client-side
    (Goal 5) and let the next save persist that fallback.
  - Cache-gap on plain reload: `filter_data` is only fetched when the
    Notifications settings tab is opened, but `notificationFilterPresetId`
    is available as soon as preferences load (page init). Between reload and
    first tab-open in that session, `shouldNotify()` has a preset ID but no
    cached `filter_data` for it — treat this as unscoped (skip the preset
    check, fall through to the severity gate only) rather than failing
    closed, since failing closed would silently suppress all notifications
    with no user-visible cause.
- `internal/webui/templates/components/notification_settings.templ`: add
  the preset `<select>` (Alpine `x-model="notificationPreferences.notificationFilterPresetId"`,
  options from the loaded preset list, first option "All alerts" = empty
  string) next to the severity checkboxes (~line 87). Update the "How it
  works" copy (`:160`, "Notifications respect your current dashboard
  filters") to also mention the independent preset scope, since that line
  is the one currently documenting the coupling this feature must not
  conflate with.
- Regenerate with `make webui-templates` — never hand-edit `*_templ.go`.

## Risks / trade-offs

- **Field-name drift**: `filter_data` uses `alert_names` (snake_case, see
  `dashboard_filter_presets.templ:176`) while `alertMatchesFilters` expects
  `alertNames`. The adapter must translate this explicitly; a naive pass-through
  would silently no-op the alertName constraint. Covered by the unit test's
  match/no-match cases.
- **Stale cached filter_data**: the preset's `filter_data` is fetched once
  per settings-modal open/selection-change, not live. If someone edits the
  shared preset's filters in another tab, this tab's notification scope is
  stale until the settings modal is reopened. Acceptable — same staleness
  window the issue's "fetched once on settings load" approach already
  accepts, and presets change far less often than alerts fire.
- **Two independent AND gates now exist** (live-view filters in
  `processNewAlerts`, and the new preset scope in `shouldNotify`). Worth a
  code comment at the `shouldNotify` call site so a future reader doesn't
  conflate or try to merge them.

## Validation

- `go build ./...` after proto regen (`make proto`) and template regen
  (`make webui-templates`).
- `node --test internal/webui/static/js` for the matcher unit test
  (match / no-match / empty-filter-data cases, per acceptance criteria).
- Manual (`make test` docker-compose stack):
  1. `All alerts` selected → confirm notification behaviour unchanged.
  2. Select a preset scoped to one team → fire/observe a non-matching
     critical alert (no popup/sound) and a matching one (popup + sound).
  3. Disable the matching alert's severity → confirm no notification even
     though the preset matches (severity gate still applies on top).
  4. Reload the page and re-login → confirm the preset selection persists.
  5. Delete the selected preset → confirm the dropdown falls back to `All
     alerts` with no console error.
