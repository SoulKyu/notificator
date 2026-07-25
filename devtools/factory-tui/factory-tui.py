#!/usr/bin/env python3
"""NOTIFICATOR DEV FACTORY — gamified real-time view of the agent office.

Little ASCII people at their desks: screens scroll when they code, coffee
steams on breaks, zzZ floats while they sleep, and screens flash on failures.
Zero dependencies (stdlib curses). Data: `looper ps`, systemd user
timers/services, `gh` (PRs/issues), newest agent log as chatter ticker.

Run:      python3 factory-tui.py
Test:     python3 factory-tui.py --once     (one frame, no curses)
Keys:     arrows select desk · Enter zoom · l follow log · s summon · Esc back · q quit
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
SUMMONABLE = {"scout", "roast", "qa", "rebaser", "groomer"}
SUMMON_SH = os.path.expanduser("~/.claude-agents/notificator/summon.sh")
ZOOM_TAIL = 15
STALL_MIN = int(os.environ.get("FACTORY_STALL_MIN") or 30)

STATE = {"loops": [], "svc": {}, "timers": {}, "prs": [], "issues": "", "ticker": "", "err": "",
         "mail_pending": {}, "intercom": [], "score": None, "events": [], "pending": []}
PENDING_MAX = 5
LOCK = threading.Lock()

# one-shot animations, consumed by the render loop (render-side state only)
ANIM = {"mail": [], "party": []}
MAIL_TICKS, PARTY_TICKS = 4, 12  # ~1 s flight, ~3 s banner at 4 fps
MAIL_SEEN = None   # (box, filename) pairs already counted — None until first poll
PR_PREV = None     # {number: title} of open PRs at previous poll
LOOP_SINCE = {}    # (type, target) -> (step, first_seen_ts) — stall detection
ALARM_SEEN = None  # alarm keys already klaxonned — None until the first curses frame
ALARM_FLASH = None # tick of the last new alarm; the title flashes for KLAXON_TICKS
KLAXON_TICKS = 12  # ~3 s at 4 fps


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
    out = sh(f"systemctl --user show {units} "
             "-p Id,ActiveState,Result,ExecMainStartTimestamp,ExecMainExitTimestamp,ExecMainStatus,NRestarts 2>/dev/null")
    cur = {}
    for line in out.splitlines() + [""]:
        if not line.strip():
            if "Id" in cur:
                svc[cur["Id"].replace(".service", "")] = cur
            cur = {}
        elif "=" in line:
            k, v = line.split("=", 1)
            cur[k] = v
    now = time.time()
    live = set()
    for lp in loops:
        k = (lp["type"], lp["target"])
        live.add(k)
        if LOOP_SINCE.get(k, (None,))[0] != lp["step"]:
            LOOP_SINCE[k] = (lp["step"], now)
    for k in set(LOOP_SINCE) - live:
        del LOOP_SINCE[k]
    with LOCK:
        STATE["loops"], STATE["svc"] = loops, svc


def ago(seconds):
    """Compact French age: 42s · 47min · 3h12 · 2j."""
    s = max(0, int(seconds))
    if s < 60:
        return f"{s}s"
    if s < 3600:
        return f"{s // 60}min"
    if s < 86400:
        return f"{s // 3600}h{s % 3600 // 60:02d}"
    return f"{s // 86400}j"


def systemd_ts(s):
    """'Sat 2026-07-25 04:00:12 CEST' -> epoch (local tz), or None."""
    m = re.search(r"\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}", s or "")
    try:
        return time.mktime(time.strptime(m.group(0), "%Y-%m-%d %H:%M:%S")) if m else None
    except Exception:
        return None


def alarms(now=None):
    """-> [(stable key, row)] for failed units and loops stalled past FACTORY_STALL_MIN."""
    now = now or time.time()
    with LOCK:
        svc, loops = dict(STATE["svc"]), list(STATE["loops"])
    out = []
    for unit, s in sorted(svc.items()):
        if s.get("Result") in ("success", "", None):
            continue
        t = systemd_ts(s.get("ExecMainExitTimestamp"))
        age = f" il y a {ago(now - t)}" if t else ""
        restarts = f" · {s['NRestarts']} restarts" if (s.get("NRestarts") or "0") != "0" else ""
        out.append((f"unit:{unit}",
                    f"🔥 {unit.replace('notificator-', '')} — échec {s['Result']} "
                    f"(exit {s.get('ExecMainStatus') or '?'}){age}{restarts}"))
    for (typ, target), (step, since) in sorted(LOOP_SINCE.items()):
        lp = next((l for l in loops if l["type"] == typ and l["target"] == target), None)
        if not lp or lp["status"] != "running" or now - since < STALL_MIN * 60:
            continue
        out.append((f"loop:{typ}:{target}",
                    f"⏳ {typ} {target.split('/')[-1]} bloqué sur {step} depuis {ago(now - since)}"))
    return out


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


def ts(s):
    """GitHub timestamp -> epoch seconds, or None."""
    try:
        return calendar.timegm(time.strptime((s or "")[:19], "%Y-%m-%dT%H:%M:%S"))
    except Exception:
        return None


def ago(sec):
    """Compact age: 42min · 4h · 3j."""
    if sec < 3600:
        return f"{max(0, int(sec // 60))}min"
    if sec < 86400:
        return f"{int(sec // 3600)}h"
    return f"{int(sec // 86400)}j"


def compute_pending(prs, issues, now):
    """Ball-in-your-court items from raw gh JSON, oldest first. -> [str]

    Covers the two gates the loop cannot pass on its own: a PR whose every gate is
    green (`ready-to-merge`) and an issue parked on `looper:hold`. `rebase:conflict`
    is deliberately out of scope — the rebaser owns that lane.
    """
    rows = []
    for p in prs:
        labels = {l["name"] for l in p.get("labels", [])}
        # `looper:review` is never cleared (it survives merge), so it says nothing
        # about the gate — `ready-to-merge` is what the agents actually set last.
        if (p.get("isDraft") or p.get("mergeable") != "MERGEABLE"
                or "ready-to-merge" not in labels or "qa:failed" in labels
                or "looper:spec-reviewing" in labels):
            continue
        t = ts(p.get("updatedAt")) or now
        rows.append((t, f"🚢 PR#{p['number']} prête à merger — {ago(now - t)}"))
    for i in issues:
        if not any(l["name"] == "looper:hold" for l in i.get("labels", [])):
            continue
        t = ts(i.get("updatedAt")) or now
        rows.append((t, f"⛔ #{i['number']} {i.get('title', '')[:50]} — attend ton go"))
    return [r for _, r in sorted(rows, key=lambda x: x[0])]


def compute_score(issues, prs, now):
    """24h team stats from raw gh JSON. -> dict, or None when nothing happened."""
    cutoff = now - 86400
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
    prs, events, open_prs, open_issues = [], [], [], []
    out = sh(f"gh pr list -R {shlex.quote(REPO)} --state open "
             f"--json number,title,labels,mergeable,isDraft,updatedAt 2>/dev/null", 30)
    if not out.strip():
        # `gh` returns "[]" for zero PRs — an empty string means GitHub is unreachable
        with LOCK:
            STATE["prs"], STATE["issues"] = ["(github injoignable)"], "(github injoignable)"
            STATE["score"], STATE["pending"] = "err", []
        return
    try:
        data = open_prs = json.loads(out or "[]")
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
    out = sh(f"gh issue list -R {shlex.quote(REPO)} --state open --json number,title,labels,updatedAt 2>/dev/null", 30)
    # same rule as the PR query: "" means unreachable, so say so instead of
    # publishing a hold queue we know is missing rows
    issues_ok = bool(out.strip())
    if not issues_ok:
        with LOCK:
            STATE["issues"] = "(github injoignable)"
    else:
        try:
            iss = open_issues = json.loads(out)
            held = sum(1 for i in iss if any(l["name"] == "looper:hold" for l in i["labels"]))
            agent = sum(1 for i in iss if any(l["name"] == "agent:proposed" for l in i["labels"]))
            with LOCK:
                STATE["issues"] = f"issues: {len(iss)} open · {agent} agents · {held} hold"
        except Exception:
            issues_ok = False
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
    pending = compute_pending(open_prs, open_issues, time.time())
    if not issues_ok:
        pending.append("⛔ hold: (github injoignable)")
    with LOCK:
        STATE["prs"], STATE["score"] = prs, score
        STATE["pending"] = pending
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


def desk_cell(emoji, name, key, state3, tick, seed, coffee_on, selected=False):
    """-> (8 display lines of width CELL_W+2, color)"""
    state, status, detail = state3
    if state == "break" and coffee_on:
        state, status, detail = "away", "au café", "☕"
    inner, color = person_cell(state, tick, seed, status, detail)
    with LOCK:
        mail = STATE["mail_pending"].get(key, 0)
    title = f" {emoji} {name} 📬 " if mail else f" {emoji} {name} "
    tl, tr, bl, br, hz, vt = ("╔", "╗", "╚", "╝", "═", "║") if selected else ("┌", "┐", "└", "┘", "─", "│")
    lines = [tl + dpad(title, CELL_W, center=True, fill=hz) + tr]
    for l in inner:
        lines.append(vt + dpad(l, CELL_W) + vt)
    lines.append(vt + dpad(" " + (status + " " + detail).strip(), CELL_W) + vt)
    lines.append(bl + hz * CELL_W + br)
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


def grid_per_row(width):
    """Desk columns per row — single source of truth for renderer AND navigation."""
    per_row = max(1, (width - 4) // (CELL_W + 3))
    if per_row > 2 and (width - 4) - per_row * (CELL_W + 3) < 16:
        per_row -= 1  # give up one desk column so the coffee corner fits
    return per_row


def render_frame(tick, width=92, sel=None, flash=False):
    rows = []
    t = time.strftime("%H:%M:%S")
    title = "─ 🏭 NOTIFICATOR DEV FACTORY "
    rows.append(("┌" + dpad(title, width - 13, fill="─") + f" {t} ─┐", 0))
    per_row = grid_per_row(width)
    used = 2 + per_row * (CELL_W + 3)
    coffee_on = width - used - 2 >= 14
    states = {k: agent_state(k, kind) for k, _, _, kind in ROSTER}
    breakers = [(k, e) for k, e, _, _ in ROSTER if states[k][0] == "break"]
    pos = {k: (2 + (i % per_row) * (CELL_W + 3) + 11, 1 + (i // per_row) * 8 + 4)
           for i, (k, _, _, _) in enumerate(ROSTER)}
    coffee = coffee_corner(tick, breakers, width - used - 2) if coffee_on else None
    for start in range(0, len(ROSTER), per_row):
        chunk = ROSTER[start:start + per_row]
        cells = [desk_cell(e, n, k, states[k], tick, start + i, coffee_on, selected=(start + i == sel))
                 for i, (k, e, n, _) in enumerate(chunk)]
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
        intercom, score, pending = list(STATE["intercom"]), STATE["score"], list(STATE["pending"])
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
    alrm = alarms()
    if alrm:  # no alarm → no panel
        on = flash and tick % 2 == 0
        rows.append(("│" + dpad(" ═══ 🚨 ALARMES " + ("‼ " if on else ""), width - 2, fill="═") + "│",
                     4 if on else 0))
        for _, msg in alrm:
            rows.append(("│ " + dpad(msg, width - 4) + " │", 4))
    if pending:  # rien en attente → pas de panneau
        rows.append(("│" + dpad(" ═══ 🙋 EN ATTENTE DE TOI ", width - 2, fill="═") + "│", 0))
        for row in pending[:PENDING_MAX]:
            rows.append(("│ " + dpad(row, width - 4) + " │", 6))
        if len(pending) > PENDING_MAX:
            rows.append(("│ " + dpad(f"… +{len(pending) - PENDING_MAX} autres", width - 4) + " │", 5))
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


# ── control room: zoom, log tail, summon ───────────────────────────────────

def log_tail(key, n=ZOOM_TAIL):
    """Newest log file of an agent -> (basename, last n non-empty lines)."""
    try:
        logs = [os.path.join(LOG_DIR, f) for f in os.listdir(LOG_DIR) if f.startswith(key)]
        if not logs:
            return None, []
        newest = max(logs, key=os.path.getmtime)
        with open(newest, "rb") as f:
            f.seek(0, 2)
            f.seek(max(0, f.tell() - 16384))
            data = f.read().decode(errors="replace")
        return os.path.basename(newest), [l for l in data.splitlines() if l.strip()][-n:]
    except Exception:
        return None, []


def zoom_lines(idx, tail_name, tail, follow, note, width=80):
    """Zoom panel over one desk -> lines at exact display width."""
    key, emoji, name, kind = ROSTER[idx]
    state, status, detail = agent_state(key, kind)
    with LOCK:
        svc, timers, loops = dict(STATE["svc"]), dict(STATE["timers"]), list(STATE["loops"])
    unit = ("notificator-scout" if kind == "virtual:scout-log"
            else kind.split(":")[1] if kind.startswith("svc:") else None)
    if unit:
        s = svc.get(unit, {})
        last = f"{s.get('Result') or '—'} · {s.get('ExecMainStartTimestamp') or '—'}"
    else:  # ponytail: looper roles have no systemd unit — show the live loop instead
        lp = next((l for l in loops if l["type"] == kind.split(":")[1]), None)
        last = f"{lp['status']} · {lp['target']}" if lp else "—"
    inner = width - 2
    lines = ["╔" + dpad(f" ZOOM — {emoji} {name} ", inner, center=True, fill="═") + "╗"]
    for b in (f" état : {state} · {status} {detail}".rstrip(),
              f" dernier run : {last}",
              f" prochain réveil : {timers.get(TIMER_OF.get(key, ''), '') or '—'}",
              f" log : {tail_name or '(aucun)'}  [{'follow' if follow else 'figé'}]"):
        lines.append("║" + dpad(b, inner) + "║")
    lines.append("╠" + "═" * inner + "╣")
    rows = (tail or ["(aucun log)"])[-ZOOM_TAIL:]
    for l in rows + [""] * (ZOOM_TAIL - len(rows)):
        lines.append("║" + dpad(" " + l, inner) + "║")
    summon = "s summon" if key in SUMMONABLE else "s summon (indispo)"
    foot = f" {note} · " if note else " "
    lines.append("╚" + dpad(foot + f"l follow · {summon} · Esc fermer ", inner, center=True, fill="═") + "╝")
    return lines


def prompt_summon(scr, key):
    """One-line textbox at the bottom; Enter sends, Esc cancels. -> msg or None."""
    from curses.textpad import Textbox
    h, w = scr.getmaxyx()
    if h < 6 or w < 24:
        return None
    ww = min(76, w - 4)
    win = curses.newwin(3, ww, h - 4, 2)
    win.border()
    win.addstr(0, 2, dpad(f" ✉ {key} — Enter envoie · Esc annule ", ww - 4))
    edit = win.derwin(1, ww - 4, 1, 2)
    win.refresh()
    curses.curs_set(1)
    cancelled = []

    def keyfilter(ch):
        if ch == 27:
            cancelled.append(True)
            return 7  # Ctrl-G ends Textbox.edit
        return 7 if ch in (10, 13) else ch

    msg = Textbox(edit).edit(keyfilter).strip()
    curses.curs_set(0)
    return None if cancelled else (msg or None)


def send_summon(key, msg):
    try:
        r = subprocess.run([SUMMON_SH, key, msg], capture_output=True, text=True, timeout=15)
        return "✉ envoyé" if r.returncode == 0 else "✗ " + (r.stderr or r.stdout or "échec").strip()[:50]
    except Exception as e:
        return "✗ " + str(e)[:50]


def klaxon(tick):
    """Beep once per new alarm; -> True while the title should flash. Curses path only."""
    global ALARM_SEEN, ALARM_FLASH
    keys = {k for k, _ in alarms()}
    if ALARM_SEEN is not None and keys - ALARM_SEEN:
        ALARM_FLASH = tick
        curses.beep()
    ALARM_SEEN = keys
    return ALARM_FLASH is not None and tick - ALARM_FLASH < KLAXON_TICKS


def main_curses(scr):
    curses.curs_set(0)
    scr.nodelay(True)
    curses.start_color()
    curses.use_default_colors()
    for i, fg in ((1, curses.COLOR_GREEN), (2, curses.COLOR_YELLOW), (3, curses.COLOR_BLUE),
                  (4, curses.COLOR_RED), (5, curses.COLOR_CYAN), (6, curses.COLOR_MAGENTA)):
        curses.init_pair(i, fg, -1)
    tick, sel, zoom, follow, frozen, note = 0, 0, None, True, (None, []), ""
    while True:
        ch = scr.getch()
        h, w = scr.getmaxyx()
        width = min(w - 1, 120)
        per_row = grid_per_row(width)  # must match render_frame's column count or Up/Down drifts
        if ch == ord("q"):
            return
        if zoom is None:
            if ch == 27:
                return
            if ch == curses.KEY_LEFT:
                sel = (sel - 1) % len(ROSTER)
            elif ch == curses.KEY_RIGHT:
                sel = (sel + 1) % len(ROSTER)
            elif ch == curses.KEY_UP:
                sel = (sel - per_row) % len(ROSTER)
            elif ch == curses.KEY_DOWN:
                sel = (sel + per_row) % len(ROSTER)
            elif ch in (curses.KEY_ENTER, 10, 13):
                zoom, follow, note = sel, True, ""
        else:
            if ch == 27:
                zoom = None
            elif ch == ord("l"):
                follow = not follow
                if not follow:
                    frozen = log_tail(ROSTER[zoom][0])
            elif ch == ord("s") and ROSTER[zoom][0] in SUMMONABLE:
                msg = prompt_summon(scr, ROSTER[zoom][0])
                if msg:
                    note = send_summon(ROSTER[zoom][0], msg)
        scr.erase()
        for y, (line, color) in enumerate(render_frame(tick, width, sel, flash=klaxon(tick))):
            if y >= h - 1:
                break
            try:
                scr.addstr(y, 0, line, curses.color_pair(color) if color else 0)
            except curses.error:
                pass
        if zoom is not None:
            # ponytail: re-read the tail every frame in follow mode — small seek'd read, dev tool
            tail_name, tail = log_tail(ROSTER[zoom][0]) if follow else frozen
            zl = zoom_lines(zoom, tail_name, tail, follow, note, min(80, max(24, w - 4)))
            y0, x0 = max(0, (h - len(zl)) // 2), max(0, (w - dwidth(zl[0])) // 2)
            for i, line in enumerate(zl):
                try:
                    scr.addstr(y0 + i, x0, line, curses.color_pair(5))
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
    # a real merge-ready PR still carries `looper:review` — the reviewer agent never
    # clears it — so the positive fixture has to as well, or the filter is untested
    ready = {"number": 72, "labels": [{"name": "qa:passed"}, {"name": "looper:review"},
                                      {"name": "ready-to-merge"}],
             "mergeable": "MERGEABLE", "isDraft": False, "updatedAt": "2026-01-01T20:00:00Z"}
    pending_prs = [ready,
                   dict(ready, number=73, isDraft=True),
                   dict(ready, number=74, mergeable="CONFLICTING"),
                   dict(ready, number=75, labels=ready["labels"] + [{"name": "qa:failed"}]),
                   dict(ready, number=76, labels=ready["labels"] + [{"name": "looper:spec-reviewing"}]),
                   dict(ready, number=77, labels=[{"name": "qa:passed"}, {"name": "looper:review"}]),
                   dict(ready, number=78, updatedAt="2026-01-01T23:00:00Z")]
    pending_issues = [{"number": 67, "title": "daily reports", "labels": [{"name": "looper:hold"}],
                       "updatedAt": "2025-12-30T00:00:00Z"},
                      {"number": 68, "title": "libre", "labels": [{"name": "agent:proposed"}],
                       "updatedAt": "2025-12-30T00:00:00Z"}]
    pend = compute_pending(pending_prs, pending_issues, now)
    # #72/#78 are the regression that matters: green gates *and* the sticky review lane
    if pend != ["⛔ #67 daily reports — attend ton go", "🚢 PR#72 prête à merger — 4h",
                "🚢 PR#78 prête à merger — 1h"]:
        print(f"FAIL compute_pending: {pend}")
        fails += 1
    if compute_pending([], [], now) != []:
        print("FAIL compute_pending: nothing pending should be empty")
        fails += 1
    # a long issue title must not eat the "— attend ton go" marker
    long_row = compute_pending([], [dict(pending_issues[0], title="t" * 200)], now)[0]
    if not long_row.endswith(" — attend ton go") or len(long_row) > 80:
        print(f"FAIL compute_pending: long title not bounded: {long_row!r}")
        fails += 1
    with LOCK:
        STATE.update(prs=["PR#0 🧪qa✗"], issues="issues: 0", err="boom", ticker="x" * 300,
                     pending=pend + [f"⛔ #{n} titre très long ✨ {'é' * 60} — attend ton go" for n in range(80, 85)],
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
    # pending tray: capped with an overflow marker, absent when the queue is empty
    body = "\n".join(l for l, _ in render_frame(1, 92))
    if "🙋 EN ATTENTE DE TOI" not in body or "… +3 autres" not in body:
        print("FAIL pending panel: header or overflow marker missing")
        fails += 1
    with LOCK:
        STATE["pending"] = []
    if "EN ATTENTE" in "\n".join(l for l, _ in render_frame(1, 92)):
        print("FAIL pending panel rendered with nothing pending")
        fails += 1
    # control room: selected border and zoom panel keep every row at exact width
    for line, _ in render_frame(3, 92, sel=5):
        if dwidth(line) != 92:
            print(f"FAIL sel row {dwidth(line)} cols: {line!r}")
            fails += 1
    for idx, follow, note in ((0, True, "✉ envoyé"), (1, False, ""), (2, True, "")):
        zl = zoom_lines(idx, "scout-1.log", ["x" * 200, "ok ✓"], follow, note, 80)
        if len(zl) != 7 + ZOOM_TAIL:  # top + 4 info + sep + 15 tail + bottom
            print(f"FAIL zoom height {len(zl)}")
            fails += 1
        for line in zl:
            if dwidth(line) != 80:
                print(f"FAIL zoom row {dwidth(line)} cols: {line!r}")
                fails += 1
    # alarm panel: one failed unit + one stalled loop, both widths, flashing title
    now = time.time()
    with LOCK:
        STATE["svc"] = {"notificator-qa": {
            "Id": "notificator-qa.service", "ActiveState": "failed", "Result": "exit-code",
            "ExecMainStatus": "2", "NRestarts": "3",
            "ExecMainExitTimestamp": time.strftime("%a %Y-%m-%d %H:%M:%S %Z", time.localtime(now - 11520))}}
        STATE["loops"] = [{"type": "worker", "target": "SoulKyu/notificator#57",
                           "step": "implement", "status": "running"}]
    LOOP_SINCE[("worker", "SoulKyu/notificator#57")] = ("implement", now - 2820)
    a = alarms(now)
    if not (len(a) == 2 and "qa" in a[0][1] and "exit 2" in a[0][1] and "3h12" in a[0][1]
            and "#57" in a[1][1] and "implement" in a[1][1] and "47min" in a[1][1]):
        print(f"FAIL alarms: {a}")
        fails += 1
    for w in (92, 60):
        for tick in range(4):
            for line, _ in render_frame(tick, w, flash=True):
                if dwidth(line) != w:
                    print(f"FAIL alarm row {dwidth(line)} cols (want {w}): {line!r}")
                    fails += 1
    with LOCK:  # unit back to success + loop step moved on → alarms gone, panel gone
        STATE["svc"]["notificator-qa"]["Result"] = "success"
    LOOP_SINCE[("worker", "SoulKyu/notificator#57")] = ("publish", now)
    if alarms(now) or any("ALARMES" in l for l, _ in render_frame(0, 92)):
        print("FAIL alarm panel rendered with no alarm")
        fails += 1
    # navigation stride == renderer column count (coffee-corner decrement included)
    for w, want in ((92, 3), (113, 4), (120, 4)):
        if grid_per_row(w) != want:
            print(f"FAIL grid_per_row({w}) = {grid_per_row(w)} (want {want})")
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
