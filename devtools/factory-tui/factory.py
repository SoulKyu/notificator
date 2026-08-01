#!/usr/bin/env -S uv run --script --quiet
# /// script
# requires-python = ">=3.10"
# dependencies = ["textual>=1.0"]
# ///
"""FACTORY — modern control room for the notificator agent office (Textual).

Five pages, real navigation, live data. The gamified curses office lives on in
factory-tui.py (zero-dep fallback); this app reuses its battle-tested data core
(pollers, alarms, label legend) and adds a modern UI on top.

Run:      uv run devtools/factory-tui/factory.py      (or ./factory.py)
Test:     uv run devtools/factory-tui/factory.py --check
Pages:    1 Usine · 2 Pipeline · 3 Loops · 4 Intercom · 5 Labels
Actions:  m merge (PR ready-to-merge) · h lever un hold · o ouvrir sur GitHub
          u débloquer un loop parqué (sweep scratch + retry) · s summon un agent
          r rafraîchir · q quitter · Ctrl-P palette
"""
from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import sys
import threading
import time

from rich.text import Text
from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Grid, Horizontal, Vertical, VerticalScroll
from textual.message import Message
from textual.screen import ModalScreen, Screen
from textual.widgets import (Button, DataTable, Footer, Header, Input, Label,
                             RichLog, Select, Static, TabbedContent, TabPane)

# ── data core: reuse the tested pollers/alarms/legend from the curses TUI ────
HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("factory_core", os.path.join(HERE, "factory-tui.py"))
core = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(core)

REPO = core.REPO
SUMMON_TARGETS = sorted(core.SUMMONABLE | {"promoter"})

# GitHub-rich data owned by this app (the curses core only keeps compact strings)
DATA = {"prs": [], "issues": [], "score": None, "pending": [], "gh_ok": True,
        "events": [], "slow_at": 0.0}
DLOCK = threading.Lock()

STATE_FR = {"work": ("⚙", "travaille", "ok"), "break": ("☕", "pause", "dim"),
            "away": ("☕", "au café", "dim"), "sleep": ("💤", "dort", "dim"),
            "error": ("🔥", "ÉCHEC", "err"), "wait": ("⏳", "attend", "warn")}

C = {"ok": "#3fb950", "warn": "#d29922", "err": "#f85149", "info": "#58a6ff",
     "cyan": "#39c5cf", "dim": "#8b949e", "accent": "#f0b429"}

LABEL_STYLE = {  # exact-match label chips; prefixes handled in chip_style()
    "ready-to-merge": f"bold black on {C['ok']}",
    "looper:hold": f"bold black on {C['err']}",
    "qa:passed": C["ok"], "qa:failed": f"bold {C['err']}",
    "rebase:conflict": f"bold {C['err']}",
    "needs-info": C["warn"], "wontfix": C["warn"],
    "roast:approved": C["ok"], "roast:needs-work": C["warn"], "roast:rejected": C["err"],
    "agent:proposed": C["cyan"], "feature-proposal": C["cyan"], "groomed": C["cyan"],
}


def chip_style(name: str) -> str:
    if name in LABEL_STYLE:
        return LABEL_STYLE[name]
    if name.startswith("looper:"):
        return C["info"]
    return C["dim"]


def legend_of(name: str) -> tuple[str, str] | None:
    """(meaning, action) for a label; action != '' means BLOCKING (you act)."""
    for lbl, meaning, action in core.LABEL_BLOCKING:
        if name == lbl:
            return meaning, action
    for lbl, meaning in core.LABEL_PIPELINE:
        if name == lbl:
            return meaning, ""
    if any(name.startswith(p) for p in ("kind/", "area/", "complexity/", "dispatch/")) or name == "triaged":
        return "coordinator a trié et catégorisé", ""
    return None


def chips(labels: list[dict]) -> Text:
    t = Text()
    for l in sorted(labels, key=lambda l: l["name"]):
        if t:
            t.append(" ")
        t.append(f" {l['name']} ", style=chip_style(l["name"]))
    return t


def gh_poll():
    """Own slow poll: full PR/issue JSON (the core only keeps compact strings)."""
    now = time.time()
    ok = True
    out = core.sh(f"gh pr list -R {REPO} --state open --limit 100 "
                  "--json number,title,labels,mergeable,isDraft,updatedAt,headRefName,author 2>/dev/null", 30)
    try:
        prs = json.loads(out)
    except Exception:
        prs, ok = None, False
    out = core.sh(f"gh issue list -R {REPO} --state open --limit 200 "
                  "--json number,title,labels,updatedAt,author 2>/dev/null", 30)
    try:
        issues = json.loads(out)
    except Exception:
        issues, ok = None, False
    since = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 86400))
    i_out = core.sh(f"gh issue list -R {REPO} --state all --limit 200 "
                    f"--search 'created:>{since}' --json labels,createdAt 2>/dev/null", 30)
    p_out = core.sh(f"gh pr list -R {REPO} --state all --limit 200 "
                    f"--search 'updated:>{since}' --json labels,createdAt,mergedAt,headRefName 2>/dev/null", 30)
    try:
        score = core.compute_score(json.loads(i_out), json.loads(p_out), now)
    except Exception:
        score = "err"
    with DLOCK:
        if prs is not None:
            DATA["prs"] = prs
        if issues is not None:
            DATA["issues"] = issues
        DATA["score"], DATA["gh_ok"], DATA["slow_at"] = score, ok, now
        DATA["pending"] = core.compute_pending(DATA["prs"], DATA["issues"], now)


def loop_tail_any(lp: dict, n: int) -> tuple[str | None, list[str]]:
    """Current run's log, else the loop's newest past run — a loop parked on
    manual_intervention has no runId, and its LAST log is the one you need."""
    name, tail = core.loop_tail(lp, n)
    if tail or not lp.get("loopId"):
        return name, tail
    d = os.path.join(core.LOOPER_LOG_DIR, lp["loopId"])
    paths = [os.path.join(r, f) for r, _, fs in os.walk(d) for f in fs if f.endswith(".stdout.log")]
    return core.newest_tail(paths, n)


LOOPER_DB = os.environ.get("FACTORY_LOOPER_DB", os.path.expanduser("~/.looper/looper.sqlite"))


def loop_park_reason(loop_id: str) -> str:
    """Last run error of a loop (why it parked) — read-only sqlite lookup."""
    import sqlite3
    try:
        db = sqlite3.connect(f"file:{LOOPER_DB}?mode=ro", uri=True)
        row = db.execute("SELECT error_message FROM runs WHERE loop_id = ? "
                         "ORDER BY started_at DESC LIMIT 1", (loop_id,)).fetchone()
        db.close()
        return (row[0] or "") if row else ""
    except Exception:
        return ""


def sweep_scratch_files(worktree: str) -> list[str]:
    """Remove UNTRACKED .review*.json scratch files from a worktree (the
    reviewer's leftovers that cause 'worktree dirty' parks; naming varies:
    .review_payload.json, .review116.json…). git-status-driven, so a tracked
    file can never match. -> basenames removed."""
    import re as _re
    swept = []
    try:
        out = subprocess.run(["git", "-C", worktree, "status", "--porcelain"],
                             capture_output=True, text=True, timeout=15).stdout
        for line in out.splitlines():
            m = _re.match(r"^\?\? (\.review[^/]*\.json)$", line)
            if m:
                os.remove(os.path.join(worktree, m.group(1)))
                swept.append(m.group(1))
    except Exception:
        pass
    return swept


def role_last_run_tail(role: str, n: int) -> tuple[str | None, list[str]]:
    """Newest run log of a looper ROLE, active loop or not — 'what did it do
    last'. Read-only sqlite lookup; any failure degrades to (None, [])."""
    import sqlite3
    try:
        db = sqlite3.connect(f"file:{LOOPER_DB}?mode=ro", uri=True)
        row = db.execute(
            "SELECT r.loop_id, r.id FROM runs r JOIN loops l ON l.id = r.loop_id "
            "WHERE l.type = ? ORDER BY r.started_at DESC LIMIT 1", (role,)).fetchone()
        db.close()
        if not row:
            return None, []
        return core.loop_tail({"loopId": row[0], "runId": row[1]}, n)
    except Exception:
        return None, []


def read_events():
    """events.jsonl (summon.sh append-only log) -> parsed dicts, oldest first."""
    path = os.path.join(core.INBOX_DIR, "events.jsonl")
    out = []
    try:
        for ln in open(path, errors="replace").read().splitlines()[-300:]:
            try:
                out.append(json.loads(ln))
            except Exception:
                pass
    except OSError:
        pass
    with DLOCK:
        DATA["events"] = out


# ── widgets ──────────────────────────────────────────────────────────────────

class AgentCard(Static):
    """One roster agent: state-colored border, live status body.
    Enter/click opens the live work view (what the agent is doing right now)."""

    can_focus = True
    BINDINGS = [Binding("enter", "open", "Voir le travail", show=False)]

    class Open(Message):
        def __init__(self, card: "AgentCard"):
            self.card = card
            super().__init__()

    def __init__(self, key: str, emoji: str, name: str, kind: str):
        super().__init__("", classes="agent-card")
        self.key, self.emoji, self.agent_name, self.kind = key, emoji, name, kind
        self.border_title = f"{emoji} {name}"

    def action_open(self):
        self.post_message(self.Open(self))

    def on_click(self):
        self.post_message(self.Open(self))

    def _parallel(self, loops) -> int:
        """How many workers of this agent are busy right now: running loops for
        a looper role, live slot workdirs (/tmp/notificator-qa-<pr>-XXXX) for the
        parallel QA, 0 elsewhere (every other custom agent is flock-single)."""
        if self.kind.startswith("looper:"):
            return sum(1 for l in loops if l["status"] == "running")
        if self.key == "qa":
            try:
                return sum(1 for n in os.listdir("/tmp")
                           if n.startswith("notificator-qa-")
                           and os.path.isdir(os.path.join("/tmp", n))
                           and n.split("-")[2].isdigit())
            except OSError:
                return 0
        return 0

    def refresh_state(self):
        role = self.kind.split(":")[1] if self.kind.startswith("looper:") else None
        with core.LOCK:
            loops = [l for l in core.STATE["loops"] if role and l["type"] == role]
            mail = core.STATE["mail_pending"].get(self.key, 0)
            nxt = core.STATE["timers"].get(core.TIMER_OF.get(self.key, ""), "")
        state, status, detail = (core.loop_state(loops[0]) if loops
                                 else core.agent_state(self.key, self.kind))
        icon, word, tone = STATE_FR.get(state, ("·", state, "dim"))
        self.set_classes(f"agent-card st-{state}")
        par = self._parallel(loops)
        subs = ([f"⚡{par}"] if par else []) + ([f"📬{mail}"] if mail else [])
        self.border_subtitle = " ".join(subs)
        body = Text()
        body.append(f"{icon} {word}", style=f"bold {C[tone]}")
        if loops:
            for lp in sorted(loops, key=core.loop_order)[:2]:
                age = f" · {core.ago(time.time() - lp['since'], fine=True)}" if lp.get("since") else ""
                body.append(f"\n#{core.loop_num(lp)} {lp['step']}{age}", style=C["dim"])
            if len(loops) > 2:
                body.append(f"\n+{len(loops) - 2} autres loops", style=C["dim"])
        else:
            if detail and detail not in (status, nxt):
                body.append(f"\n{detail}", style=C["dim"])
            if nxt:
                body.append(f"\n⏰ {nxt}", style=C["dim"])
        self.update(body)


class AgentViewScreen(Screen):
    """Full-screen live view of one agent's work: its transcript streaming as it
    happens (looper run stdout, or the agent's newest log for systemd agents)."""

    BINDINGS = [Binding("escape", "app.pop_screen", "Retour"),
                Binding("q", "app.pop_screen", "Retour", show=False)]

    def __init__(self, key: str, emoji: str, name: str, kind: str):
        super().__init__()
        self.key, self.emoji, self.agent_name, self.kind = key, emoji, name, kind
        self._last: tuple = ()

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        yield Static(id="av-info", classes="panel")
        yield RichLog(id="av-log", wrap=True, highlight=False, markup=False)
        yield Footer()

    def on_mount(self):
        self.query_one("#av-info").border_title = f"{self.emoji} {self.agent_name} — en direct"
        self.refresh_view()
        self.set_interval(2, self.refresh_view)

    def refresh_view(self):
        role = self.kind.split(":")[1] if self.kind.startswith("looper:") else None
        with core.LOCK:
            loops = [l for l in core.STATE["loops"] if role and l["type"] == role]
            nxt = core.STATE["timers"].get(core.TIMER_OF.get(self.key, ""), "")
        loops.sort(key=core.loop_order)
        state, status, detail = (core.loop_state(loops[0]) if loops
                                 else core.agent_state(self.key, self.kind))
        icon, word, tone = STATE_FR.get(state, ("·", state, "dim"))
        # a running loop streams its own run log; otherwise the newest agent log
        lp = next((l for l in loops if l["status"] == "running"), loops[0] if loops else None)
        if lp:
            name, tail = loop_tail_any(lp, 300)
        elif role:  # looper role, currently idle → its most recent run
            name, tail = role_last_run_tail(role, 300)
        else:
            name, tail = core.log_tail(core.LOG_PREFIX_OF.get(self.key, self.key), 300)
        info = Text()
        info.append(f"{icon} {word}", style=f"bold {C[tone]}")
        info.append(f"  {status} {detail}".rstrip(), style=C["dim"])
        for l in loops:
            info.append(f"\n#{core.loop_num(l)} · {l['step']} · {l['status']}", style=C["info"])
        if nxt:
            info.append(f"\n⏰ prochain réveil : {nxt}", style=C["dim"])
        info.append(f"\n📜 {name or '(aucun log)'}", style=C["accent"])
        self.query_one("#av-info", Static).update(info)
        if (name, tuple(tail or ())) == self._last:
            return  # unchanged: no rewrite, keeps the scroll position stable
        self._last = (name, tuple(tail or ()))
        log = self.query_one("#av-log", RichLog)
        log.clear()
        for line in tail or ["(rien à montrer — l'agent n'a pas encore écrit de log)"]:
            log.write(line)


class ConfirmScreen(ModalScreen[bool]):
    BINDINGS = [Binding("escape", "cancel", "Annuler")]

    def __init__(self, question: str, ok_label: str = "Oui, go"):
        super().__init__()
        self.question, self.ok_label = question, ok_label

    def compose(self) -> ComposeResult:
        with Vertical(classes="modal-box"):
            yield Label(self.question, classes="modal-q")
            with Horizontal(classes="modal-btns"):
                yield Button(self.ok_label, variant="success", id="yes")
                yield Button("Annuler", id="no")

    @on(Button.Pressed)
    def _done(self, ev: Button.Pressed):
        self.dismiss(ev.button.id == "yes")

    def action_cancel(self):
        self.dismiss(False)


class SummonScreen(ModalScreen["tuple[str, str] | None"]):
    BINDINGS = [Binding("escape", "cancel", "Annuler")]

    def compose(self) -> ComposeResult:
        with Vertical(classes="modal-box"):
            yield Label("✉ Summon un agent", classes="modal-q")
            yield Select([(a, a) for a in SUMMON_TARGETS], value="scout",
                         allow_blank=False, id="summon-agent")
            yield Input(placeholder="message… (Entrée envoie)", id="summon-msg")
            with Horizontal(classes="modal-btns"):
                yield Button("Envoyer", variant="success", id="yes")
                yield Button("Annuler", id="no")

    def _send(self):
        msg = self.query_one("#summon-msg", Input).value.strip()
        agent = self.query_one("#summon-agent", Select).value
        self.dismiss((str(agent), msg) if msg else None)

    @on(Input.Submitted)
    def _submit(self, _):
        self._send()

    @on(Button.Pressed)
    def _btn(self, ev: Button.Pressed):
        self._send() if ev.button.id == "yes" else self.dismiss(None)

    def action_cancel(self):
        self.dismiss(None)


# ── the app ──────────────────────────────────────────────────────────────────

class FactoryApp(App):
    TITLE = "🏭 NOTIFICATOR DEV FACTORY"
    SUB_TITLE = "control room"
    BINDINGS = [
        Binding("q", "quit", "Quitter"),
        Binding("r", "refresh_all", "Rafraîchir"),
        Binding("s", "summon", "Summon"),
        Binding("m", "merge", "Merger"),
        Binding("h", "unhold", "Lever hold"),
        Binding("o", "open_web", "Ouvrir GH"),
        Binding("u", "unblock", "Débloquer"),
        Binding("1", "tab('tab-usine')", "Usine", show=False),
        Binding("2", "tab('tab-pipeline')", "Pipeline", show=False),
        Binding("3", "tab('tab-loops')", "Loops", show=False),
        Binding("4", "tab('tab-intercom')", "Intercom", show=False),
        Binding("5", "tab('tab-labels')", "Labels", show=False),
    ]
    CSS = """
    Screen { background: #0d1117; }
    Header { background: #161b22; color: #f0b429; }
    Footer { background: #161b22; }
    TabPane { padding: 1 2 0 2; }

    #statsbar { height: 3; border: round #30363d; background: #161b22; padding: 0 1;
                content-align: center middle; }
    #agent-grid { layout: grid; grid-size: 4; grid-gutter: 0 1; height: auto; margin: 1 0; }
    .agent-card { height: 6; border: round #30363d; background: #161b22; padding: 0 1;
                  border-title-color: #e6edf3; border-subtitle-color: #f0b429; }
    .agent-card.st-work { border: round #3fb950; }
    .agent-card.st-error { border: round #f85149; background: #2d1416; }
    .agent-card.st-wait { border: round #d29922; }
    .agent-card.st-sleep, .agent-card.st-break { color: #8b949e; }
    .agent-card:focus { border: double #f0b429; }

    AgentViewScreen { background: #0d1117; }
    #av-info { margin: 1 2 0 2; }
    #av-log { border: round #30363d; background: #10151c; height: 1fr; margin: 0 2 1 2;
              padding: 0 1; }

    .panel { border: round #30363d; background: #161b22; padding: 0 1; height: auto;
             margin-bottom: 1; border-title-color: #e6edf3; }
    #alarms-panel.alert { border: round #f85149; border-title-color: #f85149; }

    DataTable { background: #0d1117; height: 1fr; margin-bottom: 1;
                border: round #30363d; border-title-color: #f0b429; }
    DataTable > .datatable--header { background: #161b22; color: #f0b429; }
    #detail-panel { height: auto; max-height: 14; }
    #loop-table { height: 40%; }
    #loop-log, #events-log { border: round #30363d; background: #10151c; height: 1fr;
                             padding: 0 1; margin-bottom: 1; }
    #labels-body { padding: 0 1; }

    ConfirmScreen, SummonScreen { align: center middle; }
    .modal-box { width: 64; height: auto; border: thick #f0b429; background: #161b22;
                 padding: 1 2; }
    .modal-q { margin-bottom: 1; text-style: bold; }
    .modal-btns { height: auto; align-horizontal: right; }
    .modal-btns Button { margin-left: 2; }
    """

    def __init__(self):
        super().__init__()
        self._loops: list[dict] = []
        self._pipe_sel: tuple[str, dict] | None = None
        self._ev_written = 0

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        with TabbedContent(initial="tab-usine"):
            with TabPane("🏭 Usine [1]", id="tab-usine"):
                with VerticalScroll():
                    yield Static(id="statsbar")
                    yield Grid(*[AgentCard(k, e, n, kind) for k, e, n, kind in core.ROSTER],
                               id="agent-grid")
                    yield Static(id="alarms-panel", classes="panel")
                    yield Static(id="pending-panel", classes="panel")
                    yield Static(id="score-panel", classes="panel")
            with TabPane("🚢 Pipeline [2]", id="tab-pipeline"):
                yield DataTable(id="pr-table")
                yield DataTable(id="issue-table")
                yield Static(id="detail-panel", classes="panel")
            with TabPane("🔄 Loops [3]", id="tab-loops"):
                yield DataTable(id="loop-table")
                yield RichLog(id="loop-log", wrap=True, highlight=False, markup=False)
            with TabPane("💬 Intercom [4]", id="tab-intercom"):
                yield RichLog(id="events-log", wrap=True, markup=True)
                yield Static(id="inbox-panel", classes="panel")
                yield Static(id="ticker-panel", classes="panel")
            with TabPane("🏷 Labels [5]", id="tab-labels"):
                with VerticalScroll():
                    yield Static(id="labels-body")
        yield Footer()

    # ── mount / setup ──
    def on_mount(self):
        for tid, cols in (("#pr-table", ("#", "titre", "labels", "état", "maj")),
                          ("#issue-table", ("#", "titre", "labels", "maj")),
                          ("#loop-table", ("rôle", "cible", "étape", "statut", "depuis", "raison"))):
            t = self.query_one(tid, DataTable)
            t.cursor_type = "row"
            t.zebra_stripes = True
            t.add_columns(*cols)
        # visible before the first slow poll fills in the counts
        self.query_one("#pr-table", DataTable).border_title = "🚢 Pull Requests"
        self.query_one("#issue-table", DataTable).border_title = "🐛 Issues"
        self.query_one("#loop-table", DataTable).border_title = "🔄 Loops looper"
        for wid, title in (("#alarms-panel", "🚨 Alarmes"), ("#pending-panel", "🙋 En attente de toi"),
                           ("#score-panel", "🏆 Scoreboard 24h"), ("#detail-panel", "🔍 Détail"),
                           ("#inbox-panel", "📬 Inbox agents"), ("#ticker-panel", "📻 Radio atelier"),
                           ("#events-log", "💬 Événements inter-agents"), ("#loop-log", "📜 Log du loop")):
            self.query_one(wid).border_title = title
        self.query_one("#labels-body", Static).update(self._labels_markup())
        if os.environ.get("FACTORY_NO_POLL"):
            return  # --check: deterministic, no subprocess churn
        self.poll_fast()
        self.poll_med()
        self.poll_slow()
        self.set_interval(3, self.poll_fast)
        self.set_interval(10, self.poll_med)
        self.set_interval(45, self.poll_slow)

    # ── pollers (threads) → renderers (UI thread) ──
    @work(thread=True, exclusive=True, group="fast")
    def poll_fast(self):
        core.poll_fast()
        self.call_from_thread(self.render_fast)

    @work(thread=True, exclusive=True, group="med")
    def poll_med(self):
        core.poll_med()
        read_events()
        self.call_from_thread(self.render_med)

    @work(thread=True, exclusive=True, group="slow")
    def poll_slow(self):
        gh_poll()
        self.call_from_thread(self.render_slow)

    def render_fast(self):
        now = time.time()
        with core.LOCK:
            loops = list(core.STATE["loops"])
            ps_ok = core.STATE["ps_ok"]
            mail = sum(core.STATE["mail_pending"].values())
        with DLOCK:
            n_prs, n_issues, gh_ok = len(DATA["prs"]), len(DATA["issues"]), DATA["gh_ok"]
        alarm_rows = core.alarms(now)
        running = sum(1 for l in loops if l["status"] == "running")
        queued = sum(1 for l in loops if l["status"] == "queued")
        bar = Text()
        bar.append(f"⚙ {running} en cours", style=f"bold {C['ok']}")
        bar.append(f"  ⏳ {queued} en file", style=C["warn"])
        bar.append(f"  🚢 {n_prs} PRs  🐛 {n_issues} issues", style=C["info"])
        bar.append(f"  📬 {mail}", style=C["accent"] if mail else C["dim"])
        bar.append(f"  🚨 {len(alarm_rows)}", style=f"bold {C['err']}" if alarm_rows else C["dim"])
        bar.append(f"   looper {'✅' if ps_ok else '🔴'}  github {'✅' if gh_ok else '🔴'}", style=C["dim"])
        self.query_one("#statsbar", Static).update(bar)
        for card in self.query(AgentCard):
            card.refresh_state()
        panel = self.query_one("#alarms-panel", Static)
        panel.set_classes("panel alert" if alarm_rows else "panel")
        if alarm_rows:
            panel.update(Text("\n".join(r for _, r in alarm_rows), style=C["err"]))
        else:
            panel.update(Text("aucune alarme — l'usine ronronne ✨", style=C["ok"]))
        # loops page
        table = self.query_one("#loop-table", DataTable)
        cur = table.cursor_row
        table.clear()
        self._loops = sorted(loops, key=core.loop_order)
        for lp in self._loops:
            age = core.ago(now - lp["since"], fine=True) if lp.get("since") else "—"
            st_style = {"running": C["ok"], "queued": C["warn"],
                        "manual_intervention": f"bold {C['err']}"}.get(lp["status"], C["dim"])
            reason = Text("")
            if lp["status"] == "manual_intervention" and lp.get("loopId"):
                reason = Text(loop_park_reason(lp["loopId"])[:70] + "  → u débloque",
                              style=C["err"])
            table.add_row(Text(lp["type"], style="bold"), core.loop_num(lp), lp["step"],
                          Text(lp["status"], style=st_style), age, reason)
        table.border_title = f"🔄 Loops — looper {'✅' if ps_ok else '🔴 injoignable'}"
        if self._loops:
            table.move_cursor(row=min(max(cur, 0), len(self._loops) - 1))
        self._render_loop_log()

    def _render_loop_log(self):
        log = self.query_one("#loop-log", RichLog)
        idx = self.query_one("#loop-table", DataTable).cursor_row
        if not self._loops or idx < 0 or idx >= len(self._loops):
            log.clear()
            log.write("(aucun loop)")
            return
        lp = self._loops[idx]
        name, tail = loop_tail_any(lp, 40)
        log.border_title = f"📜 {lp['type']} #{core.loop_num(lp)} — {name or 'aucun log'}"
        log.clear()
        for line in tail or ["(pas encore de log pour ce run)"]:
            log.write(line)

    def render_med(self):
        with core.LOCK:
            ticker = core.STATE["ticker"]
            mail = dict(core.STATE["mail_pending"])
        with DLOCK:
            events = list(DATA["events"])
        log = self.query_one("#events-log", RichLog)
        if len(events) < self._ev_written:  # log was rotated/truncated → rebuild
            log.clear()
            self._ev_written = 0
        for e in events[self._ev_written:]:
            ts = (e.get("ts") or "")[11:16]
            if e.get("event") == "consumed":
                log.write(f"[{C['dim']}]{ts}[/] [{C['ok']}]✓ {e.get('by', '?')}[/] a traité : {e.get('subject', '')}")
            else:
                ref = f" [{C['dim']}]\\[{e['ref']}][/]" if e.get("ref") else ""
                log.write(f"[{C['dim']}]{ts}[/] [{C['cyan']}]{e.get('from', '?')}[/] → "
                          f"[{C['cyan']}]{e.get('to', '?')}[/] : {e.get('subject', '')}{ref}")
        self._ev_written = len(events)
        inbox = self.query_one("#inbox-panel", Static)
        if mail:
            inbox.update(Text("  ".join(f"{a}: {n} en attente" for a, n in sorted(mail.items())),
                              style=C["accent"]))
        else:
            inbox.update(Text("aucun message en attente", style=C["dim"]))
        self.query_one("#ticker-panel", Static).update(Text(ticker or "silence radio", style=C["dim"]))

    def render_slow(self):
        now = time.time()
        with DLOCK:
            prs, issues = list(DATA["prs"]), list(DATA["issues"])
            score, pending = DATA["score"], list(DATA["pending"])
        table = self.query_one("#pr-table", DataTable)
        cur = table.cursor_row
        table.clear()
        for p in sorted(prs, key=lambda p: -p["number"]):
            merge_ico = {"MERGEABLE": Text("✓", style=C["ok"]),
                         "CONFLICTING": Text("💥 conflit", style=C["err"])}.get(
                             p.get("mergeable"), Text("?", style=C["dim"]))
            if p.get("isDraft"):
                merge_ico = Text("draft", style=C["dim"])
            t = core.iso_ts(p.get("updatedAt"))
            table.add_row(str(p["number"]), p.get("title", "")[:60], chips(p.get("labels", [])),
                          merge_ico, core.ago(now - t) if t else "—", key=f"pr:{p['number']}")
        table.border_title = f"🚢 Pull Requests ({len(prs)})"
        if prs:
            table.move_cursor(row=min(max(cur, 0), len(prs) - 1))
        table = self.query_one("#issue-table", DataTable)
        cur = table.cursor_row
        table.clear()
        for i in sorted(issues, key=lambda i: -i["number"]):
            t = core.iso_ts(i.get("updatedAt"))
            table.add_row(str(i["number"]), i.get("title", "")[:60], chips(i.get("labels", [])),
                          core.ago(now - t) if t else "—", key=f"issue:{i['number']}")
        table.border_title = f"🐛 Issues ({len(issues)})"
        if issues:
            table.move_cursor(row=min(max(cur, 0), len(issues) - 1))
        pend = self.query_one("#pending-panel", Static)
        if pending:
            body = Text("\n".join(pending))
            body.append("\n→ page Pipeline [2] : m merge · h lève un hold", style=C["dim"])
            pend.update(body)
        else:
            pend.update(Text("rien n'attend ton action 🎉", style=C["ok"]))
        sc = self.query_one("#score-panel", Static)
        if score == "err":
            sc.update(Text("(github injoignable)", style=C["err"]))
        elif score:
            body = Text()
            body.append(f"🔍 scout {score['scout']} issues · {score['scout_ok']} approuvées    ", style=C["info"])
            body.append(f"🔥 roast {score['roast']} verdicts · {score['kills']} kills\n", style=C["info"])
            body.append(f"🚢 worker {score['prs']} PRs · {score['merged']} mergées    ", style=C["info"])
            body.append(f"🧪 qa {score['qa_ok']} ✓ · {score['qa_ko']} ✗\n", style=C["info"])
            body.append(f"⚡ {score['spark']}", style=C["accent"])
            if score.get("star"):
                body.append(f"   ⭐ employé du jour : {score['star'].upper()}", style=f"bold {C['accent']}")
            sc.update(body)
        else:
            sc.update(Text("pas encore d'activité sur 24h", style=C["dim"]))
        self._render_detail()

    @on(AgentCard.Open)
    def _open_agent(self, ev: AgentCard.Open):
        c = ev.card
        self.push_screen(AgentViewScreen(c.key, c.emoji, c.agent_name, c.kind))

    # ── pipeline selection + detail ──
    @on(DataTable.RowHighlighted, "#pr-table")
    def _sel_pr(self, ev: DataTable.RowHighlighted):
        self._select_from_key(ev.row_key.value if ev.row_key else None)

    @on(DataTable.RowHighlighted, "#issue-table")
    def _sel_issue(self, ev: DataTable.RowHighlighted):
        self._select_from_key(ev.row_key.value if ev.row_key else None)

    @on(DataTable.RowHighlighted, "#loop-table")
    def _sel_loop(self, _):
        self._render_loop_log()

    def _select_from_key(self, key: str | None):
        if not key:
            return
        kind, num = key.split(":")
        with DLOCK:
            pool = DATA["prs"] if kind == "pr" else DATA["issues"]
            item = next((x for x in pool if str(x["number"]) == num), None)
        if item:
            self._pipe_sel = (kind, item)
            self._render_detail()

    def _render_detail(self):
        panel = self.query_one("#detail-panel", Static)
        if not self._pipe_sel:
            panel.update(Text("sélectionne une PR ou une issue", style=C["dim"]))
            return
        kind, item = self._pipe_sel
        body = Text()
        body.append(f"{'PR' if kind == 'pr' else 'Issue'} #{item['number']} ", style=f"bold {C['accent']}")
        body.append(item.get("title", ""))
        author = (item.get("author") or {}).get("login", "")
        if author:
            body.append(f"  — @{author}", style=C["dim"])
        for l in sorted(item.get("labels", []), key=lambda l: l["name"]):
            name = l["name"]
            info = legend_of(name)
            body.append(f"\n {name}", style=chip_style(name))
            if info:
                meaning, action = info
                body.append(f" — {meaning}", style=C["dim"])
                if action:
                    body.append(f"\n   → TOI : {action}", style=f"bold {C['err']}")
        panel.update(body)

    # ── actions ──
    def action_tab(self, tab_id: str):
        # switching silently no-ops while focus sits inside the outgoing pane
        # (e.g. on an AgentCard) — blur first, the switch always wins
        self.set_focus(None)
        self.query_one(TabbedContent).active = tab_id

    def action_refresh_all(self):
        self.poll_fast()
        self.poll_med()
        self.poll_slow()
        self.notify("rafraîchissement lancé", timeout=2)

    def action_summon(self):
        def done(res):
            if res:
                agent, msg = res
                self._run_summon(agent, msg)
        self.push_screen(SummonScreen(), done)

    @work(thread=True, group="action")
    def _run_summon(self, agent: str, msg: str):
        out = core.send_summon(agent, msg)
        self.call_from_thread(self.notify, f"{agent}: {out}",
                              severity="information" if out.startswith("✉") else "error")

    def action_merge(self):
        if not self._pipe_sel or self._pipe_sel[0] != "pr":
            self.notify("sélectionne une PR (page Pipeline)", severity="warning")
            return
        pr = self._pipe_sel[1]
        labels = {l["name"] for l in pr.get("labels", [])}
        if "ready-to-merge" not in labels:
            self.notify(f"PR #{pr['number']} n'est pas ready-to-merge", severity="warning")
            return

        def done(ok):
            if ok:
                self._run_gh(["pr", "merge", str(pr["number"]), "-R", REPO, "--squash"],
                             f"PR #{pr['number']} mergée 🎉")
        self.push_screen(ConfirmScreen(f"Merger (squash) PR #{pr['number']} — {pr.get('title', '')[:50]} ?",
                                       "Merger"), done)

    def action_unhold(self):
        if not self._pipe_sel:
            self.notify("sélectionne une PR ou issue (page Pipeline)", severity="warning")
            return
        kind, item = self._pipe_sel
        labels = {l["name"] for l in item.get("labels", [])}
        if "looper:hold" not in labels:
            self.notify(f"#{item['number']} n'a pas de looper:hold", severity="warning")
            return

        def done(ok):
            if ok:
                self._run_gh([kind if kind == "pr" else "issue", "edit", str(item["number"]),
                              "-R", REPO, "--remove-label", "looper:hold"],
                             f"hold levé sur #{item['number']} — la loop reprend")
        self.push_screen(ConfirmScreen(f"Lever looper:hold sur #{item['number']} ?", "Lever"), done)

    def action_open_web(self):
        if not self._pipe_sel:
            self.notify("sélectionne une PR ou issue (page Pipeline)", severity="warning")
            return
        kind, item = self._pipe_sel
        self._run_gh([kind if kind == "pr" else "issue", "view", str(item["number"]),
                      "-R", REPO, "--web"], f"#{item['number']} ouvert dans le navigateur")

    def action_unblock(self):
        idx = self.query_one("#loop-table", DataTable).cursor_row
        if not self._loops or idx < 0 or idx >= len(self._loops):
            self.notify("sélectionne un loop (page Loops [3])", severity="warning")
            return
        lp = self._loops[idx]
        if lp["status"] != "manual_intervention":
            self.notify(f"{lp['type']} #{core.loop_num(lp)} n'attend pas d'intervention", severity="warning")
            return
        reason = loop_park_reason(lp.get("loopId") or "") or "raison inconnue"

        def done(ok):
            if ok:
                self._do_unblock(dict(lp))
        self.push_screen(ConfirmScreen(
            f"Débloquer {lp['type']} #{core.loop_num(lp)} ?\n\n« {reason[:160]} »\n\n"
            "→ sweep des fichiers scratch non-trackés (.review*.json) puis looper retry",
            "Débloquer"), done)

    @work(thread=True, group="action")
    def _do_unblock(self, lp: dict):
        swept: list[str] = []
        try:
            out = core.sh("looper ps --json 2>/dev/null")
            for item in json.loads(out).get("items", []):
                if item.get("loopId") == lp.get("loopId"):
                    wt = (item.get("worktree") or {}).get("path") or ""
                    if wt and os.path.isdir(wt):
                        swept = sweep_scratch_files(wt)
                    break
        except Exception:
            pass
        r = subprocess.run(["looper", "retry", lp.get("loopId") or ""],
                           capture_output=True, text=True, timeout=30)
        if r.returncode == 0:
            what = f"{len(swept)} scratch supprimé(s) · " if swept else ""
            self.call_from_thread(self.notify,
                                  f"{lp['type']} #{core.loop_num(lp)} : {what}retry en file ✓")
            self.poll_fast()
        else:
            err = (r.stderr or r.stdout or "échec").strip()[:120]
            self.call_from_thread(self.notify, f"looper retry : {err}", severity="error", timeout=8)

    @work(thread=True, group="action")
    def _run_gh(self, args: list[str], ok_msg: str):
        try:
            r = subprocess.run(["gh"] + args, capture_output=True, text=True, timeout=60)
            if r.returncode == 0:
                self.call_from_thread(self.notify, ok_msg)
                gh_poll()
                self.call_from_thread(self.render_slow)
            else:
                err = (r.stderr or r.stdout or "échec").strip()[:120]
                self.call_from_thread(self.notify, err, severity="error", timeout=8)
        except Exception as e:
            self.call_from_thread(self.notify, str(e)[:120], severity="error", timeout=8)

    # ── labels page ──
    def _labels_markup(self) -> Text:
        body = Text()
        body.append("● BLOQUANT — attend une action de TOI\n", style=f"bold {C['err']}")
        for lbl, meaning, action in core.LABEL_BLOCKING:
            body.append(f"\n {lbl}\n", style=f"bold on #2d1416")
            body.append(f"   {meaning}\n", style=C["dim"])
            body.append(f"   → {action}\n", style=f"bold {C['accent']}")
        body.append("\n● PIPELINE — automatique, rien à faire\n", style=f"bold {C['info']}")
        for lbl, meaning in core.LABEL_PIPELINE:
            body.append(f"\n {lbl}", style="bold")
            body.append(f" — {meaning}", style=C["dim"])
        return body


def selfcheck() -> int:
    """Headless smoke test: mount, walk every page, open/close the modals."""
    import asyncio
    os.environ["FACTORY_NO_POLL"] = "1"

    async def run():
        app = FactoryApp()
        async with app.run_test(size=(120, 40)) as pilot:
            for key in ("2", "3", "4", "5", "1"):
                await pilot.press(key)
                await pilot.pause()
            assert app.query_one("#pr-table", DataTable).row_count == 0
            assert "BLOQUANT" in app._labels_markup().plain
            await pilot.press("s")
            await pilot.pause()
            assert isinstance(app.screen, SummonScreen)
            await pilot.press("escape")
            await pilot.pause()
            # live agent view: focus a card, Enter opens, Esc closes
            app.query(AgentCard).first().focus()
            await pilot.press("enter")
            await pilot.pause()
            assert isinstance(app.screen, AgentViewScreen)
            await pilot.press("escape")
            await pilot.pause()
            assert not isinstance(app.screen, AgentViewScreen)
            # regression: tab keys must still work while a card holds focus
            await pilot.press("2")
            await pilot.pause()
            assert app.query_one(TabbedContent).active == "tab-pipeline"
            # unblock on an empty loops table warns instead of crashing
            await pilot.press("3")
            await pilot.pause()
            await pilot.press("u")
            await pilot.pause()
            assert sweep_scratch_files("/nonexistent-worktree") == []
            # renderers must survive an empty world (no looper, no gh, no inbox)
            app.render_fast()
            app.render_med()
            app.render_slow()
        # legend map covers every label the legend page shows
        for lbl, _, action in core.LABEL_BLOCKING:
            assert legend_of(lbl) == (_, action) or legend_of(lbl)[1] == action
        assert legend_of("kind/bug") is not None and legend_of("nope-nope") is None
        assert chip_style("ready-to-merge") != chip_style("kind/bug")

    asyncio.run(run())
    print("selfcheck: OK")
    return 0


if __name__ == "__main__":
    if "--check" in sys.argv:
        sys.exit(selfcheck())
    FactoryApp().run()
