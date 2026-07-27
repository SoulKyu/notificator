# Spec: advanced silence creation modal on the Silences page

- Issue: [SoulKyu/notificator#130](https://github.com/SoulKyu/notificator/issues/130)
- Date: 2026-07-27
- Status: planned

## Problem

`/silences` (`internal/webui/templates/pages/Silences.templ`) is read-only for
creation. Silences only come into existence through the alert bulk *silence*
action (`processSilenceAction` in `internal/webui/handlers/dashboard_handlers.go:1953`),
which derives matchers from one alert's labels, silences on every configured
source unconditionally, and offers only a small preset duration list capped
by nothing but the UI. There is no way to author a silence from scratch:
arbitrary matchers (including negation/regex), a chosen subset of sources, a
future start time, or a duration beyond the presets.

`internal/webui/handlers/silence_handlers.go` already has the matching
primitives this feature needs (`compileMatchers`, `matchLabels`,
`countMatchedAlerts`, `toWebuiSilence`) — they're reused, not reinvented.

## Goals

1. A **Create silence** button on `/silences` opens a modal to build a
   silence with any number of matchers (equality, negation, regex, mixed),
   targeting one, several, or all configured Alertmanagers.
2. `startsAt` defaults to now but can be a future datetime (pending silence).
3. `endsAt` supports duration presets, free-form durations (`90m`, `36h`,
   `2w`), and an absolute datetime — with **no maximum duration clamp**. The
   30-day `maxSilenceExtension` cap is specific to the extend endpoint and
   must not leak into creation.
4. A live preview shows, per selected source, how many cached alerts the
   in-progress matcher set would match, updating as the user edits — before
   any silence is created. Zero-match and very-large-match counts are
   flagged as warnings, never blocking.
5. Creating across multiple sources reports per-source success/failure
   instead of a single pass/fail verdict.
6. Every silence row gets a **Duplicate** action (including expired ones)
   that prefills the modal from that silence.
7. `createdBy` is the authenticated user's username, consistent with the
   existing fix in 1bdfb3f.

## Non-goals

- No matcher-editing UI beyond the modal (e.g. no inline row editing on the
  table itself).
- No server-side persistence of "draft" silences — the modal is stateless
  across page reloads.
- No change to the existing bulk alert *silence* action or its endpoint;
  this ships a parallel, more capable path, it doesn't touch the old one.
- No label-name/value autocomplete backend work — `GET
  /api/v1/dashboard/available-labels` (`internal/webui/handlers/dashboard_handlers.go:1687`)
  already returns exactly this and is reused as-is.
- No new duration-parsing package — extend the existing
  `parseExtendedDuration` (`internal/webui/handlers/dashboard_handlers.go:35`)
  with a week unit rather than writing a new parser.

## Approach

### Backend: two new endpoints, same matching primitives

`internal/webui/handlers/silence_handlers.go`:

**`POST /api/v1/dashboard/silences/preview`**
Request: `{sources: string[], matchers: SilenceMatcher[]}`.
For each requested source, build a throwaway `models.Silence{Matchers:
matchers}`, run it through the existing `compileMatchers` /
`countMatchedAlerts` against `alertCache.GetAllAlerts()`, and return counts
per source plus a capped sample list (alertname + a handful of key labels,
e.g. first 20) for the expandable preview. Invalid regex → `compileMatchers`
already returns `ok=false`; surface that as a per-matcher validation error
rather than a silent 0-count, so the client can flag the bad row instead of
misreading "no matches" as "not matching yet".

**`POST /api/v1/dashboard/silences`**
Request: `{sources: string[], matchers: SilenceMatcher[], startsAt, endsAt,
comment: string}`. Validation is syntactic only, per the issue:
- `sources` non-empty and every entry in `alertmanagerClient.GetClientNames()`.
- `matchers` non-empty, each with a non-empty `name`; regex matchers compiled
  the same way `compileMatchers` does (`^(?:...)$`) to fail fast with the
  same semantics Alertmanager will apply.
- `endsAt > startsAt`. No comparison against "now" (a pending silence has
  `startsAt` in the future) and **no upper bound** — `maxSilenceExtension`
  is not referenced here.
- `comment` non-empty (Alertmanager requires it; default supplied client-side
  if the user leaves it blank).

For each source, call `alertmanagerClient.CreateSilenceOnAlertmanager(source,
silence)` with `CreatedBy` set to the session's username (same
`middleware.GetSessionIDFromContext` + backend lookup pattern
`annotateSilenceOrigins` already uses, or simply the already-authenticated
`middleware.GetCurrentUserFromContext(c).Username` — cheaper, no extra
backend round-trip, and this is the user acting right now rather than a
historical record needing resolution). Collect per-source
`{source, success, silenceID?, error?}` and return the full list — never
collapse to one status the way `processSilenceAction` currently does.

Router (`internal/webui/router.go`, next to the existing silence routes at
line 290-292):
```go
dashboard.POST("/silences", handlers.CreateSilence)
dashboard.POST("/silences/preview", handlers.PreviewSilence)
```

Extend `parseExtendedDuration` (`internal/webui/handlers/dashboard_handlers.go:35`)
with a `w` → `*7d` substitution alongside the existing `y`/`d` handling, and
use it (not raw `time.ParseDuration`) for the create modal's free-form
duration field so `2w` works. `ExtendSilence`'s own duration parsing and its
30-day clamp are untouched — they're a different endpoint with a different
contract.

### Frontend: modal in `Silences.templ`, matching the existing Alpine pattern

Everything lives in the same file as the current `silencesPage()` component
(no new JS file, no new dependency — consistent with how the extend/expire
actions are already built inline). Add to `SilencesContent`:

- A **Create silence** button in the header next to Refresh, toggling
  `showCreateModal`.
- A **Duplicate** button per row (next to Extend/Expire) calling
  `duplicate(silence)`, which opens the same modal pre-populated from that
  row — works for expired silences since it's just prefill, not a fetch of
  live state.
- The modal itself: matcher rows (`name`, operator select `= / != / =~ /
  !~`, `value`, remove button, plus an "Add matcher" button — array-backed,
  no cap), a paste box that parses `{a="b", c=~"d.*"}` into rows on blur
  (split top-level commas, regex-match `key(operator)"value"` per segment;
  purely client-side string parsing, no server round-trip needed to import),
  a source checkbox list (`sources`, already loaded by `loadSilences`,
  default all checked), start-now/start-later radio + datetime input,
  end-preset buttons (`1h`/`4h`/`24h`/`7d`) + free-form text input + absolute
  datetime input, a comment textarea (default "Silenced from
  /silences"), and a preview panel.

Alpine state additions to `silencesPage()`:
```js
showCreateModal: false,
createForm: { matchers: [...], sources: [...], startsAt: null, endsAt: null,
              durationInput: '', comment: '' },
preview: { loading: false, bySource: {}, error: '' },
createResult: null, // per-source outcome list shown after submit
```
- Matcher-name/value autocomplete: fetch `/api/v1/dashboard/available-labels`
  once when the modal opens (mirrors how the dashboard's own filter UI
  already consumes that endpoint) and filter client-side as the user types —
  no new backend call shape.
- Preview: debounce (e.g. 300ms) a call to `/api/v1/dashboard/silences/preview`
  on every matcher/source edit; render count + expandable alert list per
  source; show a non-blocking amber note for 0 matches and for counts above
  a fixed threshold (e.g. 200) — informational only, submit stays enabled.
- Submit: `POST /api/v1/dashboard/silences`, then render `createResult`
  (per-source ✓/✗ with the error text) and `loadSilences()` to refresh the
  table; keep the modal open on partial failure so the user can see which
  source needs a retry, close it on full success.
- Keyboard: `@keydown.escape.window` closes the modal (guarded like the
  existing dashboard shortcut work in `dashboard_core.templ` — inert while
  another modal/input would swallow it, though `/silences` has only this one
  modal today); `Enter` submits when the form validates (non-empty matchers
  with valid regex, non-empty sources, `endsAt > startsAt`), consistent with
  how `Ctrl+Enter` submit patterns already exist elsewhere in the webui.
- Theme: reuse the existing `dark:` class pairs already used throughout
  `Silences.templ` — no new theme logic.

After editing `Silences.templ`, regenerate with `make webui-templates`
(never hand-edit `Silences_templ.go`).

### Files touched

- `internal/webui/handlers/silence_handlers.go` — `CreateSilence`,
  `PreviewSilence` handlers; small request/response structs.
- `internal/webui/handlers/dashboard_handlers.go` — extend
  `parseExtendedDuration` with week support.
- `internal/webui/router.go` — register the two new routes.
- `internal/webui/templates/pages/Silences.templ` — Create button, Duplicate
  action, modal markup, Alpine state/methods.
- Regenerated `internal/webui/templates/pages/Silences_templ.go`.
- `internal/webui/handlers/silence_handlers_test.go` — new tests (below).

## Risks & trade-offs

- **Per-source fan-out has no partial-rollback.** If a silence is created on
  2 of 3 sources and the 3rd fails, the first 2 stay live — this matches the
  issue's explicit ask ("report per-source success/failure", not
  transactional all-or-nothing) and mirrors the existing `processSilenceAction`
  behavior, so it's a deliberate consistency choice, not an oversight.
- **Preview cost**: `countMatchedAlerts` scans the whole `alertCache` per
  source per keystroke-ish edit. Debouncing on the client bounds request
  rate; the scan itself is the same cost `GetSilences` already pays per
  silence on every page load, so this isn't a new order of magnitude.
- **No server-side rate/size limit on matcher count or preview sample size**
  beyond the issue's "no artificial limits" ask — the preview response caps
  the *sample list* at ~20 alerts (bandwidth, not policy) while the *count*
  stays exact and uncapped.
- **CreatedBy divergence**: using `GetCurrentUserFromContext(c).Username`
  directly (vs. the backend-resolved-from-ID path `annotateSilenceOrigins`
  uses for display) is fine here because this is the live authenticated
  session creating the silence right now, not a historical `createdBy`
  string being reinterpreted later — no resolution ambiguity to guard against.
- **Duplicating an expired silence** reuses its matchers/comment but not its
  `startsAt`/`endsAt` (those default fresh, per the issue: recreation is the
  documented alternative to extending an expired silence, not a verbatim
  clone of a lapsed time window).

## Validation

- `make webui-templates && go build ./...` passes.
- New/updated Go tests in `silence_handlers_test.go`:
  - `CreateSilence`: rejects empty matchers, unknown source, `endsAt <=
    startsAt`, invalid regex; accepts a duration far beyond 30 days (proves
    no `maxSilenceExtension` leakage); reports mixed per-source
    success/failure when one client errors.
  - `PreviewSilence`: count matches `countMatchedAlerts` for a known fixture
    cache; invalid regex surfaces as an error, not a 0 count.
  - `parseExtendedDuration`: `2w` → `14 * 24h`.
- Manual check via `make test` (docker-compose stack), covering the issue's
  acceptance criteria directly:
  1. Create a silence with 5+ mixed matchers (`=`, `!=`, `=~`) on a subset of
     sources; confirm it appears in the table with the logged-in username as
     creator.
  2. Watch the preview count update per source while editing matchers.
  3. Create a pending silence (future `startsAt`) and one with `endsAt` >
     30 days out; both succeed.
  4. Paste `{alertname=~"Foo.*", team="bar"}` and confirm both rows populate
     correctly.
  5. Duplicate an expired silence; confirm matchers/comment prefill.
  6. Force one Alertmanager to be unreachable, create across all sources,
     confirm the per-source failure is reported without hiding the
     successes.
