#!/usr/bin/env python3
"""NOTIFICATOR DEV FACTORY — gamified real-time view of the agent office.

Little ASCII people at their desks: screens scroll when they code, coffee
steams on breaks, zzZ floats while they sleep, and screens flash on failures.
Zero dependencies (stdlib curses). Data: `looper ps`, systemd user
timers/services, `gh` (PRs/issues), newest agent log as chatter ticker.

Run:      python3 factory-tui.py
Test:     python3 factory-tui.py --once     (one frame, no curses)
Keys:     q quit
"""
import curses
import json
import os
import re
import shlex
import subprocess
import sys
import threading
import time
import unicodedata

LOG_DIR = os.environ.get("FACTORY_LOG_DIR", os.path.expanduser("~/.claude-agents/notificator/logs"))
REPO = os.environ.get("FACTORY_REPO", "SoulKyu/notificator")
POLL_FAST, POLL_MED, POLL_SLOW = 3, 10, 45

# desk order: (key, emoji, name, kind)  kind: looper-role | systemd unit | virtual
ROSTER = [
    ("scout",       "🔍", "SCOUT",  "svc:notificator-scout"),
    ("roast",       "🔥", "ROAST",  "virtual:scout-log"),
    ("coordinator", "🧭", "COORD",  "looper:coordinator"),
    ("planner",     "📐", "PLAN",   "looper:planner"),
    ("groomer",     "📋", "GROOM",  "svc:notificator-groomer"),
    ("worker",      "🚢", "WORKER", "looper:worker"),
    ("reviewer",    "🔎", "REVIEW", "looper:reviewer"),
    ("fixer",       "🔧", "FIXER",  "looper:fixer"),
    ("qa",          "🧪", "QA",     "svc:notificator-qa"),
    ("rebaser",     "🔀", "REBASE", "svc:notificator-rebaser"),
    ("promoter",    "⛓", "PROMO",  "svc:notificator-promoter"),
    ("docagent",    "📚", "DOC",    "svc:notificator-docagent"),
    ("reporter",    "📊", "REPORT", "svc:notificator-reporter"),
]
TIMER_OF = {
    "scout": "notificator-scout.timer", "roast": "notificator-scout.timer",
    "qa": "notificator-qa.timer", "rebaser": "notificator-rebaser.timer",
    "promoter": "notificator-promoter.timer", "groomer": "notificator-groomer.timer",
    "docagent": "notificator-docagent.timer", "reporter": "notificator-reporter.timer",
}

STATE = {"loops": [], "svc": {}, "timers": {}, "prs": [], "issues": "", "ticker": "", "err": ""}
LOCK = threading.Lock()


def sh(cmd, timeout=20):
    try:
        return subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout).stdout
    except Exception:
        return ""


def dwidth(s):
    """Terminal display width (emoji and CJK count double)."""
    return sum(2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in s)


def dpad(s, width, center=False, fill=" "):
    """Truncate/pad to an exact display width."""
    out = ""
    for c in s:
        if dwidth(out + c) > width:
            break
        out += c
    gap = width - dwidth(out)
    if center:
        left = gap // 2
        return fill * left + out + fill * (gap - left)
    return out + fill * gap


# ── data pollers ────────────────────────────────────────────────────────────

def poll_fast():
    loops = []
    out = sh("looper ps 2>/dev/null")
    for line in out.splitlines()[2:]:
        parts = line.split()
        if len(parts) >= 7 and parts[1] != "-":
            loops.append({"type": parts[1], "target": parts[2], "step": parts[3], "status": parts[6]})
    svc = {}
    units = " ".join(k.split(":")[1] + ".service" for _, _, _, k in ROSTER if k.startswith("svc:"))
    out = sh(f"systemctl --user show {units} -p Id,ActiveState,Result 2>/dev/null")
    cur = {}
    for line in out.splitlines() + [""]:
        if not line.strip():
            if "Id" in cur:
                svc[cur["Id"].replace(".service", "")] = cur
            cur = {}
        elif "=" in line:
            k, v = line.split("=", 1)
            cur[k] = v
    with LOCK:
        STATE["loops"], STATE["svc"] = loops, svc


def poll_med():
    timers = {}
    out = sh("systemctl --user list-timers 'notificator-*' --all --no-pager --plain 2>/dev/null")
    for line in out.splitlines():
        m = re.search(r"(\S+ \S+ \S+ \S+)\s+(.+?)\s+(?:\S+ \S+ \S+ \S+|-)\s+(?:.+?)\s+(notificator-\S+\.timer)", line)
        if m:
            timers[m.group(3)] = m.group(2).strip()
    try:
        logs = sorted((os.path.join(LOG_DIR, f) for f in os.listdir(LOG_DIR)), key=os.path.getmtime)
        if logs:
            tail = open(logs[-1], errors="replace").read().strip().splitlines()
            name = os.path.basename(logs[-1]).rsplit("-", 1)[0]
            line = next((l for l in reversed(tail) if l.strip() and "LOOPER_RESULT" not in l), "")
            with LOCK:
                STATE["ticker"] = f"[{name}] {line.strip()[:200]}"
    except Exception:
        pass
    with LOCK:
        STATE["timers"] = timers


def poll_slow():
    prs = []
    out = sh(f"gh pr list -R {shlex.quote(REPO)} --state open --json number,title,labels,mergeable 2>/dev/null", 30)
    if not out.strip():
        # `gh` returns "[]" for zero PRs — an empty string means GitHub is unreachable
        with LOCK:
            STATE["prs"], STATE["issues"] = ["(github injoignable)"], "(github injoignable)"
        return
    try:
        for p in json.loads(out or "[]"):
            labels = {l["name"] for l in p["labels"]}
            tag = ("💥conflit" if p.get("mergeable") == "CONFLICTING" else
                   "🧪qa✗" if "qa:failed" in labels else
                   "✅qa" if "qa:passed" in labels else
                   "📐spec" if any("spec" in l for l in labels) else "👀review")
            prs.append(f"PR#{p['number']} {tag}")
    except Exception:
        pass
    out = sh(f"gh issue list -R {shlex.quote(REPO)} --state open --json labels 2>/dev/null", 30)
    try:
        iss = json.loads(out or "[]")
        held = sum(1 for i in iss if any(l["name"] == "looper:hold" for l in i["labels"]))
        agent = sum(1 for i in iss if any(l["name"] == "agent:proposed" for l in i["labels"]))
        with LOCK:
            STATE["issues"] = f"issues: {len(iss)} open · {agent} agents · {held} hold"
    except Exception:
        pass
    with LOCK:
        STATE["prs"] = prs


def poller():
    last = {"fast": 0, "med": 0, "slow": 0}
    while True:
        now = time.time()
        for name, interval, fn in (("fast", POLL_FAST, poll_fast), ("med", POLL_MED, poll_med), ("slow", POLL_SLOW, poll_slow)):
            if now - last[name] >= interval:
                try:
                    fn()
                except Exception as e:
                    with LOCK:
                        STATE["err"] = str(e)[:80]
                last[name] = now
        time.sleep(1)


# ── little people ───────────────────────────────────────────────────────────
# Each state renders the inside of a desk cell: monitor (10 wide), a person,
# and a status line. All through dpad() — emoji are double width.

CODE_CHARS = "░▒▓█▓▒"


def screen_content(tick, seed, width=8):
    """Scrolling pseudo-code on the monitor."""
    return "".join(CODE_CHARS[(tick + seed * 7 + i * 3) % len(CODE_CHARS)] for i in range(width))


def person_cell(state, tick, seed, status, detail):
    """-> (5 inner lines, color) for a desk interior."""
    t = tick + seed * 5
    if state == "work":
        arms = ["/|    |\\", "\\|    |/"][t % 2]
        bubble = ["tak", "tak·", "tak··", "  ♪" if (t % 37) < 3 else "tak···"][t % 4]
        return [
            " ┌────────┐",
            " │" + screen_content(t, seed) + "│ " + bubble,
            " └────────┘",
            "   (^_^)⌨",
            "   " + arms,
        ], 1
    if state == "break":
        steam = ["  ~", " ~ ", "~  ", " ~ "][t % 4]
        return [
            " ┌────────┐",
            " │" + dpad("off", 8, center=True) + "│",
            " └────────┘" + steam,
            "   (u_u)☕",
            "   /|    |\\",
        ], 2
    if state == "sleep":
        zz = ["z", "zZ", "zzZ", " zZ"][t % 4]
        return [
            " ┌────────┐",
            " │········│",
            " └────────┘  " + zz,
            "   (-_-)",
            "   =====  ",
        ], 3
    if state == "error":
        flash = ["!ERROR!", "       "][t % 2]
        return [
            " ┌────────┐",
            " │" + dpad(flash, 8, center=True) + "│ 🔥",
            " └────────┘",
            "   (>_<)!!",
            "   /|    |\\",
        ], 4
    # wait / queued
    return [
        " ┌────────┐",
        " │ ▁▁▁▁▁▁ │ …",
        " └────────┘",
        "   (o_o)",
        "   /|    |\\",
    ], 2


def agent_state(key, kind):
    """-> (state, status, detail)"""
    with LOCK:
        loops, svc, timers, ticker = STATE["loops"], dict(STATE["svc"]), dict(STATE["timers"]), STATE["ticker"]
    nxt = timers.get(TIMER_OF.get(key, ""), "")
    if kind.startswith("looper:"):
        role = kind.split(":")[1]
        for lp in loops:
            if lp["type"] == role:
                tgt = lp["target"].split("/")[-1]
                if lp["status"] == "running":
                    return "work", lp["step"], tgt
                if lp["status"] == "queued":
                    return "wait", "en file", tgt
                return "wait", lp["status"], tgt
        return "break", "veille", "poll 30s"
    if kind == "virtual:scout-log":
        s = svc.get("notificator-scout", {})
        if s.get("ActiveState") in ("active", "activating"):
            if "roast" in ticker:
                return "work", "roast!", "issues"
            return "wait", "attend", "le scout"
        return "break", "pause", nxt or "?"
    unit = kind.split(":")[1]
    s = svc.get(unit, {})
    if s.get("ActiveState") in ("active", "activating"):
        return "work", "run", "en cours"
    if s.get("Result") not in ("success", "", None):
        return "error", "échec", s.get("Result", "")
    if nxt and any(u in nxt.split()[0] for u in ("h", "day", "week")) if nxt.split() else False:
        return "sleep", "dort", nxt
    return "break", "pause", nxt or "?"


CELL_W = 20  # inner width of a desk cell


def desk_cell(emoji, name, key, kind, tick, seed):
    """-> (8 display lines of width CELL_W+2, color)"""
    state, status, detail = agent_state(key, kind)
    inner, color = person_cell(state, tick, seed, status, detail)
    title = f" {emoji} {name} "
    lines = ["┌" + dpad(title, CELL_W, center=True, fill="─") + "┐"]
    for l in inner:
        lines.append("│" + dpad(l, CELL_W) + "│")
    lines.append("│" + dpad(" " + (status + " " + detail).strip(), CELL_W) + "│")
    lines.append("└" + "─" * CELL_W + "┘")
    return lines, color


def render_frame(tick, width=92):
    rows = []
    t = time.strftime("%H:%M:%S")
    title = "─ 🏭 NOTIFICATOR DEV FACTORY "
    rows.append(("┌" + dpad(title, width - 13, fill="─") + f" {t} ─┐", 0))
    per_row = max(1, (width - 4) // (CELL_W + 3))
    for start in range(0, len(ROSTER), per_row):
        chunk = ROSTER[start:start + per_row]
        cells = [desk_cell(e, n, k, kind, tick, start + i) for i, (k, e, n, kind) in enumerate(chunk)]
        for li in range(8):
            line = "│ "
            for cl, _ in cells:
                line += cl[li] + " "
            rows.append((dpad(line, width - 1) + "│", 0))
    with LOCK:
        prs, issues, ticker, err = STATE["prs"], STATE["issues"], STATE["ticker"], STATE["err"]
    rows.append(("│" + dpad(" ═══ 📌 TABLEAU DU MUR ", width - 2, fill="═") + "│", 0))
    rows.append(("│ " + dpad("  ".join(prs) or "aucune PR ouverte — tout est mergé 🎉", width - 4) + " │", 5))
    rows.append(("│ " + dpad(issues or "…", width - 4) + " │", 5))
    off = tick % max(1, len(ticker)) if len(ticker) > width - 12 else 0
    rows.append(("│ 📻 " + dpad(ticker[off:off + width - 8] or "silence radio", width - 6) + "│", 6))
    if err:
        rows.append(("│ ⚠ " + dpad(err, width - 5) + "│", 4))
    rows.append(("└" + "─" * (width - 2) + "┘", 0))
    return rows


def main_curses(scr):
    curses.curs_set(0)
    scr.nodelay(True)
    curses.start_color()
    curses.use_default_colors()
    for i, fg in ((1, curses.COLOR_GREEN), (2, curses.COLOR_YELLOW), (3, curses.COLOR_BLUE),
                  (4, curses.COLOR_RED), (5, curses.COLOR_CYAN), (6, curses.COLOR_MAGENTA)):
        curses.init_pair(i, fg, -1)
    tick = 0
    while True:
        if scr.getch() in (ord("q"), 27):
            return
        h, w = scr.getmaxyx()
        scr.erase()
        for y, (line, color) in enumerate(render_frame(tick, min(w - 1, 120))):
            if y >= h - 1:
                break
            try:
                scr.addstr(y, 0, line, curses.color_pair(color) if color else 0)
            except curses.error:
                pass
        scr.refresh()
        time.sleep(0.25)
        tick += 1


def selfcheck():
    """Alignment invariants: monitor segment = 11 cols in every state, frame rows all equal."""
    fails = 0
    for state in ("work", "break", "sleep", "error", "wait"):
        for tick in range(8):
            inner, _ = person_cell(state, tick, 3, "s", "d")
            row = inner[1]
            seg = row[:row.index("│", row.index("│") + 1) + 1]
            if dwidth(seg) != 11:
                print(f"FAIL {state} t{tick}: monitor segment {dwidth(seg)} cols: {seg!r}")
                fails += 1
            fails += sum(1 for l in inner if dwidth(l) > CELL_W)
    with LOCK:
        STATE.update(prs=["PR#0 🧪qa✗"], issues="issues: 0", err="boom", ticker="x" * 300)
    for tick in range(6):
        for line, _ in render_frame(tick, 92):
            if dwidth(line) != 92:
                print(f"FAIL row {dwidth(line)} cols: {line!r}")
                fails += 1
    print("selfcheck: OK" if fails == 0 else f"selfcheck: {fails} FAILURES")
    return fails


if __name__ == "__main__":
    if "--check" in sys.argv:
        sys.exit(1 if selfcheck() else 0)
    threading.Thread(target=poller, daemon=True).start()
    if "--once" in sys.argv:
        time.sleep(8)  # let pollers fill (gh calls can be slow)
        for line, _ in render_frame(2):
            print(line)
        sys.exit(0)
    time.sleep(1)
    try:
        curses.wrapper(main_curses)
    except KeyboardInterrupt:
        pass
