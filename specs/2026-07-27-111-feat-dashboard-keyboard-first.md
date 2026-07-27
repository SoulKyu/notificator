# Dashboard keyboard-first triage

Issue: SoulKyu/notificator#111

## Problem

`/dashboard` triage is pointer-only. The only keyboard affordance is `/` /
`Ctrl-F`/`Cmd-F` to focus search (`@keydown.slash.window="focusSearch($event)"` /
`@keydown.ctrl.f.window.prevent` in `internal/webui/templates/pages/NewDashboard.templ:15-17`,
guard implemented in `focusSearch()` at `internal/webui/templates/scripts/dashboard_core.templ:160`).
Selecting a row, acknowledging, silencing, commenting, and opening the detail modal all require a
mouse. On a 40-alert page at 3am that's a mouse round-trip per action, per alert — the cost lands at
the worst possible time (time-to-acknowledge during an incident).

The state and actions this needs already exist:
- `selectedAlerts` / `selectedGroups` drive every bulk action
  (`internal/webui/templates/scripts/dashboard_actions.templ`), all funneled through
  `POST /api/v1/dashboard/bulk-action`.
- `toggleAlert(fingerprint)`, `selectAll()`, `clearSelection()` already manage selection
  (`internal/webui/templates/scripts/dashboard_utilities.templ:123-152`).
- `acknowledgeAlert(fingerprint)` / `acknowledgeSelected()` open the ack modal;
  `silenceAlert(fingerprint)` / `silenceSelected()` open the silence modal
  (`internal/webui/templates/scripts/dashboard_actions.templ:301-329,738-777`).
- `showAlertDetails(fingerprint, options)` opens the detail modal
  (`internal/webui/templates/scripts/dashboard_modal.templ:52`).
- `hideSelected()` hides everything in `selectedAlerts` globally via
  `POST /api/v1/dashboard/hidden-alerts`; `hideAlertInFilter(fingerprint)` hides one alert inside
  the active filter preset only (`internal/webui/templates/scripts/dashboard_actions.templ:157-242,279-299`).

None of this needs a new endpoint. What's missing is a keyboard path to it.

## Goals

- `j`/`k` move a visible row cursor over the alert table; `Enter`/`o` open the cursor row's detail
  modal.
- `x` toggles the cursor row's selection (same list `toggleAlert()` already drives); `Shift+X`
  selects/deselects every alert on the current page.
- `a` acknowledges — cursor row alone, or the current selection when non-empty.
- `s` opens the silence flow for the cursor row / selection.
- `c` opens the detail modal on the Comments tab with the comment input focused.
- `h` hides the cursor row.
- `Esc` clears the selection, or closes the top-most modal if one is open.
- `?` toggles a cheat-sheet overlay of every binding.
- None of this fires while a dashboard modal is open or while focus is in an
  `input`/`textarea`/`select`/`[contenteditable]` — reusing the exact guard `focusSearch()` already
  applies, not a second copy of it.
- Every action stays clickable; nothing existing is removed or rewired.

## Non-goals (explicitly out of scope)

- Remapping / user-configurable bindings.
- A command palette, multi-key chords, or `gg`/`G` jump grammar.
- Shortcuts on the statistics dashboard or the silences page.
- Any change to `/api/v1/dashboard/bulk-action`, `/api/v1/dashboard/hidden-alerts`, or what the
  actions themselves do.

## Approach

Client-side only, Alpine.js mixin, no proto/API change.

1. **New mixin file** `internal/webui/templates/scripts/dashboard_keyboard.templ`, following the
   `window.dashboard*Mixin` pattern (`dashboardActionsMixin`, `dashboardModalMixin`, ...). Merge it
   in `init()` next to the others (`Object.assign(this, window.dashboardKeyboardMixin)`,
   `internal/webui/templates/scripts/dashboard_core.templ` `init()`), and add the new templ to the
   script include list alongside the others in
   `internal/webui/templates/pages/NewDashboard.templ` (near line 986 where the other
   `@scripts.Dashboard*()` calls sit).

2. **State**: `cursorIndex` (int, default `0`) lives on the root component, next to
   `selectedAlerts`.

3. **Guard**: extract the modal-open / input-focus check already inline in `focusSearch()`
   (`dashboard_core.templ:160-169` — the `showSettings || showAckModal || showSilenceModal ||
   showAlertModal || showFilterPresetsModal || showColumnConfigModal` check, plus the
   `input, textarea, select, [contenteditable]` check) into a shared `shortcutsActive(event)`
   helper, extending the check with the new `showShortcutsHelp` state (step 7) so the cheat-sheet
   overlay blocks row shortcuts the same way every other modal does. `focusSearch()` and the new
   keyboard mixin both call it — one guard, not two copies that can drift.

4. **Bindings**: window-level `@keydown.*` on the outer `x-data` div in `NewDashboard.templ`, next
   to the existing `@keydown.slash.window`/`@keydown.ctrl.f.window.prevent`. Every row shortcut
   (`j`/`k`/`x`/`a`/`s`/`h`/`Enter`/`o`) is a no-op when `alerts.length === 0` (empty filter/search
   results) — checked before the handler body runs, alongside the `shortcutsActive()` guard. Each
   handler is otherwise a one-line call into `shortcutsActive()` + an existing mixin method:
   - `j`/`k` → `moveCursor(1)` / `moveCursor(-1)` (new, mixin-local: clamps to `[0, alerts.length-1]`,
     scrolls the row into view via `scrollIntoView({block: 'nearest'})`).
   - `Enter`/`o` → `showAlertDetails(this.alerts[this.cursorIndex].fingerprint)`.
   - `x` → `toggleAlert(this.alerts[this.cursorIndex].fingerprint)` (existing function — checkbox
     and keyboard stay in sync because they're the same call).
   - `Shift+X` → `selectedAlerts.length ? clearSelection() : selectAll()`.
   - `a` → `selectedAlerts.length || selectedGroups.length ? acknowledgeSelected() :
     acknowledgeAlert(cursorFingerprint)`. Both open the existing ack modal (reason still required,
     nothing auto-confirms) — the shortcut reaches the same confirmation step a click does, it does
     not skip it.
   - `s` → same selected-vs-cursor branch, calling `silenceSelected()` / `silenceAlert(...)`.
   - `c` → `await showAlertDetails(cursorFingerprint)`, then set `this.currentAlertTab = 'comments'`
     (`showAlertDetails` always resets it to `'overview'` first —
     `dashboard_modal.templ:52-56` — so the tab switch must happen after the await, not before),
     then focus the comment textarea on `$nextTick`.
   - `h` → hide the cursor row using the **global** path: set `selectedAlerts = [cursorFingerprint]`
     and call `hideSelected()`, then restore the prior selection. `hideAlertInFilter()` is
     filter-scoped and refuses (via a blocking `alert()`) when no filter preset is active, which
     would make `h` a no-op on the default view — reusing `hideSelected()` instead keeps the
     shortcut's behavior independent of filter state, matching "no silent no-op".
   - `Esc` → if `showShortcutsHelp`, close it; else if any modal is open, let the modal's own `Esc`
     handling run (existing behavior wins, per the issue); else `clearSelection()`.
   - `?` → toggle `showShortcutsHelp`.

5. **Cursor rendering**: `components/dynamic_alerts_table.templ` already iterates
   `x-for="(alert, index) in alerts"` (line 71) with `index` in scope — add a focus-ring class keyed
   on `index === cursorIndex`, next to the existing `selectedAlerts.includes(alert.fingerprint)`
   class binding (line 76). Reset `cursorIndex` to `0` whenever `alerts` is replaced wholesale
   (filter change, page change, sort change — the existing `loadDashboardData()` / SSE-merge paths);
   on an SSE incremental merge, keep `cursorIndex` if the alert at that index still has the same
   fingerprint, otherwise re-locate it by fingerprint if still present, otherwise reset to `0`.

6. **Grouped view**: `components/group_components.templ` renders groups, not individual alert rows
   — there's no 1:1 mapping from a group row to a single acknowledge/silence/hide target. Rather
   than guess a semantic, row shortcuts (`j`/`k`/`x`/`a`/`s`/`h`) — and `Shift+X`, since
   `selectAll()`/`clearSelection()` operate on `selectedAlerts`, not the `selectedGroups` that drive
   grouped-view selection — are inert while `viewMode === 'group'`, and `?` cheat-sheet shows a note
   that navigation is list-view-only. `c`, `Esc` and `?` still work in group view since they don't
   depend on a cursor row.

7. **Cheat-sheet overlay**: small templ component (e.g. `components/shortcuts_help.templ`) gated on
   `showShortcutsHelp`, listing the table above; included from `NewDashboard.templ` like the other
   modals.

8. Templ workflow: edit `.templ` sources only, run `make webui-templates`, never hand-edit
   `*_templ.go`.

## Risks / trade-offs

- **Guard drift**: the modal/input guard is duplicated today only inside `focusSearch()`. Factoring
  it out touches that function — low risk (pure extraction, same conditions), but it's the one
  piece of "existing" code this feature edits rather than just calling.
- **`h` behavior change**: reusing `hideSelected()` for a single cursor row means `h` always hides
  globally, never filter-scoped, even though a per-row filter-hide (`hideAlertInFilter`) exists
  elsewhere in the UI (`table_components.templ:120`). This is a deliberate choice (documented above)
  to avoid a silent no-op when no filter preset is active — flagging it so it isn't mistaken for an
  oversight during review.
- **`a`/`s` open a modal, they don't act**: acceptance criteria phrased as "acknowledges the cursor
  alert" actually means "opens the pre-targeted ack modal for the cursor alert" — consistent with
  today's click path, but worth calling out so validation doesn't expect a silent instant ack.
- **SSE cursor stability**: incremental merges can reorder/replace `alerts`; a naive reset-to-0 on
  every merge would yank the cursor away mid-triage. The fingerprint-based re-locate (step 5) adds a
  small amount of mixin logic — kept intentionally minimal (index check, then linear `findIndex`
  fallback) rather than a general diffing layer.
- **No new abstractions**: no shortcut-registry/config layer — bindings are a fixed, hard-coded set
  per the issue's explicit non-goal (no remapping).

## Validation

- Manual keyboard-only pass on `/dashboard` with ≥2 alerts loaded: `/` to search, `Esc` to leave
  search, `j`/`k` to move the cursor (visible ring, scrolls into view), `x` on two rows, `a` to
  acknowledge the selection, confirm in the modal — no pointer input used at any step.
- `a` with empty selection acks only the cursor row; with a non-empty selection it acks the
  selection and the cursor is ignored — verify via the request body sent to
  `POST /api/v1/dashboard/bulk-action` (`alertFingerprints`/`groupNames`).
- `x` toggling a row and clicking its checkbox both land in `selectedAlerts` — toggle one via
  keyboard, the other via click, confirm both reflect in the footer's "N selected" count
  (`dynamic_alerts_table.templ:100-102`).
- Open the ack modal, silence modal, and detail modal one at a time; confirm no row shortcut (`j`,
  `k`, `x`, `a`, `s`, `h`, `Enter`, `o`) does anything while each is open, and that typing in the
  search box or the comment textarea doesn't trigger any binding either.
- `?` opens the cheat-sheet; `Esc` closes it without affecting selection. While it's open, confirm
  `j`/`k`/`x`/`a`/`s`/`h`/`Enter`/`o` are all no-ops (the extended `shortcutsActive()` guard covers
  `showShortcutsHelp`).
- Switch to grouped view: confirm `j`/`k`/`x`/`a`/`s`/`h`/`Shift+X` are inert (not silently
  mismapped to groups) and that this is visible in the cheat-sheet, not just silent.
- Trigger an SSE update while the cursor sits mid-list: cursor stays on the same alert if it's still
  present, resets to row 0 if it was removed.
- Filter/search down to zero results: confirm `j`/`k`/`x`/`a`/`s`/`h`/`Enter`/`o` are all no-ops
  (no error thrown reaching into an empty `alerts` array).
- `make webui-templates` regenerates `*_templ.go` cleanly; `go build ./...` passes.
