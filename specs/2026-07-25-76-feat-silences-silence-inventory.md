# Spec: silences — silence inventory page + real silences in the alert modal

- Issue: [SoulKyu/notificator#76](https://github.com/SoulKyu/notificator/issues/76)
- Date: 2026-07-25
- Status: planned

## Problem

Notificator can create silences (dashboard → Silence, single or bulk, via
`processSilenceAction` in `internal/webui/handlers/dashboard_handlers.go:1903`)
and remove the ones attached to a currently-firing alert
(`processUnsilenceAction`). Everything else about a silence's life is invisible:

- **No expiry visibility.** A 2h silence created at 01:00 expires at 03:00 with
  no warning anywhere in the UI.
- **No inventory.** A silence whose alert is no longer firing exists only in
  Alertmanager's own UI — including the stale `severity=critical` silence from
  three weeks ago that is quietly suppressing a real page.
- **N copies, N UIs.** `processSilenceAction` creates the silence on *every*
  configured Alertmanager (`client.CreateSilence` per client). Auditing means
  opening N Alertmanager UIs with the right `X-Scope-OrgID`.
- **The alert modal lies by omission.** `GetAlertDetails` sets
  `details.Silences = []` with a `// Note: Silences would need to be implemented`
  comment (`dashboard_handlers.go:1297`), so a silenced alert shows opaque
  `silencedBy` IDs and nothing else.

## Goals

- A `/silences` page, reachable from the nav for any logged-in user, listing
  silences from every configured Alertmanager with a **source** column.
- Per row: matchers, creator, comment, state (`active` / `pending` / `expired`),
  absolute end time **and** a relative countdown, with an *expiring soon* flag
  under 1h.
- Default view = active + pending, sorted soonest-expiry-first; expired hidden
  until the state filter includes them. Free-text filter over
  matchers/creator/comment plus a source filter.
- Per active silence: how many cached alerts it currently matches; zero-match
  silences flagged as *suppressing nothing*.
- Per-row actions **Extend +1h / +4h / +24h** (same silence ID, new end time)
  and **Expire now**, applied to the correct Alertmanager only.
- One unreachable Alertmanager degrades that source by name, not the page.
- `details.Silences` populated in `GetAlertDetails`, and the alert modal renders
  creator / comment / end time + countdown with the same Extend/Expire actions.

## Non-goals

- Creating silences from this page (creation stays in the dashboard flow; see
  #27 for matcher editing, #25 for duration limits — neither is touched).
- Notifying before a silence expires (needs a scheduler; natural follow-up).
- Persisting silence history in the backend DB or attributing silences to
  Notificator users beyond Alertmanager's `createdBy`.
- Merging the N per-Alertmanager copies into one logical silence — they are
  listed with their source.
- Editing matchers of an existing silence.
- No `proto/` change, no RPC, no DB migration.

## Approach

Silences stay on the "straight to Alertmanager" side of the two mental models in
`openwiki/dashboard.md#alert-actions`. Everything needed already exists as dead
code; this issue is mostly wiring.

### 1. Alertmanager layer — `internal/alertmanager/client.go`

`MultiClient.FetchAllSilences()` (line 727) and `SilenceWithSource` (line 666)
exist with zero callers. Add, mirroring `FetchAllAlertsDetailed()` (line 674):

```go
func (mc *MultiClient) FetchAllSilencesDetailed() ([]SilenceWithSource, map[string]error)
```

and reimplement `FetchAllSilences()` on top of it (same collapse rule: error
only when every source failed).

Extend and expire reuse what is there:

- **Extend** = `MultiClient.CreateSilenceOnAlertmanager(source, silence)` with
  the **same `ID`** and a new `EndsAt`. Alertmanager `POST /api/v2/silences`
  upserts on `id`, and `Client.CreateSilence` already marshals the whole
  `models.Silence` — a field set, not new HTTP code.
- **Expire** = `MultiClient.DeleteSilenceFromAlertmanager(source, id)`
  (line 793) — Alertmanager's DELETE expires rather than deletes.

Extend re-reads the current silence with
`MultiClient.FetchSilenceFromAlertmanager(source, id)` first, so `StartsAt`,
matchers, creator and comment are preserved verbatim (Alertmanager rejects a
changed `StartsAt` on an active silence). New end time is
`max(now, silence.EndsAt) + delta`, so extending a silence that is minutes from
expiry still yields a full window. Expired silences are not extendable —
Alertmanager would mint a new ID, which breaks the "silence ID is unchanged"
criterion; the handler rejects them with 409.

### 2. Matching helper

New `silenceMatchesAlert(silence models.Silence, labels map[string]string) bool`
in `internal/webui/handlers/silence_handlers.go`:

- every matcher must hold (AND);
- `IsRegex == false` → string equality, negated when `IsEqual == false`;
- `IsRegex == true` → Alertmanager semantics: full-string match, i.e. compile
  `^(?:` + value + `)$`, negated when `IsEqual == false`;
- a missing label is the empty string (so `foo!="bar"` matches an alert without
  `foo`, matching Alertmanager);
- an uncompilable regex makes the matcher (and thus the silence) not match —
  Alertmanager rejects such silences at creation, so this is defensive only.

Run over `alertCache.GetAllAlerts()` — the same in-memory source
`GetDashboardData` reads, so zero extra Alertmanager traffic. Count only alerts
whose `Source` equals the silence's source Alertmanager, since one logical
silence exists once per Alertmanager and each copy only suppresses its own.

### 3. Handlers — new `internal/webui/handlers/silence_handlers.go`

| Method | Path | Behaviour |
|---|---|---|
| `GET` | `/api/v1/dashboard/silences` | `FetchAllSilencesDetailed()`, enrich each silence with `source` + `matchedAlerts`, return `{ silences: [...], failedSources: {name: msg} }` |
| `POST` | `/api/v1/dashboard/silences/:id/extend` | body `{ "source": "...", "duration": "1h" }` (allowlist `1h`/`4h`/`24h`), fetch → bump `EndsAt` → re-create with same ID |
| `DELETE` | `/api/v1/dashboard/silences/:id` | query/body `source`, `DeleteSilenceFromAlertmanager` |

Both mutating endpoints require the source Alertmanager name and 400 on an
unknown one (`GetClientNames()` is the allowlist). All three are registered in
the existing `dashboard` group in `internal/webui/router.go` (which already has
`authMiddleware.RequireAuth()`), and the page route
`protectedPages.GET("/silences", handlers.SilencesPage)` goes next to
`/statistics` (line 358).

Response silence shape (JSON, extends the existing wire model with two fields):

```json
{
  "id": "…", "source": "prod-am", "createdBy": "alice",
  "comment": "incident 1234", "startsAt": "…", "endsAt": "…",
  "matchers": [{"name":"alertname","value":"KafkaLagHigh","isRegex":false,"isEqual":true}],
  "status": {"state": "active"}, "matchedAlerts": 3
}
```

State comes from Alertmanager (`status.state`); no client-side recomputation.
Timestamps go out as RFC3339 and the countdown / "expiring soon" flag is
computed in the browser, so it ticks without polling.

### 4. Alert modal

In `GetAlertDetails` (`dashboard_handlers.go:1297`), replace the empty-slice
stub: when `alert.Status.SilencedBy` is non-empty, fetch the alert's source
Alertmanager silences and keep those whose ID is in `SilencedBy`, mapped into
the existing (currently unused) `webuimodels.Silence`
(`internal/webui/models/dashboard.go:245`). Fetch failure → empty slice, as
today (the modal must not fail because Alertmanager is down). This block lives
outside the `backendClient != nil` guard it is currently nested in — silences
have nothing to do with the backend.

### 5. UI

- `internal/webui/templates/pages/Silences.templ` — page following the
  `StatisticsDashboard` page/handler shape (`PageNavigator`, `ProfileUser` page
  data), plus a self-contained Alpine component holding the fetched rows,
  filters, sort and the extend/expire calls. Filtering/sorting/countdown are
  client-side over one `GET /silences` payload; the mutations re-fetch.
- `internal/webui/templates/components/PageNavigator.templ` — third tab
  **Silences** (`activePage == "silences"`), same shape as the existing two; the
  dashboard and statistics pages keep their current `activePage` values.
- `internal/webui/templates/components/alert_modal_shared.templ` — a silences
  section rendering `alertDetails.silences` with creator, comment, end time +
  countdown and Extend/Expire.
- Regenerate with `make webui-templates`; never hand-edit `*_templ.go`.
  Keep `&&`/`<` out of inline `.templ` attribute JS (entity-escaping gotcha) —
  helpers live in the Alpine component.

### Files touched

- `internal/alertmanager/client.go` — `FetchAllSilencesDetailed`, rewire
  `FetchAllSilences`.
- `internal/webui/handlers/silence_handlers.go` — new: 3 API handlers,
  `SilencesPage`, `silenceMatchesAlert`.
- `internal/webui/handlers/silence_handlers_test.go` — new.
- `internal/webui/handlers/dashboard_handlers.go` — fill `details.Silences`.
- `internal/webui/router.go` — 3 API routes + `/silences` page route.
- `internal/webui/templates/pages/Silences.templ` — new.
- `internal/webui/templates/components/PageNavigator.templ` — nav tab.
- `internal/webui/templates/components/alert_modal_shared.templ` — modal section.
- Generated `*_templ.go` via `make webui-templates`.

## Risks & trade-offs

- **Extend semantics.** Upsert-on-ID is Alertmanager behaviour, not a documented
  contract of every fork; Mimir/Cortex ruler-backed Alertmanagers behave the
  same but should be checked manually. If a target returns a *new* ID, the
  handler surfaces it as an error rather than silently orphaning the old
  silence — the response ID is compared to the requested one.
- **Matched-alert count is an approximation.** It is computed against the
  in-memory alert cache (refresh-interval stale, resolved alerts absent), so it
  answers "is this silence suppressing anything right now?", not "has it ever".
  *Suppressing nothing* is a cleanup hint, not proof — worth saying so in the
  tooltip.
- **Regex dialect.** Go RE2 vs Alertmanager's (also RE2) match, but the
  full-string anchoring is easy to get wrong; that is exactly what the unit test
  covers.
- **N copies stay N rows.** Deliberate (see non-goals): a user expiring "the"
  silence has to expire it on each source. The source column and the identical
  comment make the copies visually obvious.
- **Fan-out cost.** The page issues one `GET /api/v2/silences` per Alertmanager
  per page load (sequentially, like `FetchAllAlertsDetailed`). Fine for a
  handful of instances; if it ever hurts, the silences deserve the same
  background-cache treatment alerts already get. No auto-refresh timer for now —
  a manual refresh button keeps the traffic user-driven.
- **Permissions.** Any logged-in user can expire any silence, matching today's
  silence/unsilence actions. Out of scope to change here, but worth stating.

## Validation

- `go test ./internal/webui/handlers/...` — new table test for
  `silenceMatchesAlert`: equal match, equal non-match, `isEqual=false`
  (negation), regex match/non-match, regex anchoring (`web` must not match
  `webserver`), missing label, multi-matcher AND (one failing matcher ⇒ no
  match).
- `make webui-templates && go build ./...` passes; `git status` shows no
  hand-edited `*_templ.go`.
- Manual via `make test` (docker-compose stack), with ≥2 Alertmanagers:
  - `/silences` lists silences from both sources with the source column;
    countdown ticks; a <1h silence is flagged.
  - Default view hides expired; enabling the expired filter shows them.
  - Silence a firing alert from the dashboard → its row shows a non-zero
    matched count; a silence matching nothing shows *suppressing nothing*.
  - Extend +1h → end time moves, ID unchanged (cross-check in the Alertmanager
    UI); Expire now → row leaves the active view, other Alertmanager untouched.
  - Stop one Alertmanager → the page still lists the other and names the failing
    source; the table is not empty and there is no blanket error.
  - Open a silenced alert's modal → silences listed with creator, comment, end
    time; Extend/Expire work from there too.
