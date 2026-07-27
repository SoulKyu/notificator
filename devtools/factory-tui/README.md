# 🏭 Factory TUI — live view of the autonomous dev loop

> **⚠️ Development tooling — NOT part of the notificator product.**
> Everything under `devtools/` supports the development *process* of this repo
> (the autonomous agent loop), never ships to users, and has no impact on builds
> or releases.

## ⭐ factory.py — the modern control room (Textual)

The main dashboard: a [Textual](https://textual.textualize.io/) app with real
navigation, five pages, and the human actions built in. Single file, PEP 723
inline dependencies — `uv` handles everything:

```bash
uv run devtools/factory-tui/factory.py           # the app (or ./factory.py)
uv run devtools/factory-tui/factory.py --check   # headless smoke test, exit 0/1
```

| Page | Key | Content |
|---|---|---|
| 🏭 Usine | `1` | stats bar, one live card per agent (state-colored border, current loops, next wake-up, 📬 badge), 🚨 alarms, 🙋 waiting-on-you queue, 🏆 24h scoreboard. **Enter/click on a card → full-screen live view of what that agent is doing right now** (its transcript streaming, à la Claude Code; Esc back) |
| 🚢 Pipeline | `2` | PR + issue tables with colored label chips; the detail pane explains every label of the selected item and spells out in red **exactly what YOU must do** when one is blocking |
| 🔄 Loops | `3` | `looper ps` live table + the selected loop's agent log (falls back to the loop's last run — the log you need when it's parked on `manual_intervention`). Parked loops show their park **reason** (last run error from looper's db) and `u` **unblocks** them: sweeps untracked `.review*.json` scratch files from the loop's worktree (the usual "worktree dirty" cause) then `looper retry`, behind a confirm showing the reason |
| 💬 Intercom | `4` | full inter-agent event feed (`events.jsonl`), pending inboxes, 📻 log ticker |
| 🏷 Labels | `5` | the label legend: blocking labels with their required human action, then the automatic pipeline labels |

Actions (footer keys): `m` merge a `ready-to-merge` PR (squash, with confirm),
`h` lift a `looper:hold` (confirm), `o` open the selection on GitHub,
`u` unblock a parked loop (scratch sweep + retry, with confirm),
`s` summon an agent (modal), `r` refresh, `q` quit, `Ctrl-P` command palette.
Every write goes through the same paths as the agents (`gh`, `summon.sh`) and
always behind an explicit confirm.

It reuses the data core of `factory-tui.py` below (pollers, alarms, legend) —
same sources, same intervals, same graceful degradation.

## factory-tui.py — the gamified office (stdlib curses, zero deps)

A zero-dependency terminal dashboard (Python stdlib `curses`) showing the agent
"office" in real time, top-down 2D style: who is working, who is on a coffee
break, who is asleep until their next timer, and what is on the team board
(open PRs, issue counts, live log chatter).

```
┌─ NOTIFICATOR DEV FACTORY ──────────────────────── 14:32:07 ─┐
│ ┌──────────┐ ┌──────────┐ ┌──────────┐                      │
│ │🔍 SCOUT  │ │🔥 ROAST  │ │🧭 COORD  │        ...           │
│ │⌨ tak··   │ │zZ dort   │ │☕~ pause │                      │
│ └──audit───┘ └──15min───┘ └─poll 30s─┘                      │
│ ── TABLEAU ────────────────────────────────────────────────  │
│ PR#43 👀review  PR#44 🧪qa                                   │
│ 📻 [rebase-43] go build ./... passes                         │
└──────────────────────────────────────────────────────────────┘
```

## Run

```bash
python3 devtools/factory-tui/factory-tui.py          # live TUI (q to quit)
python3 devtools/factory-tui/factory-tui.py --once   # one frame to stdout (tests/CI)
python3 devtools/factory-tui/factory-tui.py --check  # alignment self-check, exit 0/1
```

## Control room (keys)

| Key | Action |
|---|---|
| arrows | select a desk (double-line border) |
| Enter | zoom panel: state, last run result + start time, next wake-up, live tail (last 15 lines) of the agent's newest log |
| `l` (in zoom) | toggle follow mode for the log tail (live ↔ frozen) |
| `s` (in zoom) | prompt a one-line message and send it via `~/.claude-agents/notificator/summon.sh <agent> "<msg>"` — summonable agents only (scout, roast, qa, rebaser, groomer); shown as `(indispo)` elsewhere |
| Esc | close the zoom (quits from the office view) |
| `q` | quit, always |

Summoning is the single write path (delegated to `summon.sh`); everything else
stays read-only.

## Data sources (all read-only, polled)

| Source | What it feeds | Interval |
|---|---|---|
| `looper ps --json` | looper roles: coordinator, planner, reviewer, fixer, worker — a role running N>1 loops concurrently gets N desks (`🚢 WORKER·1`, `🚢 WORKER·2`…), each with its own target and step. The grid is capped by terminal height so it never hides the panels below it; desks that don't fit fold into the last desk of their role (`🚢 WORKER·2+3`) | 3 s |
| `systemctl --user` (services + timers `notificator-*`) | custom agents: scout, roast, qa, rebaser, promoter, groomer, doc, reporter — running / next wake-up / failure | 3–10 s |
| `gh pr list` / `gh issue list` | the team board | 45 s |
| `gh pr list` / `gh issue list` (last-24h search, one batched query set) | the 🏆 SCOREBOARD panel: per-agent stats (scout issues/approved, roast verdicts/kills, worker PRs/merged, qa pass/fail), hourly activity sparkline, ⭐ employé du jour — hidden when there is no data, "(github injoignable)" when GitHub is down | 45 s |
| newest file in the agents log dir | the 📻 chatter ticker | 10 s |
| agent inboxes (`inbox/<agent>/`, `inbox/archive/`) | 📬 pending-mail badge on desks + the 💬 INTERCOM panel (last agent-to-agent messages) | 10 s |

## Animated events

Observable transitions feed a render-side event queue (no extra pollers):

- **✉ mail in flight** — a new file in `inbox/<agent>/` sends an envelope flying
  from the sender's desk (parsed from the message `From:` header) to the
  recipient's desk over ~1 s; unknown senders launch from the team board
- **🎉 merge party** — a PR that disappears from `gh pr list` and turns out
  `MERGED` (one `gh pr view` check) throws a full-width celebration banner
  naming the PR for ~3 s
- **🚨 alarm board** — breakage accumulates in a panel above the 📌 TABLEAU
  instead of scrolling past: an unreachable looper daemon (`looper ps --json`
  unparseable, or parseable but of an unknown shape — every desk would
  otherwise render a calm `veille`; two consecutive failed polls are needed, a
  single hiccup against a self-restarting daemon is noise, not a signal), a
  `notificator-*` unit whose `Result` is not
  `success` (name, result, exit code — or signal number when `ExecMainCode` says
  the unit was killed — age since `ExecMainExitTimestamp`), a loop parked on
  `manual_intervention` (the one alarm where you are the blocker) and a
  `running` looper loop stuck on the same step past `FACTORY_STALL_MIN`
  (default 30 min), aged from `looper ps --json`'s `agent.startedAt` so a
  restarted TUI does not reset the clock (a `queued` loop has no clock — waiting
  for a free slot is not stalling). Rows clear on the unit's next
  successful run, when a human unblocks the loop, or when the step moves on; no
  alarm → no panel. The panel is
  capped at 5 rows plus a `… +N autres alarmes (2 unités en échec · 1 loop
  bloquée)` tail so a machine-wide breakage cannot push the 📌 TABLEAU
  off-screen nor silently swallow a whole alarm category. Each *new*
  alarm rings one
  `curses.beep()` and flashes the panel title for ~3 s (live TUI only —
  `--once` and `--check` never beep)
- **☕ coffee corner** — when the terminal leaves enough spare width, a coffee
  machine is drawn beside the desks; agents on break queue there and their desk
  shows an empty chair (narrow terminals fall back to the plain desk rendering)

## Configuration (env)

| Variable | Default | Purpose |
|---|---|---|
| `FACTORY_REPO` | `SoulKyu/notificator` | GitHub repo for the board |
| `FACTORY_LOG_DIR` | `~/.claude-agents/notificator/logs` | agent logs to feed the ticker |
| `FACTORY_LOOPER_LOG_DIR` | `~/.looper/logs/loops` | looper run logs (`<loopId>/<runId>/*.stdout.log`) for the zoom tail of a looper desk |
| `FACTORY_INBOX_DIR` | `~/.claude-agents/notificator/inbox` | agent mailboxes for 📬 badges + 💬 INTERCOM |
| `FACTORY_STALL_MIN` | `30` | minutes on the same looper step before a 🚨 stall alarm |

## Requirements

- Python ≥ 3.8 (stdlib only), a UTF-8 terminal
- `looper`, `gh` (authenticated) and the systemd user timers of the agent loop —
  missing sources degrade gracefully (desks show "?" instead of crashing)

## For agents improving these files

`factory-tui.py`: keep it **stdlib-only** and **read-only** (it must never mutate
GitHub, looper state, or files outside its own process). Preserve the `--once`
mode — it is the testable path (`python3 factory-tui.py --once` must always print
a frame and exit 0). Emoji are double-width: any new cell rendering must go
through `dpad()`, and `--check` must stay green — it asserts the alignment
invariants (11-col monitor segment in every state, all frame rows at identical
display width). It is also the **data core imported by `factory.py`** (pollers,
alarms, legend): renaming or reshaping those functions breaks the Textual app.

`factory.py`: writes are allowed but only the deliberate human actions (merge,
un-hold, summon) and always behind a confirm modal. Keep the PEP 723 header
working (`uv run factory.py` with no other setup) and `--check` green.
