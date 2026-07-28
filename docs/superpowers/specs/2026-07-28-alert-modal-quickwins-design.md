# Alert Modal Quick Wins — Copy Link & Frequency Sparkline

**Date:** 2026-07-28
**Status:** Approved (POCs validated by user; contextual-links and keyboard-nav POCs were reviewed and rejected for this phase)
**Scope:** Two frontend-only features on the alert detail modal. Zero backend changes.

## Feature 1 — Copy alert link

Add a share button to the alert modal header that copies the alert's deep link to the clipboard.

### Behavior

- New icon button (link glyph) in the modal header, positioned left of the close button, same styling pattern (`icon-btn`, rounded hover).
- Click copies `{window.location.origin}/dashboard/alert/{fingerprint}` — this route already exists and the modal already handles deep-linking (`internal/webui/templates/scripts/dashboard_modal.templ`, `pushAlertHistoryEntry` / `openAlertModal`).
- Clipboard write: `navigator.clipboard.writeText()` when `window.isSecureContext`, otherwise fallback via hidden `textarea` + `document.execCommand('copy')` (deployments on plain HTTP must still work).
- Feedback: reuse the existing copy-confirmation pattern from `AlertModalLabelsWithCopy` (`internal/webui/templates/components/alert_modal_shared.templ`) — brief toast/check state, auto-dismiss ~2s.

### Files touched

- `internal/webui/templates/components/alert_modal_shared.templ` (header shell — button)
- `internal/webui/templates/scripts/dashboard_modal.templ` (copy function)
- Regenerate with `make webui-templates`; never edit `*_templ.go`.

## Feature 2 — Frequency sparkline

Show a 30-day occurrence sparkline in the Overview tab so the on-call engineer immediately sees whether an alert is a one-off or a flapper.

### Behavior

- Data source: existing endpoint `GET /api/v1/dashboard/alert/:fingerprint/history` (`HandleGetAlertHistory`, `internal/webui/handlers/dashboard_handlers.go`), already fetched by `loadAlertHistory` in `dashboard_modal.templ`. No new endpoint.
- Frontend aggregates occurrences into a count-per-day array over the last 30 days.
- Rendering: inline SVG bar chart (30 bars) inside the Timeline card of the Overview tab:
  - bar height proportional to daily count, minimum 2px stub for zero days
  - today's bar highlighted (red), others blue, hover tooltip "Nd ago — X occurrences"
  - header line: "N occurrences in the last 30 days" + week-over-week trend (▲/▼ last 7d vs previous 7d)
- Known ceiling: the history endpoint caps at 50 entries. An alert firing >50×/30d displays "50+". Upgrade path: add a `limit`/`days` query param to the endpoint if this ever matters.

### Error handling

- History fetch failure or empty history → sparkline section hidden entirely; no error surfaced (the History tab already handles its own error state).
- Aggregation is defensive: entries without a parseable timestamp are skipped.

### Files touched

- `internal/webui/templates/components/alert_modal_shared.templ` (Timeline card — sparkline markup)
- `internal/webui/templates/scripts/dashboard_modal.templ` (aggregation + SVG generation off the already-fetched history)

## Out of scope (explicitly rejected for this phase)

- Contextual links row (Prometheus/Grafana from annotations) — POC rejected
- Keyboard navigation (←/→/a/s) — POC rejected

## Cross-cutting requirement

Dark mode was not shown in the POCs, but both features MUST style dark mode using the existing `dark:` utility classes already used throughout the modal.

## Testing

- `make webui-templates && go build ./...`
- Manual verification via `make test` (docker-compose rebuild): copy button on HTTP and HTTPS contexts, sparkline with a flapping alert and with a single-occurrence alert.

## POC references

Validated POCs (session scratchpad, not committed): `poc-1-copy-link.html`, `poc-4-sparkline.html`.
