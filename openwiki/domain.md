# Domain concepts

The vocabulary of Notificator. Each concept has one canonical definition here; other pages link
to it rather than redefining.

## Alert

An `Alert` (`internal/models/alert.go`) mirrors an Alertmanager alert: `Labels`, `Annotations`,
`StartsAt`, `EndsAt`, `GeneratorURL`, `Status`, and `Source` (which Alertmanager it came from).
Convenience getters read conventional labels/annotations with safe defaults:

- `GetAlertName()` ← label `alertname` (default `"Unknown"`)
- `GetSeverity()` ← label `severity` (default `"unknown"`)
- `GetTeam()` ← label `team` (default `"unknown"`)
- `GetInstance()` ← label `instance`
- `GetSummary()` ← annotation `summary`

### Fingerprint (alert identity)

`GetFingerprint()` is an **MD5 hash of the alert's labels sorted `key=value` and joined**. This
is the stable identity used everywhere — as the key of the alert cache, the join key for
comments/acks, and the `fingerprint` field in statistics. Because it is derived purely from
labels, the same logical alert has the same fingerprint across different Alertmanagers.

> The WebUI normalizes some upstream values before fingerprinting/coloring
> (`suppressed`→`silenced`, `information`→`info`; see [webui](webui.md#alert-cache)) so
> identity and severity stay consistent regardless of Alertmanager version.

## Status: firing / silenced / inhibited / resolved

`AlertStatus{State, SilencedBy, InhibitedBy}`. A **silence** (`internal/models/silence.go`,
`Silence{Matchers, StartsAt, EndsAt, CreatedBy, Comment, Status}`) suppresses matching alerts;
`IsSilenced()` is true when the state is `silenced` or `SilencedBy` is non-empty. An alert is
**inhibited** when another alert suppresses it (`InhibitedBy`). **Resolved** means it stopped
firing — at which point the WebUI archives it (see resolved alerts below).

## Filtering

`internal/filters/filter.go` — `AlertFilter{SearchText, Severity, Status, Team}` with
case-insensitive substring matching over alertname/summary/team/instance. This is the simple
server-side filter; the dashboard also has rich client-side filtering in the `.templ` scripts.

## Collaboration state (persisted by the backend)

These live in the backend DB and are edited over gRPC (`proto/alert.proto`, `AlertService`):

- **Comment** — free-text note on an alert (by fingerprint/alert_key), authored by a user.
- **Acknowledgment** — a user claiming/owning an alert, with an optional reason. `AlertUpdate`
  events (comment/ack added/deleted) are broadcast to subscribers in real time.
- **Resolved alert** — a snapshot of a resolved alert plus its comments/acks, JSON-serialized,
  with a **TTL** (`ExpiresAt`, default 24h). Kept so the team can review recently-resolved
  incidents; auto-purged after expiry. (Note: the newer statistics system, below, is the
  primary historical store; this is the short-lived "recently resolved with context" view.)
- **Hidden alert / hidden rule** — a user (or admin-impersonated user) can hide a specific alert
  (by fingerprint) or define a **hidden rule** (label key/value, optional regex, priority) to
  hide matching alerts from their view.
- **Filter preset** — a saved, optionally shared/default filter configuration (JSON `filter_data`).
- **User color preference** — rule-based alert coloring: `label_conditions` → color, with a
  `priority` for conflict resolution and lightness/darkness factors.
- **Annotation button config** — configurable per-user buttons that surface specific annotation
  keys on an alert.
- **Notification preference** & **column preferences** — per-user browser/sound notification
  toggles and dashboard column layout.

All of these accept an optional `impersonate_user_id` so an admin can manage another user's data
(see [webui](webui.md#auth)).

## Statistics & on-call analytics {#statistics}

The `StatisticsService` (`proto/alert.proto`) turns alert lifecycle events into metrics. Each
captured `AlertStatistic` records `FiredAt`, `AcknowledgedAt`, `ResolvedAt`, plus derived
durations:

- **MTTR** (Mean Time To Resolve) = `resolved − fired`
- **MTTA** (Mean Time To Acknowledge) = `acknowledged − fired`
- **Fix Time** = `resolved − acknowledged`

`QueryStatistics` aggregates these over a time range with optional severity/team filters,
grouped by `severity | team | alert_name | period(hour/day/week/month) | none`. Averages are
`NULLIF(x,0)`-guarded so zero-second results don't drag the mean down.

### On-call rules & time-of-day filtering

An **on-call rule** (`RuleConfig{criteria, logic}` with `RuleCriterion` on severity/label/
alert_name and operators like `equals`/`in`/`regex`) selects which alerts count as on-call load.
Separately, **time-of-day filtering** counts an alert if its **active interval
`[fired_at, resolved_at]` overlaps** the configured window (e.g. 18:00–08:00, cross-midnight
supported), with `weekend_mode` = `exclude | same_hours | full_weekends`.

> The overlap semantics were a bug fix (commit `4ed9ce0`): a daytime-fired alert that runs into
> the night window **is** on-call load and must be counted. The reference implementation and its
> intended behavior live in `internal/backend/services/statistics_query.go` and the regression
> test `statistics_oncall_test.go`. See [backend](backend.md#statistics-engine).

### Statistics views

A **statistics view** (`StatisticsViewData`) is a saved, optionally shared analytics
configuration: date range (relative or absolute), time-of-day window, grouping, filters, and
top-N limit — the analytics-dashboard equivalent of a filter preset.
