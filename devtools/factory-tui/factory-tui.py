#!/usr/bin/env python3
"""NOTIFICATOR DEV FACTORY — top-down real-time view of the agent office.

Zero dependencies (stdlib curses). Data: `looper ps`, systemd user timers/services,
`gh` (PRs/issues), and the newest agent log as a chatter ticker.

Run:      python3 factory-tui.py
Test:     python3 factory-tui.py --once     (one frame, no curses)
Keys:     q quit
"""
import curses
import json
import os
import re
import subprocess
import sys
import threading
import time
import unicodedata

LOG_DIR = os.environ.get("FACTORY_LOG_DIR", os.path.expanduser("~/.claude-agents/notificator/logs"))
REPO = os.environ.get("FACTORY_REPO", "SoulKyu/notificator")


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
POLL_FAST, POLL_MED, POLL_SLOW = 3, 10, 45

# desk order: (key, emoji, name, kind)  kind: looper-role | systemd unit name
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
TIMER_OF = {  # agent key -> systemd timer (for break countdowns)
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
    # ticker: freshest log's informative tail
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
    out = sh(f"gh pr list -R {REPO} --state open --json number,title,labels,mergeable 2>/dev/null", 30)
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
    out = sh(f"gh issue list -R {REPO} --state open --json labels 2>/dev/null", 30)
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


TYPING = ["⌨ tak", "⌨ tak·", "⌨ tak··", "⌨ tak···"]
COFFEE = ["☕", "☕~", "☕~~", "☕~"]
SLEEP = ["💤", "z", "zZ", "zZz"]
FIRE = ["🔥", "‼️"]


def agent_state(key, kind, tick):
    """-> (icon, line1, line2, color)  color: 1 ok/work 2 break 3 sleep 4 error"""
    with LOCK:
        loops, svc, timers, ticker = STATE["loops"], dict(STATE["svc"]), dict(STATE["timers"]), STATE["ticker"]
    nxt = timers.get(TIMER_OF.get(key, ""), "")
    if kind.startswith("looper:"):
        role = kind.split(":")[1]
        for lp in loops:
            if lp["type"] == role:
                tgt = lp["target"].split("/")[-1]
                if lp["status"] in ("running", "queued"):
                    return TYPING[tick % 4], f"{lp['step'][:10]}", tgt[:10], 1
                return "🤔", lp["status"][:10], tgt[:10], 2
        return COFFEE[tick % 4], "veille", "poll 30s", 2
    if kind == "virtual:scout-log":  # roast rides the scout service
        s = svc.get("notificator-scout", {})
        if s.get("ActiveState") == "activating" or s.get("ActiveState") == "active":
            if "roast" in ticker:
                return TYPING[tick % 4], "roast en", "cours!", 1
            return SLEEP[tick % 4], "attend le", "scout", 3
        return COFFEE[tick % 4], "pause", nxt[:10] or "?", 2
    unit = kind.split(":")[1]
    s = svc.get(unit, {})
    if s.get("ActiveState") in ("active", "activating"):
        return TYPING[tick % 4], "run en", "cours", 1
    if s.get("Result") not in ("success", "", None):
        return FIRE[tick % 2], "échec!", s.get("Result", "")[:10], 4
    if nxt and ("h" in nxt.split()[0] if nxt.split() else False):
        return SLEEP[tick % 4], "dort", nxt[:10], 3
    return COFFEE[tick % 4], "pause", nxt[:10] or "?", 2


def render_frame(tick, width=78):
    rows = []
    t = time.strftime("%H:%M:%S")
    rows.append(("┌─ NOTIFICATOR DEV FACTORY " + "─" * (width - 38) + f" {t} ─┐", 0))
    desks_per_row = max(2, (width - 4) // 13)
    for start in range(0, len(ROSTER), desks_per_row):
        chunk = ROSTER[start:start + desks_per_row]
        cells = []
        for key, emoji, name, kind in chunk:
            icon, l1, l2, color = agent_state(key, kind, tick)
            cells.append((f"{emoji} {name}", icon, l1, l2, color))
        for li in range(4):
            line = "│ "
            for c in cells:
                if li == 0:
                    line += "┌" + "─" * 10 + "┐ "
                elif li == 1:
                    line += "│" + dpad(c[0], 10) + "│ "
                elif li == 2:
                    line += "│" + dpad(c[1] + " " + c[2], 10) + "│ "
                else:
                    line += "└" + dpad(c[3], 10, center=True, fill="─") + "┘ "
            rows.append((dpad(line, width - 1) + "│", 0))
        rows.append(("│" + " " * (width - 2) + "│", 0))
    with LOCK:
        prs, issues, ticker, err = STATE["prs"], STATE["issues"], STATE["ticker"], STATE["err"]
    rows.append(("│ ── TABLEAU " + "─" * (width - 14) + " │", 0))
    rows.append(("│ " + dpad("  ".join(prs) or "aucune PR ouverte", width - 4) + " │", 5))
    rows.append(("│ " + dpad(issues or "…", width - 4) + " │", 5))
    off = tick % max(1, len(ticker)) if len(ticker) > width - 12 else 0
    rows.append(("│ 📻 " + dpad(ticker[off:off + width - 7] or "silence radio", width - 8) + "│", 6))
    if err:
        rows.append(("│ ⚠ " + dpad(err, width - 6) + "│", 4))
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
        for y, (line, color) in enumerate(render_frame(tick, min(w - 1, 100))):
            if y >= h - 1:
                break
            try:
                scr.addstr(y, 0, line, curses.color_pair(color) if color else 0)
            except curses.error:
                pass
        scr.refresh()
        time.sleep(0.25)
        tick += 1


if __name__ == "__main__":
    threading.Thread(target=poller, daemon=True).start()
    if "--once" in sys.argv:
        time.sleep(8)  # let pollers fill (gh calls can be slow)
        for line, _ in render_frame(2):
            print(line)
        sys.exit(0)
    time.sleep(1)
    curses.wrapper(main_curses)
