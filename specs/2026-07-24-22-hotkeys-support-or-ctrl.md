# Spec: hotkeys — `/` or `Ctrl+F` to focus dashboard search

- Issue: [SoulKyu/notificator#22](https://github.com/SoulKyu/notificator/issues/22)
- Date: 2026-07-24
- Status: planned

## Problem

The webui dashboard has a search input (`#dashboard-search` in
`internal/webui/templates/pages/NewDashboard.templ`) that can only be reached
with the mouse. Users expect the common keyboard shortcuts — `/` (GitHub-style)
and `Ctrl+F` / `Cmd+F` — to jump straight to it.

## Goals

- Pressing `/` anywhere on the dashboard page focuses the alerts search input.
- Pressing `Ctrl+F` (or `Cmd+F` on macOS) focuses the same input and prevents
  the browser's native find bar from opening.
- The `/` shortcut is inert while the user is typing in any input, textarea,
  select, or contenteditable element, and while a modal is open, so normal
  typing is never hijacked.

## Non-goals

- No global shortcut system, shortcut-help overlay, or configurable bindings.
- No shortcuts on other pages (statistics, playground) — the issue targets
  search, which lives on the main dashboard.
- No `Escape`-to-clear behavior (can be a follow-up if requested).

## Approach

The dashboard is Alpine.js-driven, and window-level key handling already uses
Alpine modifiers (`@keydown.escape.window`, `@keydown.ctrl.enter`, …). Reuse
that pattern — no new dependency, no new JS file.

On the dashboard's Alpine root element in
`internal/webui/templates/pages/NewDashboard.templ`, add:

```html
@keydown.slash.window="focusSearch($event)"
@keydown.ctrl.f.window.prevent="focusSearch($event)"
@keydown.meta.f.window.prevent="focusSearch($event)"
```

and a small helper in the dashboard Alpine component
(`internal/webui/templates/scripts/dashboard_core.templ`):

```js
focusSearch(event) {
    // '/' must not fire while typing elsewhere; Ctrl/Cmd+F always wins.
    const t = event.target;
    if (event.key === '/' &&
        (t.closest('input, textarea, select, [contenteditable]'))) {
        return;
    }
    event.preventDefault();
    document.getElementById('dashboard-search')?.focus();
}
```

After editing the `.templ` files, regenerate with `make webui-templates`
(never edit `*_templ.go` by hand).

### Files touched

- `internal/webui/templates/pages/NewDashboard.templ` — bind the three
  window-level keydown handlers on the dashboard root.
- `internal/webui/templates/scripts/dashboard_core.templ` — add `focusSearch`.
- Generated `*_templ.go` files via `make webui-templates` (not hand-edited).

## Risks & trade-offs

- **Hijacking `Ctrl+F`** removes access to the browser's native find on the
  dashboard. This is what the issue asks for and matches common dashboards,
  but it is a deliberate trade-off; users can still use the browser menu.
  If it proves annoying we can drop the `prevent` and keep only `/`.
- **`/` while typing**: guarded by the target check above; dropdown filter
  inputs (team/severity search) keep normal typing.
- **Modals**: window-level listeners still fire with a modal open. The
  ack/silence textareas are covered by the input guard; `Ctrl+F` focusing a
  background input behind a modal is a minor cosmetic edge, acceptable.
- Templ entity-escaping gotcha: keep the helper in the Alpine component script
  (`dashboard_core.templ`), not inline in attribute JS, to avoid `&&`
  escaping issues in `.templ` attributes.

## Validation

- `make webui-templates && go build ./...` passes.
- Manual check via `make test` (docker-compose stack):
  - `/` on the dashboard focuses the search box; typing then filters alerts.
  - `/` pressed inside the team-filter search box types a literal `/`.
  - `Ctrl+F` (and `Cmd+F` on macOS) focuses the search box, no native find bar.
  - `Ctrl+F` inside a textarea (ack comment) still focuses dashboard search
    without inserting text.
