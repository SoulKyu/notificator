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
import calendar
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
INBOX_DIR = os.environ.get("FACTORY_INBOX_DIR", os.path.expanduser("~/.claude-agents/notificator/inbox"))
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

STATE = {"loops": [], "svc": {}, "timers": {}, "prs": [], "issues": "", "ticker": "", "err": "",
         "mail_pending": {}, "intercom": [], "score": None, "events": []}
LOCK = threading.Lock()

# one-shot animations, consumed by the render loop (render-side state only)
ANIM = {"mail": [], "party": []}
MAIL_TICKS, PARTY_TICKS = 4, 12  # ~1 s flight, ~3 s banner at 4 fps
MAIL_SEEN = None   # (box, filename) pairs already counted — None until first poll
PR_PREV = None     # {number: title} of open PRs at previous poll


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


def dslice(s, start):
    """Drop the first `start` display columns (space-pads if a wide char is split)."""
    w = 0
    for i, c in enumerate(s):
        if w >= start:
            return " " * (w - start) + s[i:]
        w += dwidth(c)
    return ""


def overlay(line, col, s):
    """Paint `s` at display column `col`, preserving total display width."""
    return dpad(line, col) + s + dslice(line, col + dwidth(s))


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
    # agent-to-agent mail: pending inboxes + recent archived conversations
    global MAIL_SEEN
    pending, intercom, events, seen = {}, [], [], set()
    try:
        for box in os.listdir(INBOX_DIR):
            if box == "archive":
                continue
            msgs = [f for f in os.listdir(os.path.join(INBOX_DIR, box)) if f.startswith("msg-")]
            if msgs:
                pending[box] = len(msgs)
            for f in msgs:
                seen.add((box, f))
                if MAIL_SEEN is not None and (box, f) not in MAIL_SEEN:
                    head = open(os.path.join(INBOX_DIR, box, f), errors="replace").read(2048)
                    m = re.search(r"^From: (\S+)", head, re.M)
                    events.append({"kind": "mail", "frm": m.group(1) if m else None, "to": box})
        MAIL_SEEN = seen
        arch = os.path.join(INBOX_DIR, "archive")
        for f in sorted(os.listdir(arch), reverse=True)[:3]:
            to = f.rsplit(".", 1)[-1]
            head, _, body = open(os.path.join(arch, f), errors="replace").read().partition("\n\n")
            frm = next((l[6:] for l in head.splitlines() if l.startswith("From: ")), "?")
            first = body.strip().splitlines()[0] if body.strip() else ""
            intercom.append(f"{frm} → {to}: {first[:120]}")
        intercom.reverse()
    except Exception:
        pass
    with LOCK:
        STATE["timers"], STATE["mail_pending"], STATE["intercom"] = timers, pending, intercom
        STATE["events"].extend(events[:6])


def compute_score(issues, prs, now):
    """24h team stats from raw gh JSON. -> dict, or None when nothing happened."""
    cutoff = now - 86400

    def ts(s):
        try:
            return calendar.timegm(time.strptime((s or "")[:19], "%Y-%m-%dT%H:%M:%S"))
        except Exception:
            return None

    sc = {"scout": 0, "scout_ok": 0, "roast": 0, "kills": 0,
          "prs": 0, "merged": 0, "qa_ok": 0, "qa_ko": 0}
    events = []
    for i in issues:
        labels = {l["name"] for l in i.get("labels", [])}
        t = ts(i.get("createdAt"))
        if t is None or t < cutoff:
            continue
        events.append(t)
        if "agent:proposed" in labels:
            sc["scout"] += 1
            if "roast:approved" in labels:
                sc["scout_ok"] += 1
        if any(l.startswith("roast:") for l in labels):
            sc["roast"] += 1
        if "roast:rejected" in labels:
            sc["kills"] += 1
    for p in prs:
        labels = {l["name"] for l in p.get("labels", [])}
        created, merged = ts(p.get("createdAt")), ts(p.get("mergedAt"))
        from_looper = (p.get("headRefName") or "").startswith("looper/")
        if created is not None and created >= cutoff:
            events.append(created)
            if from_looper:
                sc["prs"] += 1
        if merged is not None and merged >= cutoff:
            events.append(merged)
            if from_looper:
                sc["merged"] += 1
        if "qa:passed" in labels:
            sc["qa_ok"] += 1
        elif "qa:failed" in labels:
            sc["qa_ko"] += 1
    if not events and not any(sc.values()):
        return None
    hours = [0] * 24
    for t in events:
        hours[min(23, int((t - cutoff) // 3600))] += 1
    top = max(hours) or 1
    blocks = "▁▂▃▄▅▆▇█"
    sc["spark"] = "".join("▁" if n == 0 else blocks[1 + n * 6 // top] for n in hours)
    outcomes = {"scout": sc["scout_ok"], "roast": sc["roast"], "worker": sc["merged"], "qa": sc["qa_ok"]}
    best = max(outcomes, key=lambda a: (outcomes[a], sc["merged"] if a == "worker" else 0))
    sc["star"] = best if outcomes[best] else None
    return sc


def poll_slow():
    global PR_PREV
    prs, events = [], []
    out = sh(f"gh pr list -R {shlex.quote(REPO)} --state open --json number,title,labels,mergeable 2>/dev/null", 30)
    if not out.strip():
        # `gh` returns "[]" for zero PRs — an empty string means GitHub is unreachable
        with LOCK:
            STATE["prs"], STATE["issues"] = ["(github injoignable)"], "(github injoignable)"
            STATE["score"] = "err"
        return
    try:
        data = json.loads(out or "[]")
        for p in data:
            labels = {l["name"] for l in p["labels"]}
            tag = ("💥conflit" if p.get("mergeable") == "CONFLICTING" else
                   "🧪qa✗" if "qa:failed" in labels else
                   "✅qa" if "qa:passed" in labels else
                   "📐spec" if any("spec" in l for l in labels) else "👀review")
            prs.append(f"PR#{p['number']} {tag}")
        now_open = {p["number"]: p.get("title", "") for p in data}
        # a PR gone from the open list may just have been merged → party
        if PR_PREV is not None:
            for n in sorted(set(PR_PREV) - set(now_open))[:3]:
                st = sh(f"gh pr view {n} -R {shlex.quote(REPO)} --json state 2>/dev/null", 30)
                try:
                    if json.loads(st).get("state") == "MERGED":
                        events.append({"kind": "party", "pr": f"PR#{n} {PR_PREV[n][:50]}"})
                except Exception:
                    pass
        PR_PREV = now_open
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
    since = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - 86400))
    i_out = sh(f"gh issue list -R {shlex.quote(REPO)} --state all --limit 200 "
               f"--search {shlex.quote(f'created:>{since}')} --json labels,createdAt 2>/dev/null", 30)
    p_out = sh(f"gh pr list -R {shlex.quote(REPO)} --state all --limit 200 "
               f"--search {shlex.quote(f'updated:>{since}')} "
               f"--json labels,createdAt,mergedAt,headRefName 2>/dev/null", 30)
    if not i_out.strip() or not p_out.strip():
        score = "err"
    else:
        try:
            score = compute_score(json.loads(i_out), json.loads(p_out), time.time())
        except Exception:
            score = None
    with LOCK:
        STATE["prs"], STATE["score"] = prs, score
        STATE["events"].extend(events)


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
    if state == "away":  # on break, gone to the coffee corner — empty chair
        return [
            " ┌────────┐",
            " │" + dpad("off", 8, center=True) + "│",
            " └────────┘",
            "",
            "   ╰────╯",
        ], 2
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


def desk_cell(emoji, name, key, state3, tick, seed, coffee_on):
    """-> (8 display lines of width CELL_W+2, color)"""
    state, status, detail = state3
    if state == "break" and coffee_on:
        state, status, detail = "away", "au café", "☕"
    inner, color = person_cell(state, tick, seed, status, detail)
    with LOCK:
        mail = STATE["mail_pending"].get(key, 0)
    title = f" {emoji} {name} 📬 " if mail else f" {emoji} {name} "
    lines = ["┌" + dpad(title, CELL_W, center=True, fill="─") + "┐"]
    for l in inner:
        lines.append("│" + dpad(l, CELL_W) + "│")
    lines.append("│" + dpad(" " + (status + " " + detail).strip(), CELL_W) + "│")
    lines.append("└" + "─" * CELL_W + "┘")
    return lines, color


def coffee_corner(tick, breakers, w):
    """-> 8 lines (width w) of coffee machine, with break-state agents queuing."""
    steam = ["~ ", " ~", "· ", " ·"][tick % 4]
    face = ["(u_u)☕", "(^o^)☕"][(tick // 2) % 2] if breakers else ""
    queue = "".join(e for _, e in breakers[:5])
    return [
        dpad(" ☕ COIN CAFÉ", w),
        dpad("  ┌──────┐", w),
        dpad("  │ ████ │ " + steam, w),
        dpad("  │ ●──● │", w),
        dpad("  │ [══] │", w),
        dpad("  └──────┘", w),
        dpad("   " + face, w),
        dpad("   " + queue, w),
    ]


def apply_overlays(rows, tick, width, pos, board_pos, party_y):
    """Consume queued events, then paint mail flights and the merge banner."""
    with LOCK:
        fresh, STATE["events"] = STATE["events"], []
    for e in fresh:
        e["start"] = tick
        ANIM["mail" if e["kind"] == "mail" else "party"].append(e)
    ANIM["mail"] = [m for m in ANIM["mail"] if tick - m["start"] < MAIL_TICKS]
    ANIM["party"] = [p for p in ANIM["party"] if tick - p["start"] < PARTY_TICKS]
    for m in ANIM["mail"]:
        p = (tick - m["start"]) / (MAIL_TICKS - 1)
        x0, y0 = pos.get(m["frm"], board_pos)
        x1, y1 = pos.get(m["to"], board_pos)
        x = max(1, min(width - 3, round(x0 + (x1 - x0) * p)))
        y = max(1, min(len(rows) - 2, round(y0 + (y1 - y0) * p)))
        line, color = rows[y]
        rows[y] = (overlay(line, x, "✉"), color)
    if ANIM["party"]:
        deco = ["🎉", "✨"][tick % 2]
        msg = f" {deco} MERGÉ: {ANIM['party'][-1]['pr']} {deco} "
        y = max(1, min(len(rows) - 2, party_y))
        rows[y] = ("│" + dpad(msg, width - 2, center=True) + "│", 6)


def render_frame(tick, width=92):
    rows = []
    t = time.strftime("%H:%M:%S")
    title = "─ 🏭 NOTIFICATOR DEV FACTORY "
    rows.append(("┌" + dpad(title, width - 13, fill="─") + f" {t} ─┐", 0))
    per_row = max(1, (width - 4) // (CELL_W + 3))
    if per_row > 2 and (width - 4) - per_row * (CELL_W + 3) < 16:
        per_row -= 1  # give up one desk column so the coffee corner fits
    used = 2 + per_row * (CELL_W + 3)
    coffee_on = width - used - 2 >= 14
    states = {k: agent_state(k, kind) for k, _, _, kind in ROSTER}
    breakers = [(k, e) for k, e, _, _ in ROSTER if states[k][0] == "break"]
    pos = {k: (2 + (i % per_row) * (CELL_W + 3) + 11, 1 + (i // per_row) * 8 + 4)
           for i, (k, _, _, _) in enumerate(ROSTER)}
    coffee = coffee_corner(tick, breakers, width - used - 2) if coffee_on else None
    for start in range(0, len(ROSTER), per_row):
        chunk = ROSTER[start:start + per_row]
        cells = [desk_cell(e, n, k, states[k], tick, start + i, coffee_on) for i, (k, e, n, _) in enumerate(chunk)]
        for li in range(8):
            line = "│ "
            for cl, _ in cells:
                line += cl[li] + " "
            if start == 0 and coffee:
                line = dpad(line, used) + coffee[li]
            rows.append((dpad(line, width - 1) + "│", 0))
    office_rows = (len(ROSTER) + per_row - 1) // per_row
    with LOCK:
        prs, issues, ticker, err = STATE["prs"], STATE["issues"], STATE["ticker"], STATE["err"]
        intercom, score = list(STATE["intercom"]), STATE["score"]
    if intercom:
        rows.append(("│" + dpad(" ═══ 💬 INTERCOM ", width - 2, fill="═") + "│", 0))
        for msg in intercom:
            rows.append(("│ " + dpad(msg, width - 4) + " │", 6))
    if score:  # None (no data) → no panel
        rows.append(("│" + dpad(" ═══ 🏆 SCOREBOARD 24h ", width - 2, fill="═") + "│", 0))
        if score == "err":
            rows.append(("│ " + dpad("(github injoignable)", width - 4) + " │", 4))
        else:
            half = (width - 4) // 2
            rows.append(("│ " + dpad(f"🔍 scout  {score['scout']} issues · {score['scout_ok']} approuvées", half)
                         + dpad(f"🔥 roast  {score['roast']} verdicts · {score['kills']} kills", width - 4 - half) + " │", 5))
            rows.append(("│ " + dpad(f"🚢 worker {score['prs']} PRs · {score['merged']} mergées", half)
                         + dpad(f"🧪 qa     {score['qa_ok']} ✓ · {score['qa_ko']} ✗", width - 4 - half) + " │", 5))
            star = f"   ⭐ employé du jour: {score['star'].upper()}" if score["star"] else ""
            rows.append(("│ " + dpad("⚡ " + score["spark"] + star, width - 4) + " │", 6))
    rows.append(("│" + dpad(" ═══ 📌 TABLEAU DU MUR ", width - 2, fill="═") + "│", 0))
    board_pos = (width // 2, len(rows) - 1)
    rows.append(("│ " + dpad("  ".join(prs) or "aucune PR ouverte — tout est mergé 🎉", width - 4) + " │", 5))
    rows.append(("│ " + dpad(issues or "…", width - 4) + " │", 5))
    off = tick % max(1, len(ticker)) if len(ticker) > width - 12 else 0
    rows.append(("│ 📻 " + dpad(ticker[off:off + width - 8] or "silence radio", width - 6) + "│", 6))
    if err:
        rows.append(("│ ⚠ " + dpad(err, width - 5) + "│", 4))
    rows.append(("└" + "─" * (width - 2) + "┘", 0))
    apply_overlays(rows, tick, width, pos, board_pos, 1 + office_rows * 8 // 2)
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
    for state in ("work", "break", "sleep", "error", "wait", "away"):
        for tick in range(8):
            inner, _ = person_cell(state, tick, 3, "s", "d")
            row = inner[1]
            seg = row[:row.index("│", row.index("│") + 1) + 1]
            if dwidth(seg) != 11:
                print(f"FAIL {state} t{tick}: monitor segment {dwidth(seg)} cols: {seg!r}")
                fails += 1
            fails += sum(1 for l in inner if dwidth(l) > CELL_W)
    now = calendar.timegm(time.strptime("2026-01-02T00:00:00", "%Y-%m-%dT%H:%M:%S"))
    demo_issues = [{"labels": [{"name": "agent:proposed"}, {"name": "roast:approved"}],
                    "createdAt": "2026-01-01T12:00:00Z"}]
    demo_prs = [{"labels": [{"name": "qa:passed"}], "createdAt": "2026-01-01T13:00:00Z",
                 "mergedAt": "2026-01-01T20:00:00Z", "headRefName": "looper/x"}]
    s = compute_score(demo_issues, demo_prs, now)
    if not (s and s["scout"] == s["scout_ok"] == s["roast"] == 1 and s["kills"] == 0
            and s["prs"] == s["merged"] == s["qa_ok"] == 1 and len(s["spark"]) == 24
            and set(s["spark"]) <= set("▁▂▃▄▅▆▇█") and s["star"] == "worker"):
        print(f"FAIL compute_score: {s}")
        fails += 1
    if compute_score([], [], now) is not None:
        print("FAIL compute_score: empty input should be None")
        fails += 1
    with LOCK:
        STATE.update(prs=["PR#0 🧪qa✗"], issues="issues: 0", err="boom", ticker="x" * 300,
                     mail_pending={"scout": 2}, intercom=["roast → scout: amend #1 📬", "scout → roast: done"],
                     events=[{"kind": "mail", "frm": "scout", "to": "worker"},
                             {"kind": "mail", "frm": "inconnu", "to": "qa"},
                             {"kind": "party", "pr": "PR#7 grand merge 🎉"}])
    for score, w in ((s, 92), ("err", 92), (None, 60)):  # 92 → coffee corner on, 60 → fallback desks
        with LOCK:
            STATE["score"] = score
        for tick in range(6):
            for line, _ in render_frame(tick, w):
                if dwidth(line) != w:
                    print(f"FAIL row {dwidth(line)} cols (want {w}): {line!r}")
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
