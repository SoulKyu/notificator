# 🏭 Factory TUI — live view of the autonomous dev loop

> **⚠️ Development tooling — NOT part of the notificator product.**
> Everything under `devtools/` supports the development *process* of this repo
> (the autonomous agent loop), never ships to users, and has no impact on builds
> or releases.

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

## Data sources (all read-only, polled)

| Source | What it feeds | Interval |
|---|---|---|
| `looper ps` | looper roles: coordinator, planner, reviewer, fixer, worker | 3 s |
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
- **☕ coffee corner** — when the terminal leaves enough spare width, a coffee
  machine is drawn beside the desks; agents on break queue there and their desk
  shows an empty chair (narrow terminals fall back to the plain desk rendering)

## Configuration (env)

| Variable | Default | Purpose |
|---|---|---|
| `FACTORY_REPO` | `SoulKyu/notificator` | GitHub repo for the board |
| `FACTORY_LOG_DIR` | `~/.claude-agents/notificator/logs` | agent logs to feed the ticker |
| `FACTORY_INBOX_DIR` | `~/.claude-agents/notificator/inbox` | agent mailboxes for 📬 badges + 💬 INTERCOM |

## Requirements

- Python ≥ 3.8 (stdlib only), a UTF-8 terminal
- `looper`, `gh` (authenticated) and the systemd user timers of the agent loop —
  missing sources degrade gracefully (desks show "?" instead of crashing)

## For agents improving this file

Keep it **stdlib-only** and **read-only** (this dashboard must never mutate GitHub,
looper state, or files outside its own process). Preserve the `--once` mode — it is
the testable path (`python3 factory-tui.py --once` must always print a frame and
exit 0). Emoji are double-width: any new cell rendering must go through `dpad()`,
and `--check` must stay green — it asserts the alignment invariants (11-col monitor
segment in every state, all frame rows at identical display width).
