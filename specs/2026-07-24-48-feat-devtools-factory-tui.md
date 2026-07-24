# Spec: factory TUI control-room mode — navigate, zoom, summon

- Issue: [SoulKyu/notificator#48](https://github.com/SoulKyu/notificator/issues/48)
- Date: 2026-07-24
- Status: planned

## Problem

The factory TUI (`devtools/factory-tui/factory-tui.py`, merged with #44) is
display-only: the curses loop reads a single key (`q`/`Esc` to quit). Operating
the agent fleet — tailing an agent's log, checking why its last run failed,
summoning scout/roast/qa/rebaser/groomer — still requires a separate shell.
The TUI is the natural cockpit for the autonomous loop; without interaction it
stays a toy.

## Goals

- Arrow keys select a desk (visually highlighted border); `Enter` opens a zoom
  panel drawn over the office for any of the 13 desks; `Esc` closes it.
- The zoom panel shows: agent state, last run outcome + timestamp, next timer
  wake-up, and the last ~15 lines of that agent's newest log file.
- Inside zoom, `l` toggles follow mode (log tail refreshes every frame) and
  `s` prompts for a one-line message and sends it via
  `summon.sh <agent> "<msg>"` — only for the 5 summonable agents
  (scout, roast, qa, rebaser, groomer); other desks show `s` greyed/disabled.
- `q` keeps quitting globally; `Esc` no longer quits at office level (it is
  reserved for closing the zoom — see Risks); `--once` and `--check` keep
  their exact semantics (exit 0, non-interactive paths untouched).
- Still stdlib-only; the TUI stays read-only except writing summon messages
  through `summon.sh`.

## Non-goals

- No holds/labels management, no PR actions, no `looper` control commands —
  the only mutation is a summon message file, and only via `summon.sh`.
- No mouse support, no scrollback in the log tail beyond the ~15 lines.
- No multi-line summon composer — one `curses.textpad.Textbox` line.
- No new desks or data sources beyond two extra systemd properties (below).

## Approach

All changes live in `devtools/factory-tui/factory-tui.py`; the interactive
code is only reachable from `main_curses()`, so `--once`/`--check` never touch
it. `curses.wrapper` already enables `keypad(True)`, so arrows arrive as
`curses.KEY_UP/DOWN/LEFT/RIGHT`. Set `os.environ.setdefault("ESCDELAY", "25")`
before `curses.wrapper` so `Esc` closes the zoom without the default 1 s lag.

### Interaction state + key handling

Three locals in `main_curses()`: `sel` (roster index, starts 0), `zoom`
(bool), `follow` (bool). Replace the single `getch` check with a small
dispatcher:

- office mode: arrows move `sel` (left/right ±1, up/down ±`per_row`, clamped
  to `0..12`); `Enter` (`10`, `13`, `curses.KEY_ENTER`) opens zoom;
  `q` quits; a bare `Esc` (27) is ignored (it no longer quits — see Risks).
- zoom mode: `Esc` closes zoom, `l` toggles `follow`, `s` opens the summon
  prompt (summonable desks only), `q` still quits.

`per_row` uses the same formula as `render_frame()`
(`max(1, (width - 4) // (CELL_W + 3))`); extract it into a tiny helper so the
two can't drift.

### Desk highlight

`render_frame(tick, width, sel=None)` threads `sel` down to `desk_cell()`,
which swaps the border charset for the selected desk: `┌─┐│└┘` →
`╔═╗║╚╝` (all single display width, so `dwidth` invariants and `--check`
stay untouched). `--once` keeps calling `render_frame(2)` with the default
`sel=None` — output unchanged.

### Zoom panel

Drawn after the office rows in `main_curses()` (overlay via `scr.addstr` on
top, centered box ~70×22, clamped to the terminal). Content, all through
`dpad()` because log lines may contain emoji:

- header: emoji + name + kind
- state/status/detail from the existing `agent_state(key, kind)`
- last run: for `svc:` desks, systemd `Result` + `ExecMainStartTimestamp` /
  `ExecMainExitTimestamp` — added to the property list already fetched by
  `poll_fast()` (`-p Id,ActiveState,Result` grows two properties; no new
  subprocess call). For `looper:` desks show the current `looper ps`
  status/target from `STATE["loops"]`; for the virtual roast desk fall back
  to the scout unit. Timestamps are displayed as systemd returns them — no
  parsing.
- next wake-up: `STATE["timers"].get(TIMER_OF.get(key, ""), "")` (already
  polled) — the double fallback matches the existing footer code; the looper
  desks (coordinator, planner, worker, reviewer, fixer) have no `TIMER_OF`
  entry, so an empty/absent value renders as "no timer" instead of raising
  `KeyError`.
- log tail: newest file in `LOG_DIR` whose basename starts with the agent
  key (same naming the ticker relies on:
  `basename.rsplit("-", 1)[0]`); read the last ~8 KB with `seek`, keep the
  last 15 lines. Read on open, and re-read every frame while `follow` is on.
  No match → "no log". Roast has no own logs → reuse scout's.
- footer: key hints; `s summon` rendered with `A_DIM` on non-summonable
  desks.

The log-tail read happens in the UI loop (on-demand, ≤8 KB) — no new poller
thread, no new `STATE` keys beyond the two systemd properties.

### Summon

`SUMMON = os.environ.get("FACTORY_SUMMON", os.path.expanduser("~/.claude-agents/notificator/summon.sh"))`
next to the existing env config. On `s` (summonable desks only):

1. `curses.curs_set(1)`, `scr.nodelay(False)`, draw a one-line input strip in
   the zoom footer, `curses.textpad.Textbox(...).edit()`.
2. Empty message or `Esc`-aborted edit → cancel.
3. Otherwise `subprocess.run([SUMMON, key, msg], env={**os.environ, "SUMMON_FROM": "factory-tui"}, capture_output=True, text=True, timeout=10)`
   — an argv list, not `sh()`, so the message is never shell-interpreted.
4. Show the result line (stdout on success, stderr on failure) in the panel;
   restore `nodelay(True)` / `curs_set(0)`.

`summon.sh` itself validates the agent allowlist and writes
`inbox/<agent>/msg-<ts>.md`; the TUI mirrors the same 5-agent allowlist
(`scout roast qa rebaser groomer`) only to grey the key out. Textbox blocks
the render loop while typing — acceptable for a one-line message; the poller
threads keep updating `STATE` meanwhile.

### `--check` additions

The panel renderer takes its log-tail lines as a parameter (the interactive
path reads them from `LOG_DIR`; the check never touches the filesystem).
`selfcheck()` gains one loop: render the zoom panel lines for every desk in
every state, passing synthetic tail lines that cover the classic breakage —
`["ok", "🔥 échec du run 🧪", "日本語ログ行", "x" * 300]` (emoji, CJK,
overlong) — and assert each row has the panel's exact display width (same
`dwidth` discipline as the frame check), plus one frame render with
`sel=0` asserting rows are still 92 cols. Injecting the lines directly keeps
`--check` deterministic and environment-independent (CI has no `LOG_DIR`),
mirroring how the frame check already injects synthetic emoji state into
`STATE`. `--check` stays exit 0/1 with the same meaning.

### Files touched

- `devtools/factory-tui/factory-tui.py` — all of the above.
- `devtools/factory-tui/README.md` — document the keys
  (arrows/Enter/Esc/l/s), the `FACTORY_SUMMON` env var, and amend the
  read-only note ("read-only except summon messages via summon.sh").

## Risks & trade-offs

- **Esc vs. arrow keys**: `keypad(True)` only decodes an arrow's `ESC [ A`
  sequence when all its bytes arrive within `ESCDELAY`; on a laggy SSH link
  (the normal way one watches the factory host) a single arrow press can
  straddle the 25 ms window and be delivered as a bare `27`. That is why
  office-level `Esc`-quits is dropped (a behavior change from the current
  TUI): a mis-decoded arrow then at worst does nothing in office mode and at
  worst closes the panel in zoom — both recoverable — instead of terminating
  the whole session. Quit is reserved for `q`; `ESCDELAY=25` stays for a
  snappy zoom close.
- **Alignment regressions**: emoji in log lines and titles are the classic
  breakage — every zoom row goes through `dpad()` and `--check` now asserts
  the panel, so CI catches it.
- **Log-file heuristic**: prefix matching on the agent key can miss if log
  naming changes; the panel degrades to "no log" rather than crashing,
  matching the TUI's existing graceful-degradation stance.
- **Blocking textbox**: the UI freezes while composing a summon. Fine for one
  line; a non-blocking editor is not worth the complexity in dev tooling.
- **Security/trust boundary**: the message is passed as a single argv element
  and `summon.sh` quotes it into a file — no shell interpolation of user
  input. `SUMMON_FROM=factory-tui` keeps the mail header honest and the
  script's own depth guard intact.
- **Cost**: zero — two extra properties on an existing `systemctl show` call,
  one ≤8 KB file read per frame only while a zoom is open with follow on.

## Validation

- `python3 devtools/factory-tui/factory-tui.py --check` → `selfcheck: OK`,
  exit 0 (now also covering zoom-panel alignment).
- `python3 devtools/factory-tui/factory-tui.py --once` → prints a frame,
  exit 0, output identical to before (no selection highlight in `--once`).
- Manual, in a live session:
  - arrows walk all 13 desks including row wrap at the 5th/10th desk; the
    selected desk shows the double border.
  - `Enter` on each desk opens the zoom with state, last run + timestamp,
    next timer, and 15 log lines; `l` makes the tail advance live; `Esc`
    returns to the office; `q` quits from both modes.
  - `s` on scout sends a message: file appears in
    `~/.claude-agents/notificator/inbox/scout/`, panel shows the
    "message queued" confirmation, and the 📬 badge lights on the desk at the
    next inbox poll.
  - `s` on a looper desk (e.g. WORKER) is greyed and inert.
  - `grep -n "import" factory-tui.py` shows stdlib modules only.
