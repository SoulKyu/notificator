#!/usr/bin/env python3
"""NOTIFICATOR DEV FACTORY — gamified real-time view of the agent office.

Little ASCII people at their desks: screens scroll when they code, coffee
steams on breaks, zzZ floats while they sleep, and screens flash on failures.
Zero dependencies (stdlib curses). Data: `looper ps --json`, systemd user
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
import tempfile
import threading
import time
import unicodedata

LOG_DIR = os.environ.get("FACTORY_LOG_DIR", os.path.expanduser("~/.claude-agents/notificator/logs"))
# looper keeps its own role logs out of LOG_DIR, one directory per run
LOOPER_LOG_DIR = os.environ.get("FACTORY_LOOPER_LOG_DIR", os.path.expanduser("~/.looper/logs/loops"))
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
LOG_PREFIX_OF = {"rebaser": "rebase"}  # desks whose log files are not named after their ROSTER key
SUMMONABLE = {"scout", "roast", "qa", "rebaser", "groomer"}
SUMMON_SH = os.path.expanduser("~/.claude-agents/notificator/summon.sh")
ZOOM_TAIL = 15


def stall_min(v):
    """FACTORY_STALL_MIN in minutes. A typo'd tuning knob ('45m') must not take the
    whole dashboard down at import time; 0 would flag every fresh loop as stalled."""
    try:
        return max(1, int(v or 30))
    except ValueError:
        return 30


STALL_MIN = stall_min(os.environ.get("FACTORY_STALL_MIN"))

STATE = {"loops": [], "svc": {}, "timers": {}, "prs": [], "issues": "", "ticker": "", "err": "",
         "mail_pending": {}, "intercom": [], "score": None, "events": [], "pending": [],
         "ps_ok": True}
PENDING_MAX = 5
LOCK = threading.Lock()

# one-shot animations, consumed by the render loop (render-side state only)
ANIM = {"mail": [], "party": []}
MAIL_TICKS, PARTY_TICKS = 4, 12  # ~1 s flight, ~3 s banner at 4 fps
MAIL_SEEN = None   # (box, filename) pairs already counted — None until first poll
PR_PREV = None     # {number: title} of open PRs at previous poll
LOOP_SINCE = {}    # (type, target) -> (step, first_seen_ts) — stall detection
PS_FAIL = 0        # consecutive failed `looper ps` polls
PS_FAIL_MIN = 2    # ... before the klaxon: one hiccup every ~20 min would beep all day
ALARM_SEEN = None  # alarm keys already klaxonned — None until the first curses frame
ALARM_FLASH = None # tick of the last new alarm; the title flashes for KLAXON_TICKS
KLAXON_TICKS = 12  # ~3 s at 4 fps
ALARM_ROWS = 5     # panel height cap; the tail is summarised in one extra row


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

def iso_ts(s):
    """'2026-07-25T05:20:55.942Z' -> epoch (UTC), or None."""
    try:
        return calendar.timegm(time.strptime((s or "")[:19], "%Y-%m-%dT%H:%M:%S"))
    except Exception:
        return None


def track_loops(loops, now, prune=True):
    """Refresh LOOP_SINCE. prune=False when the poll produced no usable output — a
    transient `looper ps` failure must not restart every stall clock at zero."""
    with LOCK:
        live = set()
        for lp in loops:
            k = (lp["type"], lp["target"])
            live.add(k)
            if lp["status"] != "running":  # queued/paused waiting is not stall time
                LOOP_SINCE.pop(k, None)
                continue
            if k not in LOOP_SINCE:  # first sighting: trust looper's clock, not ours
                LOOP_SINCE[k] = (lp["step"], lp.get("since") or now)
            elif LOOP_SINCE[k][0] != lp["step"]:
                LOOP_SINCE[k] = (lp["step"], now)
        if prune:
            for k in set(LOOP_SINCE) - live:
                del LOOP_SINCE[k]


def parse_ps(out):
    """`looper ps --json` rows → (loop dicts, parsed?). An older CLI, or one writing a
    diagnostic to stdout, must cost the loop desks only — never the systemd half of
    poll_fast. Parse success is the signal, not emptiness: sh() hands back stdout even on
    a non-zero exit, and a loop with no `type` is not renderable, so a schema drift reads
    as "injoignable" rather than as a calm, frozen factory."""
    try:
        return [{"type": i["type"], "target": (i.get("target") or {}).get("label") or "?",
                 "step": i.get("currentStep") or "-",
                 # displayStatus refines `status`: a loop parked on manual_intervention
                 # is merely `paused` in `status`, and that is the one state a human owes work to
                 "status": i.get("displayStatus") or i.get("status") or "-",
                 "since": iso_ts((i.get("agent") or {}).get("startedAt")),
                 "loopId": i.get("loopId"), "runId": i.get("runId")}
                for i in json.loads(out)["items"]], True
    except Exception:
        return [], False


def poll_fast():
    # --json over the table: no positional column parsing, and it carries loopId/runId,
    # which is the only way to find a looper role's own log (see loop_tail).
    loops, ps_ok = parse_ps(sh("looper ps --json 2>/dev/null"))
    svc = {}
    units = " ".join(k.split(":")[1] + ".service" for _, _, _, k in ROSTER if k.startswith("svc:"))
    out = sh(f"systemctl --user show {units} "
             "-p Id,ActiveState,Result,ExecMainStartTimestamp,ExecMainExitTimestamp,"
             "ExecMainStatus,ExecMainCode,NRestarts 2>/dev/null")
    cur = {}
    for line in out.splitlines() + [""]:
        if not line.strip():
            if "Id" in cur:
                svc[cur["Id"].replace(".service", "")] = cur
            cur = {}
        elif "=" in line:
            k, v = line.split("=", 1)
            cur[k] = v
    global PS_FAIL
    PS_FAIL = 0 if ps_ok else PS_FAIL + 1
    track_loops(loops, time.time(), prune=ps_ok)  # raw ps_ok: one failure still keeps the clocks
    with LOCK:
        STATE["loops"], STATE["svc"] = loops, svc
        # ... but the alarm waits for PS_FAIL_MIN in a row: the daemon restarts and
        # self-upgrades, and beeping for something that healed 3 s ago is alert fatigue
        STATE["ps_ok"] = PS_FAIL < PS_FAIL_MIN


def ago(seconds, fine=False):
    """Compact French age. Rounded (42min · 4h · 3j) for GitHub ages; fine=True
    (42s · 47min · 3h12 · 2j) where the exact duration is the signal — a stall clock."""
    s = max(0, int(seconds))
    if s < 60:
        return f"{s}s" if fine else "0min"
    if s < 3600:
        return f"{s // 60}min"
    if s < 86400:
        return f"{s // 3600}h{s % 3600 // 60:02d}" if fine else f"{s // 3600}h"
    return f"{s // 86400}j"


def systemd_ts(s):
    """'Sat 2026-07-25 04:00:12 CEST' -> epoch (local tz), or None."""
    m = re.search(r"\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}", s or "")
    try:
        return time.mktime(time.strptime(m.group(0), "%Y-%m-%d %H:%M:%S")) if m else None
    except Exception:
        return None


def alarms(now=None):
    """-> [(stable key, row)] for a dead looper, failed units, loops waiting on a human
    and loops stalled past FACTORY_STALL_MIN."""
    now = now or time.time()
    with LOCK:
        svc, loops, since_of = dict(STATE["svc"]), list(STATE["loops"]), dict(LOOP_SINCE)
        ps_ok = STATE["ps_ok"]
    out = []
    if not ps_ok:  # otherwise every looper desk renders a calm "veille" and nothing alarms
        out.append(("looper:down", "🚨 looper injoignable — état des loops inconnu"))
    for unit, s in sorted(svc.items()):
        if s.get("Result") in ("success", "", None):
            continue
        t = systemd_ts(s.get("ExecMainExitTimestamp"))
        age = f" il y a {ago(now - t, fine=True)}" if t else ""
        n = int(s.get("NRestarts") or "0")
        restarts = f" · {n} redémarrage{'s' if n > 1 else ''}" if n else ""
        # ExecMainCode is si_code: 0 (or unset) means no main process ever exited, so
        # ExecMainStatus carries nothing and Result is the whole story; 2/3 = killed/dumped,
        # and then the number is a signal, not an exit code
        raw = s.get("ExecMainStatus") or ""
        detail = "" if not raw or s.get("ExecMainCode") == "0" else \
            f" ({'signal' if s.get('ExecMainCode') in ('2', '3') else 'exit'} {raw})"
        out.append((f"unit:{unit}",
                    f"🔥 {unit.replace('notificator-', '')} — échec {s['Result']}"
                    f"{detail}{age}{restarts}"))
    # the one alarm where the operator is the blocker: track_loops() drops non-running
    # loops, so manual_intervention can never surface as a stall row
    for lp in sorted(loops, key=lambda l: (l["type"], l["target"])):
        if lp["status"] != "manual_intervention":
            continue
        out.append((f"halt:{lp['type']}:{lp['target']}",
                    f"✋ {lp['type']} {lp['target'].split('/')[-1]} attend une intervention humaine"))
    for (typ, target), (step, since) in sorted(since_of.items()):
        lp = next((l for l in loops if l["type"] == typ and l["target"] == target), None)
        if not lp or lp["status"] != "running" or now - since < STALL_MIN * 60:
            continue
        # clock first: dpad() truncates from the right, and the age is the one number
        # this alarm exists for — the step name is what a narrow frame may safely eat
        out.append((f"loop:{typ}:{target}",
                    f"⏳ {ago(now - since, fine=True)} — {typ} {target.split('/')[-1]} bloqué sur {step}"))
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
        t = iso_ts(p.get("updatedAt")) or now
        rows.append((t, f"🚢 PR#{p['number']} prête à merger — {ago(now - t)}"))
    for i in issues:
        if not any(l["name"] == "looper:hold" for l in i.get("labels", [])):
            continue
        t = iso_ts(i.get("updatedAt")) or now
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
        t = iso_ts(i.get("createdAt"))
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
        created, merged = iso_ts(p.get("createdAt")), iso_ts(p.get("mergedAt"))
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


def loop_num(lp):
    """Loop number from a `looper ps` target — `owner/repo#81` and `owner/repo/81` both -> `81`."""
    return re.split(r"[/#]", lp["target"])[-1]


def loop_state(lp):
    """One `looper ps` row -> (state, status, detail)"""
    tgt = loop_num(lp)
    if lp["status"] == "running":
        return "work", lp["step"], tgt
    if lp["status"] == "queued":
        return "wait", "en file", tgt
    return "wait", lp["status"], tgt


def agent_state(key, kind):
    """-> (state, status, detail)"""
    with LOCK:
        loops, svc, timers, ticker = STATE["loops"], dict(STATE["svc"]), dict(STATE["timers"]), STATE["ticker"]
    nxt = timers.get(TIMER_OF.get(key, ""), "")
    if kind.startswith("looper:"):
        role = kind.split(":")[1]
        for lp in loops:
            if lp["type"] == role:
                return loop_state(lp)
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


def loop_order(lp):
    """Sort key giving desks a stable order — `looper ps` row order is not guaranteed."""
    tail = loop_num(lp)
    return (0, int(tail), "") if tail.isdigit() else (1, 0, lp["target"])


def build_desks(max_desks=None, keep=None):
    """ROSTER expanded for the current frame: a looper role running N>1 loops
    concurrently gets one desk per loop (WORKER·1, WORKER·2…). N<=1 → one desk.
    `max_desks` caps the grid so a busy office can't push the panels below it off
    screen; desks that don't fit fold into the last desk of their role (`WORKER·2+3`).
    `keep` is a desk id the cap must never trim — the zoom panel is a centred overlay
    that needs no grid slot, so running out of grid rows must not close it mid-read."""
    with LOCK:
        loops = list(STATE["loops"])
    desks = []
    for key, emoji, name, kind in ROSTER:
        role = kind.split(":")[1] if kind.startswith("looper:") else None
        mine = sorted((lp for lp in loops if lp["type"] == role), key=loop_order) if role else []
        if len(mine) > 1:
            # `·N` is a label (a rank); `id` is the loop itself, so lookups survive a sibling
            # finishing. loopId over target: parse_ps defaults target to "?" for targetless rows
            # (queued loops), and two of those would share an id and collide in desk_index.
            # ponytail: no loopId (older looper CLI) → fall back to rank, which is unique within
            # the frame but not stable across them; a wrong-desk zoom is worse than a jumpy one.
            desks += [{"key": key,
                       "id": f"{key}:{lp['loopId']}" if lp.get("loopId") else f"{key}:{lp['target']}#{i}",
                       "emoji": emoji,
                       "name": f"{name}·{i + 1}", "kind": kind,
                       "state": loop_state(lp), "loop": lp} for i, lp in enumerate(mine)]
        else:
            desks.append({"key": key, "id": key, "emoji": emoji, "name": name, "kind": kind,
                          "state": agent_state(key, kind), "loop": mine[0] if mine else None})
    if max_desks is None or len(desks) <= max_desks:
        return desks
    # Trim the widest role first, highest rank first; the roster itself is never trimmed.
    # ponytail: O(desks²) rescan — desks stay in the dozens, and it keeps ROSTER order from
    # deciding who shrinks (scanning backwards once would always cost WORKER its desks first).
    hidden, floor = {}, max(max_desks, len(ROSTER))
    while len(desks) > floor:
        counts = {}
        for d in desks:
            counts[d["key"]] = counts.get(d["key"], 0) + 1
        key = max(counts, key=lambda k: counts[k])  # ties → ROSTER order, so it stays deterministic
        if counts[key] < 2:
            break
        victim = max((j for j, d in enumerate(desks) if d["key"] == key and d["id"] != keep),
                     default=None)
        if victim is None:
            break
        hidden[key] = hidden.get(key, 0) + 1
        desks.pop(victim)
    for key, n in hidden.items():
        last = max(j for j, d in enumerate(desks) if d["key"] == key)
        desks[last]["name"] += f"+{n}"
    return desks


CELL_W = 20  # inner width of a desk cell


def desk_cell(emoji, name, key, state3, tick, seed, coffee_on, selected=False):
    """-> (8 display lines of width CELL_W+2, color). `key` None → no 📬 badge: mail_pending
    counts a role, not a desk, so only a role's first desk carries it (see render_frame)."""
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


def panel_snapshot():
    """One read of everything drawn below the office. desk_cap budgets for these rows and
    render_frame emits them, so both must see the same values: reading STATE twice leaves the
    unlocked build_desks/key-dispatch/office loop in between, and a poll landing in that window
    makes the frame taller than the budget it was granted."""
    with LOCK:
        p = {"prs": list(STATE["prs"]), "issues": STATE["issues"], "ticker": STATE["ticker"],
             "err": STATE["err"], "intercom": list(STATE["intercom"]),
             "score": STATE["score"], "pending": list(STATE["pending"])}
    p["alarms"] = alarms()  # outside the `with`: alarms() takes LOCK itself and it is not reentrant
    return p


def alarm_shown(alrm):
    """Alarm rows render_frame emits before the summarising tail. Shared with panel_rows so the
    budget and the renderer cannot drift: at exactly one row over the cap the tail would occupy
    the row it displaced and say strictly less, so summarise only when it buys space back."""
    return ALARM_ROWS if len(alrm) > ALARM_ROWS + 1 else len(alrm)


def panel_rows(p):
    """Non-office rows of the frame about to be drawn, from a panel_snapshot. Five of the six
    panels are conditional in render_frame, so a worst-case constant charges ~23 rows that are
    usually not drawn and pins the cap at len(ROSTER) below a 55-row terminal — the dynamic desks
    would never appear."""
    n = 1 + 3 + 1 + 1  # title + wall board (header + PRs + issues) + radio + bottom border
    if p["intercom"]:
        n += 1 + len(p["intercom"])
    if p["score"]:
        n += 2 if p["score"] == "err" else 4
    if p["alarms"]:
        shown = alarm_shown(p["alarms"])
        n += 1 + shown + (len(p["alarms"]) > shown)
    if p["pending"]:
        n += 1 + min(len(p["pending"]), PENDING_MAX) + (len(p["pending"]) > PENDING_MAX)
    if p["err"]:
        n += 1
    return n


def desk_cap(h, per_row, p):
    """Desks that still leave room for the panels below the office — an unbounded grid silently
    swallows the wall board and the radio. Budget is h-1: main_curses stops drawing at h-1, so
    on `h - panels` landing on a multiple of 8 an h-row frame loses its closing border."""
    return max(len(ROSTER), max(1, (h - 1 - panel_rows(p)) // 8) * per_row)


def render_frame(tick, width=92, sel=None, desks=None, panels=None, flash=False):
    desks = build_desks() if desks is None else desks
    rows = []
    t = time.strftime("%H:%M:%S")
    title = "─ 🏭 NOTIFICATOR DEV FACTORY "
    rows.append(("┌" + dpad(title, width - 13, fill="─") + f" {t} ─┐", 0))
    per_row = grid_per_row(width)
    used = 2 + per_row * (CELL_W + 3)
    coffee_on = width - used - 2 >= 14
    breakers = [(d["key"], d["emoji"]) for d in desks if d["state"][0] == "break"]
    pos, badge, seen = {}, [], set()
    for i, d in enumerate(desks):  # mail flies to a role's first desk — the badge follows it there,
        pos.setdefault(d["key"], (2 + (i % per_row) * (CELL_W + 3) + 11, 1 + (i // per_row) * 8 + 4))
        badge.append(None if d["key"] in seen else d["key"])  # …not onto all N desks of the role,
        seen.add(d["key"])  # which would read as N queued messages. `seen` spans grid rows.
    coffee = coffee_corner(tick, breakers, width - used - 2) if coffee_on else None
    for start in range(0, len(desks), per_row):
        chunk = desks[start:start + per_row]
        cells = [desk_cell(d["emoji"], d["name"], badge[start + i], d["state"], tick, start + i,
                           coffee_on, selected=(start + i == sel)) for i, d in enumerate(chunk)]
        for li in range(8):
            line = "│ "
            for cl, _ in cells:
                line += cl[li] + " "
            if start == 0 and coffee:
                line = dpad(line, used) + coffee[li]
            rows.append((dpad(line, width - 1) + "│", 0))
    office_rows = (len(desks) + per_row - 1) // per_row
    p = panel_snapshot() if panels is None else panels  # caller's snapshot: the one desk_cap budgeted
    prs, issues, ticker, err = p["prs"], p["issues"], p["ticker"], p["err"]
    intercom, score, pending = p["intercom"], p["score"], p["pending"]
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
    alrm = p["alarms"]  # from the snapshot: an alarm raised mid-frame must not outgrow the budget
    if alrm:  # no alarm → no panel
        on = flash and tick % 2 == 0
        rows.append(("│" + dpad(" ═══ 🚨 ALARMES " + ("‼ " if on else ""), width - 2, fill="═") + "│",
                     4 if on else 0))
        # capped: a machine-wide breakage must not push the board and the ⚠ err line off-screen
        shown = alarm_shown(alrm)
        for _, msg in alrm[:shown]:
            rows.append(("│ " + dpad(msg, width - 4) + " │", 4))
        if len(alrm) > shown:
            hid = alrm[shown:]            # say *what* was swallowed: stall rows always sort last
            n_u = sum(1 for k, _ in hid if k.startswith("unit:"))
            n_h = sum(1 for k, _ in hid if k.startswith("halt:"))
            n_l = sum(1 for k, _ in hid if k.startswith("loop:"))
            s_u, s_h, s_l = ("s" if n > 1 else "" for n in (n_u, n_h, n_l))
            tail = " · ".join(p for p in (f"{n_u} unité{s_u} en échec" if n_u else "",
                                          f"{n_h} loop{s_h} en attente humaine" if n_h else "",
                                          f"{n_l} loop{s_l} bloquée{s_l}" if n_l else "") if p)
            rows.append(("│ " + dpad(f"… +{len(hid)} autres alarmes ({tail})", width - 4) + " │", 4))
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

def newest_tail(paths, n):
    """Newest of `paths` -> (basename, last n non-empty lines). Reads the tail only."""
    try:
        newest = max(paths, key=os.path.getmtime)
        with open(newest, "rb") as f:
            f.seek(0, 2)
            f.seek(max(0, f.tell() - 16384))
            data = f.read().decode(errors="replace")
        return os.path.basename(newest), [l for l in data.splitlines() if l.strip()][-n:]
    except Exception:
        return None, []


def log_tail(key, n=ZOOM_TAIL):
    """Newest log file of an agent in LOG_DIR -> (basename, last n non-empty lines)."""
    try:
        return newest_tail([os.path.join(LOG_DIR, f) for f in os.listdir(LOG_DIR) if f.startswith(key)], n)
    except Exception:
        return None, []


def loop_tail(lp, n=ZOOM_TAIL):
    """Agent stdout of one looper run. looper writes these under LOOPER_LOG_DIR/<loopId>/<runId>/,
    never into LOG_DIR, so a role-prefixed filename in LOG_DIR would never match a looper desk."""
    try:
        d = os.path.join(LOOPER_LOG_DIR, lp["loopId"], lp["runId"])
        return newest_tail([os.path.join(d, f) for f in os.listdir(d) if f.endswith(".stdout.log")], n)
    except Exception:  # missing loopId/runId (older looper) or run already reaped
        return None, []


def desk_index(desks, ident):
    """Position of a desk by stable id, or None. Selection must follow the loop, not position or
    rank: desks shift mid-list, and `WORKER·2` renames onto another loop when a sibling finishes."""
    return next((i for i, d in enumerate(desks) if d["id"] == ident), None)


def desk_resolve(desks, ident, fallback):
    """Selection follows its loop; when that loop is gone, its role's desk; else the clamped
    position. The bare clamp would park the highlight on an unrelated role, since the list
    shrinks mid-list (three worker desks collapsing to one makes index 6 the reviewer)."""
    idx = desk_index(desks, ident)
    if idx is None and ident:
        idx = next((i for i, d in enumerate(desks) if d["key"] == ident.split(":", 1)[0]), None)
    return idx if idx is not None else min(fallback, len(desks) - 1)


def desk_tail(desk, n=ZOOM_TAIL):
    """A desk sitting on a loop tails that run's own agent log; the others tail the newest
    LOG_DIR log of their role. No cross-fallback — the two roots never hold each other's logs."""
    if desk["loop"]:
        return loop_tail(desk["loop"], n)
    return log_tail(LOG_PREFIX_OF.get(desk["key"], desk["key"]), n)


def zoom_lines(desk, tail_name, tail, follow, note, width=80):
    """Zoom panel over one desk -> lines at exact display width."""
    key, emoji, name, kind = desk["key"], desk["emoji"], desk["name"], desk["kind"]
    state, status, detail = desk["state"]
    lp = desk["loop"]
    with LOCK:
        svc, timers = dict(STATE["svc"]), dict(STATE["timers"])
    unit = ("notificator-scout" if kind == "virtual:scout-log"
            else kind.split(":")[1] if kind.startswith("svc:") else None)
    if unit:
        s = svc.get(unit, {})
        last = f"{s.get('Result') or '—'} · {s.get('ExecMainStartTimestamp') or '—'}"
    else:  # ponytail: looper roles have no systemd unit — show this desk's loop instead
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
    tick, sel, follow, frozen, note = 0, 0, True, (None, []), ""
    sel_id = zoom_id = None
    while True:
        ch = scr.getch()
        h, w = scr.getmaxyx()
        width = min(w - 1, 120)
        per_row = grid_per_row(width)  # must match render_frame's column count or Up/Down drifts
        # desks appear/disappear mid-list with concurrent loops — re-resolve by id, not position.
        # A desk vanishing auto-closes the zoom and swallows this frame's keystroke, so an Esc
        # meaning "close the panel" can't fall through to the navigation branch and quit the TUI.
        panels = panel_snapshot()  # one read per frame: the cap and the frame must agree
        desks = build_desks(desk_cap(h, per_row, panels), keep=zoom_id)
        sel = desk_resolve(desks, sel_id, sel)
        if ch == ord("q"):  # above the swallow below: q quits in both modes, unconditionally
            return
        zoom = desk_index(desks, zoom_id) if zoom_id else None
        if zoom is None and zoom_id:
            zoom_id, ch = None, -1  # -1 is getch()'s "no key" in nodelay mode → every branch no-ops
        if zoom is None:
            if ch == 27:
                return
            if ch == curses.KEY_LEFT:
                sel = (sel - 1) % len(desks)
            elif ch == curses.KEY_RIGHT:
                sel = (sel + 1) % len(desks)
            elif ch == curses.KEY_UP:
                sel = (sel - per_row) % len(desks)
            elif ch == curses.KEY_DOWN:
                sel = (sel + per_row) % len(desks)
            elif ch in (curses.KEY_ENTER, 10, 13):
                zoom, zoom_id, follow, note = sel, desks[sel]["id"], True, ""
            sel_id = desks[sel]["id"]
        else:
            if ch == 27:
                zoom, zoom_id = None, None
            elif ch == ord("l"):
                follow = not follow
                if not follow:
                    frozen = desk_tail(desks[zoom])
            elif ch == ord("s") and desks[zoom]["key"] in SUMMONABLE:
                msg = prompt_summon(scr, desks[zoom]["key"])
                if msg:
                    note = send_summon(desks[zoom]["key"], msg)
        scr.erase()
        for y, (line, color) in enumerate(render_frame(tick, width, sel, desks, panels,
                                                       flash=klaxon(tick))):
            if y >= h - 1:
                break
            try:
                scr.addstr(y, 0, line, curses.color_pair(color) if color else 0)
            except curses.error:
                pass
        if zoom is not None:
            # ponytail: re-read the tail every frame in follow mode — small seek'd read, dev tool
            tail_name, tail = desk_tail(desks[zoom]) if follow else frozen
            zl = zoom_lines(desks[zoom], tail_name, tail, follow, note, min(80, max(24, w - 4)))
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
    global STALL_MIN
    STALL_MIN = 30  # pin the knob: alarms() and render_frame() both read it, fixtures assume 30
    fails = 0
    if (stall_min("45m"), stall_min(None), stall_min("0"), stall_min("45")) != (30, 30, 1, 45):
        print(f"FAIL stall_min: junk knob must fall back, not crash: {stall_min('45m')}")
        fails += 1
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
    desks = build_desks()
    for idx, follow, note in ((0, True, "✉ envoyé"), (1, False, ""), (2, True, "")):
        zl = zoom_lines(desks[idx], "scout-1.log", ["x" * 200, "ok ✓"], follow, note, 80)
        if len(zl) != 7 + ZOOM_TAIL:  # top + 4 info + sep + 15 tail + bottom
            print(f"FAIL zoom height {len(zl)}")
            fails += 1
        for line in zl:
            if dwidth(line) != 80:
                print(f"FAIL zoom row {dwidth(line)} cols: {line!r}")
                fails += 1
    # stall bookkeeping: seeded from looper's clock, survives a transient `looper ps` failure
    now = time.time()
    k57 = ("worker", "SoulKyu/notificator#57")
    lp57 = {"type": "worker", "target": "SoulKyu/notificator#57", "step": "implement",
            "status": "running", "since": now - 2820}
    LOOP_SINCE.clear()
    track_loops([lp57], now)
    track_loops([dict(lp57, step="review")], now + 3)  # step change → clock restarts
    if LOOP_SINCE.get(k57) != ("review", now + 3):
        print(f"FAIL track_loops: step change did not restart the clock: {LOOP_SINCE}")
        fails += 1
    # poll_fast() is where `prune` is derived, so drive it — a failed `looper ps` must
    # keep the clocks, an honestly empty one must prune.
    PS_OK = json.dumps({"items": [{"type": "worker", "status": "running", "currentStep": "implement",
                                   "target": {"label": "SoulKyu/notificator#57"},
                                   "agent": {"startedAt": "2026-01-02T00:00:00Z"}}]})
    SYSD = "Id=notificator-qa.service\nActiveState=active\nResult=success\n\n"
    real_sh = sh
    def stub(ps):
        def fake(cmd, timeout=20):
            if cmd.startswith("looper ps"):
                return ps
            # mimic `systemctl show -p`: only the requested properties come back, so dropping
            # one from the -p list breaks this fixture instead of quietly changing a row
            want = set(re.search(r"-p ([\w,]+)", cmd).group(1).split(","))
            return "".join(l + "\n" for l in SYSD.splitlines()
                           if not l.strip() or l.split("=", 1)[0] in want)
        globals()["sh"] = fake
    PS_QUEUED = json.dumps({"items": [{"type": "worker", "status": "queued", "currentStep": "implement",
                                       "target": {"label": "SoulKyu/notificator#57"}, "agent": None}]})
    PS_SCHEMA = json.dumps({"items": [{"kind": "worker", "status": "running",  # `type` renamed upstream
                                       "currentStep": "implement"}]})
    for name, ps, keep, ps_ok in (("present", PS_OK, True, True),
                                  ("timeout", "", True, False),                             # sh() failure path
                                  ("garbage", "looper: daemon unreachable\n", True, False),  # non-JSON stdout
                                  ("schema", PS_SCHEMA, True, False),                        # valid JSON, no `type`
                                  ("gone", '{"items": []}', False, True),                    # really gone → prune
                                  ("queued", PS_QUEUED, False, True)):  # waiting in the queue is not stalling
        stub(ps)
        LOOP_SINCE.clear()
        LOOP_SINCE[k57] = ("implement", now - 2820)
        globals()["PS_FAIL"] = 0
        for _ in range(1 if ps_ok else PS_FAIL_MIN):  # a failure only alarms once debounced
            poll_fast()
        if (LOOP_SINCE.get(k57) == ("implement", now - 2820)) != keep:
            print(f"FAIL poll_fast/{name}: LOOP_SINCE = {LOOP_SINCE}")
            fails += 1
        if any(k == "looper:down" for k, _ in alarms(now)) == ps_ok:
            print(f"FAIL poll_fast/{name}: looper:down alarm should be {not ps_ok}")
            fails += 1
    # the debounce itself: one failed poll is routine, two in a row is the alarm
    stub("")
    globals()["PS_FAIL"] = 0
    poll_fast()
    if any(k == "looper:down" for k, _ in alarms(now)):
        print("FAIL poll_fast: a single failed `looper ps` must not raise looper:down")
        fails += 1
    poll_fast()
    if not any(k == "looper:down" for k, _ in alarms(now)):
        print(f"FAIL poll_fast: looper:down must raise after {PS_FAIL_MIN} failed polls")
        fails += 1
    # `status` flattens manual_intervention into paused; only displayStatus keeps the desk honest
    stub(json.dumps({"items": [{"type": "fixer", "status": "paused", "loopStatus": "paused",
                                "displayStatus": "manual_intervention", "currentStep": None,
                                "target": {"label": "SoulKyu/notificator#83"}, "agent": None}]}))
    poll_fast()
    if agent_state("fixer", "looper:fixer")[1] != "manual_intervention":
        print(f"FAIL agent_state: paused loop lost its display status: {agent_state('fixer', 'looper:fixer')}")
        fails += 1
    # ... and a desk label is not an alarm: the operator is the blocker here
    if not any(k == "halt:fixer:SoulKyu/notificator#83" for k, _ in alarms(now)):
        print(f"FAIL alarms: manual_intervention raised no halt row: {alarms(now)}")
        fails += 1
    # the scheduler starts the loop that just waited 47 min: its clock is the agent's, not the queue's
    fresh = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 5))
    stub(json.dumps({"items": [{"type": "worker", "status": "running", "currentStep": "implement",
                                "target": {"label": "SoulKyu/notificator#57"},
                                "agent": {"startedAt": fresh}}]}))
    poll_fast()
    if alarms(now):
        print(f"FAIL alarms: queue time counted as stall time: {alarms(now)}")
        fails += 1
    stub(PS_OK)
    LOOP_SINCE.clear()
    poll_fast()  # first sighting seeds from looper's agent.startedAt, not from now
    if LOOP_SINCE.get(k57) != ("implement", iso_ts("2026-01-02T00:00:00Z")):
        print(f"FAIL poll_fast: first sighting not seeded from agent.startedAt: {LOOP_SINCE}")
        fails += 1
    exited = time.strftime("%a %Y-%m-%d %H:%M:%S %Z", time.localtime(now - 11520))
    # the `-p` property list in poll_fast() and the keys alarms() reads are a stringly-typed
    # contract ~800 lines apart: drive a failed unit through the real parser so trimming the
    # list fails loudly instead of quietly downgrading `signal 11` back to `exit 11`
    SYSD = ("Id=notificator-qa.service\nActiveState=active\nResult=success\n"
            "ExecMainExitTimestamp=\nExecMainStatus=0\nExecMainCode=0\nNRestarts=0\n\n"
            "Id=notificator-reporter.service\nActiveState=failed\nResult=core-dump\n"
            f"ExecMainExitTimestamp={exited}\nExecMainStatus=11\nExecMainCode=3\nNRestarts=2\n\n"
            # ExecStartPre failed: the unit is dead but ExecMain* never got set
            "Id=notificator-groomer.service\nActiveState=failed\nResult=exit-code\n"
            "ExecMainExitTimestamp=\nExecMainStatus=0\nExecMainCode=0\nNRestarts=0\n\n")
    stub('{"items": []}')  # no loops: the only alarms left are the failed units
    poll_fast()
    row = next((r for k, r in alarms(now) if k == "unit:notificator-reporter"), "")
    if not all(x in row for x in ("core-dump", "signal 11", "2 redémarrages", "il y a")):
        print(f"FAIL alarms: systemctl -p list drifted from what alarms() reads: {row!r}")
        fails += 1
    row = next((r for k, r in alarms(now) if k == "unit:notificator-groomer"), "")
    if "échec exit-code" not in row or "(exit" in row:
        print(f"FAIL alarms: no main process exited, so there is no status to report: {row!r}")
        fails += 1
    if any(k == "unit:notificator-qa" for k, _ in alarms(now)):
        print("FAIL alarms: a healthy unit must not alarm")
        fails += 1
    globals()["sh"] = real_sh
    LOOP_SINCE.clear()
    track_loops([lp57], now)
    # alarm panel: machine-wide breakage (7 failed units) + one stalled loop, both widths,
    # flashing title — more alarms than ALARM_ROWS, so the overflow row renders too
    with LOCK:
        STATE["svc"] = {f"notificator-{n}": {
            "Id": f"notificator-{n}.service", "ActiveState": "failed", "Result": "exit-code",
            "ExecMainStatus": "2", "ExecMainCode": "1", "NRestarts": "3",
            "ExecMainExitTimestamp": exited}
            for n in ("qa", "scout", "worker", "rebaser", "promoter", "docagent", "reporter")}
        STATE["svc"]["notificator-worker"].update(  # ExecMainStatus is a signal when si_code is 2
            Result="oom-kill", ExecMainStatus="9", ExecMainCode="2")
        STATE["svc"]["notificator-reporter"].update(  # ... and when it is 3 (killed + dumped core)
            Result="core-dump", ExecMainStatus="11", ExecMainCode="3")
        STATE["loops"] = [lp57]
    a = alarms(now)
    if not (len(a) == 8 and "qa" in a[2][1] and "exit 2" in a[2][1] and "3h12" in a[2][1]
            and any("core-dump" in r and "signal 11" in r for _, r in a)
            and "signal 9" in a[-2][1] and "oom-kill" in a[-2][1]
            and "#57" in a[-1][1] and "implement" in a[-1][1] and "47min" in a[-1][1]):
        print(f"FAIL alarms: {a}")
        fails += 1
    for w in (92, 60):
        for tick in range(4):
            frame = render_frame(tick, w, flash=True)
            for line, _ in frame:
                if dwidth(line) != w:
                    print(f"FAIL alarm row {dwidth(line)} cols (want {w}): {line!r}")
                    fails += 1
            top = next(i for i, (l, _) in enumerate(frame) if "ALARMES" in l)
            body = 0  # the panel ends at the next panel header
            for l, _ in frame[top + 1:]:
                if "═══" in l:
                    break
                body += 1
            if body != ALARM_ROWS + 1:  # capped rows + the one summarising tail
                print(f"FAIL alarm panel is {body} rows, want {ALARM_ROWS} + tail (w={w})")
                fails += 1
            # the hidden tail names its categories, so a swallowed stall row is still visible
            tail = f"+{len(a) - ALARM_ROWS} autres alarmes (2 unités en échec · 1 loop bloquée)"
            if sum(1 for l, _ in frame if tail in l) != 1:
                print(f"FAIL alarm panel not capped at {ALARM_ROWS} rows with a typed tail (w={w})")
                fails += 1
    # exactly ALARM_ROWS + 1 alarms: the tail would occupy the row it displaced, so all
    # six render — and the stall row, which sorts last, is the one that proves it
    with LOCK:
        for n in ("scout", "rebaser"):
            STATE["svc"][f"notificator-{n}"]["Result"] = "success"
    for w in (92, 60):
        frame = render_frame(1, w)
        top = next(i for i, (l, _) in enumerate(frame) if "ALARMES" in l)
        body = 0
        for l, _ in frame[top + 1:]:
            if "═══" in l:
                break
            body += 1
        if body != ALARM_ROWS + 1 or any("autres alarmes" in l for l, _ in frame):
            print(f"FAIL alarm panel summarised a single hidden row ({body} rows, w={w})")
            fails += 1
        if not any("47min" in l for l, _ in frame):  # the clock leads, so dpad() cannot eat it
            print(f"FAIL alarm panel: stall clock truncated away at w={w}")
            fails += 1
    with LOCK:  # units back to success + loop step moved on → alarms gone, panel gone
        for unit in STATE["svc"].values():  # not `s`: the score fixture below still needs it
            unit["Result"] = "success"
    track_loops([dict(lp57, step="publish")], now)
    if alarms(now) or any("ALARMES" in l for l, _ in render_frame(0, 92)):
        print("FAIL alarm panel rendered with no alarm")
        fails += 1
    # navigation stride == renderer column count (coffee-corner decrement included)
    for w, want in ((92, 3), (113, 4), (120, 4)):
        if grid_per_row(w) != want:
            print(f"FAIL grid_per_row({w}) = {grid_per_row(w)} (want {want})")
            fails += 1
    # dynamic desks: 3 concurrent worker loops → 3 worker desks, rows still exact width.
    # injected out of `looper ps` order on purpose — build_desks() must sort them.
    # `#N` is what `looper ps` really prints; one `/N` row keeps both spellings covered.
    with LOCK:
        STATE["loops"] = [{"type": "worker", "target": t, "step": "code", "status": "running",
                           "loopId": f"lp_{t[-2:]}", "runId": None}
                          for t in ("SoulKyu/notificator#55", "SoulKyu/notificator/49",
                                    "SoulKyu/notificator#50")]
    desks = build_desks()
    workers = [d for d in desks if d["key"] == "worker"]
    if [d["name"] for d in workers] != ["WORKER·1", "WORKER·2", "WORKER·3"]:
        print(f"FAIL dynamic desks: {[d['name'] for d in workers]}")
        fails += 1
    if [d["state"][2] for d in workers] != ["49", "50", "55"]:
        print(f"FAIL dynamic targets: {[d['state'][2] for d in workers]}")
        fails += 1
    if len(desks) != len(ROSTER) + 2:
        print(f"FAIL desk count {len(desks)} (want {len(ROSTER) + 2})")
        fails += 1
    for tick in range(4):
        for line, _ in render_frame(tick, 92, sel=len(desks) - 1):
            if dwidth(line) != 92:
                print(f"FAIL dynamic row {dwidth(line)} cols: {line!r}")
                fails += 1
    for line in zoom_lines(workers[2], "worker-3.log", ["ok ✓"], True, "", 80):
        if dwidth(line) != 80:
            print(f"FAIL dynamic zoom row {dwidth(line)} cols: {line!r}")
            fails += 1
    # a zoom on loop 50 must keep showing loop 50 when a lower-numbered sibling finishes —
    # its rank slides from WORKER·2 to WORKER·1 while the index does not move at all
    ident = workers[1]["id"]
    worker_at = desk_index(desks, ident)
    with LOCK:  # loop 49 finishes; 50 and 55 keep running
        STATE["loops"] = [lp for lp in STATE["loops"] if loop_num(lp) != "49"]
    shrunk = build_desks()
    if shrunk[desk_index(shrunk, ident)]["loop"]["target"] != workers[1]["loop"]["target"]:
        print(f"FAIL desk identity repointed: {shrunk[desk_index(shrunk, ident)]['loop']['target']}")
        fails += 1
    with LOCK:  # single loop → single desk, unchanged from ROSTER
        STATE["loops"] = [STATE["loops"][0]]
    solo = build_desks()
    # a selection parked on REVIEW must still be REVIEW once two worker loops finish
    if desk_index(solo, "reviewer") == desk_index(desks, "reviewer") or desk_index(solo, "reviewer") is None:
        print(f"FAIL desk_index: REVIEW {desk_index(desks, 'reviewer')} → {desk_index(solo, 'reviewer')}")
        fails += 1
    if desk_index(solo, ident) is not None:
        print("FAIL desk_index: vanished desk should resolve to None")
        fails += 1
    # …and the selection then falls back to the role, not to whoever inherited the old index
    if solo[desk_resolve(solo, ident, worker_at)]["key"] != "worker":
        print(f"FAIL desk_resolve: worker selection landed on {solo[desk_resolve(solo, ident, worker_at)]['key']}")
        fails += 1
    solo_workers = [d["name"] for d in solo if d["key"] == "worker"]
    if len(solo) != len(ROSTER) or solo_workers != ["WORKER"]:
        print(f"FAIL single loop duplicated the desk: {solo_workers}")
        fails += 1
    # targetless loops (queued, the common case) all carry target "?" — the id must come from
    # loopId or two of them collide and desk_index silently resolves both to the first desk
    with LOCK:
        STATE["loops"] = [{"type": "worker", "target": "?", "step": "—", "status": "queued",
                           "loopId": f"lp_{c}", "runId": None} for c in "AB"]
    queued = build_desks()
    for i, d in enumerate(queued):
        if d["key"] == "worker" and desk_index(queued, d["id"]) != i:
            print(f"FAIL desk id collision: {d['name']} resolves to index {desk_index(queued, d['id'])}")
            fails += 1
    # …and with no loopId at all (older looper CLI) the rank fallback must still be unique
    with LOCK:
        STATE["loops"] = [{"type": "worker", "target": "?", "step": "—", "status": "queued"}
                          for _ in range(2)]
    handleless = build_desks()
    for i, d in enumerate(handleless):
        if d["key"] == "worker" and desk_index(handleless, d["id"]) != i:
            print(f"FAIL loopId-less desk id collision: {d['name']} → index {desk_index(handleless, d['id'])}")
            fails += 1
    # a full factory must not push the wall board off a short terminal: cap the grid, fold the rest
    with LOCK:
        STATE["loops"] = [{"type": r, "target": f"SoulKyu/notificator#{i}", "step": "code", "status": "running"}
                          for r in ("worker", "reviewer", "fixer", "coordinator", "planner")
                          for i in range(5)]
        # every panel populated: this is panel_rows()' worst case, so the cap must hold there.
        # Pending in particular is the normal state of the factory, not an edge case.
        STATE["pending"] = [f"attente {i}" for i in range(PENDING_MAX + 4)]  # header + 5 + "+N autres"
        STATE["score"] = s
        STATE["intercom"] = [f"a → b: msg {i}" for i in range(3)]  # 1 + 3, the budgeted worst case
        STATE["err"] = "poll error"  # the budgeted error row: without it the frame is a row short
        # …and the alarm panel, the one render_frame builds itself: without a failed unit here
        # alarms() is empty and the sweep never sees its 1 + ALARM_ROWS + tail rows
        STATE["svc"] = {f"notificator-{u}": {"Id": f"notificator-{u}.service", "Result": "exit-code",
                                             "ExecMainStatus": "1", "ExecMainCode": "1"}
                        for u in ("scout", "qa", "rebaser", "promoter", "docagent", "reporter", "groomer")}
    term_w = 120
    # sweep the heights: the budget boundary only shows up when (h - PANEL_ROWS) % 8 == 0,
    # so a single height silently passes an off-by-one that eats the closing border.
    # 62 is the floor's own limit: len(ROSTER) desks are 4 grid rows, 32 + 29 panel rows = 61 frame
    # rows, and main_curses draws h-1 of them. Below that no cap fits the panels, so nothing to assert.
    for term_h in range(62, 78):
        panels = panel_snapshot()
        cap = desk_cap(term_h, grid_per_row(term_w), panels)
        capped = build_desks(cap)
        if not len(ROSTER) <= len(capped) <= cap:
            print(f"FAIL desk cap: {len(capped)} desks (cap {cap}) at h={term_h}")
            fails += 1
        frame = render_frame(3, term_w, sel=0, desks=capped, panels=panels)
        board = next((y for y, (l, _) in enumerate(frame) if "TABLEAU DU MUR" in l), None)
        if board is None or board >= term_h - 1:
            print(f"FAIL desk cap: wall board at row {board} of a {term_h}-row terminal")
            fails += 1
        radio = next((y for y, (l, _) in enumerate(frame) if "📻" in l), None)  # below board → cut first
        if radio is None or radio >= term_h - 1:
            print(f"FAIL desk cap: radio ticker at row {radio} of a {term_h}-row terminal")
            fails += 1
        if len(frame) > term_h - 1:  # main_curses breaks at h-1: an h-row frame loses its └───┘
            print(f"FAIL desk cap: {len(frame)}-row frame on a {term_h}-row terminal")
            fails += 1
        if sum(1 for d in capped if "+" in d["name"]) < 1:
            print(f"FAIL desk cap: hidden loops not folded into a desk title: {[d['name'] for d in capped]}")
            fails += 1
        for line, _ in frame:  # folded `+N` titles are the one desk name mutated after construction
            if dwidth(line) != term_w:
                print(f"FAIL capped row {dwidth(line)} cols at h={term_h}: {line!r}")
                fails += 1
    # the budget and the frame must come out of one read of STATE. Cap on an empty office, let a
    # poll fill every panel in the gap, then draw: re-reading STATE here would emit the 16 rows the
    # budget never granted and push the board past h-1, so the frame must honour the snapshot.
    with LOCK:  # svc too: it feeds alarms(), and leaving it set makes the later poll fixtures order-dependent
        STATE["pending"], STATE["intercom"], STATE["err"], STATE["score"] = [], [], "", None
        STATE["svc"] = {}
    quiet, term_h = panel_snapshot(), 60
    capped = build_desks(desk_cap(term_h, grid_per_row(term_w), quiet))
    with LOCK:  # the poller lands between desk_cap() and render_frame()
        STATE["pending"] = [f"attente {i}" for i in range(PENDING_MAX + 4)]
        STATE["intercom"] = [f"a → b: msg {i}" for i in range(3)]
        STATE["err"], STATE["score"] = "poll error", s
        STATE["svc"] = {"notificator-qa": {"Id": "notificator-qa.service", "Result": "exit-code",
                                           "ExecMainStatus": "1", "ExecMainCode": "1"}}
    frame = render_frame(3, term_w, sel=0, desks=capped, panels=quiet)
    if len(frame) > term_h - 1:
        print(f"FAIL panel snapshot: poll in the gap grew the frame to {len(frame)} rows at h={term_h}")
        fails += 1
    # the zoomed desk is a centred overlay, not a grid slot: the cap must never trim it out from
    # under the operator. h=50 with every panel up caps at the len(ROSTER) floor, so without `keep`
    # every WORKER·N but the first is gone and desk_index returns None → the panel closes mid-read.
    top_worker = max((d for d in build_desks() if d["key"] == "worker"), key=lambda d: d["name"])["id"]
    kept = build_desks(desk_cap(50, grid_per_row(term_w), panel_snapshot()), keep=top_worker)
    if desk_index(kept, top_worker) is None:
        print(f"FAIL desk cap: zoomed desk trimmed away: {[d['name'] for d in kept]}")
        fails += 1
    with LOCK:  # …and with no optional panel drawn the budget must hand the extra rows back out:
        STATE["pending"], STATE["intercom"], STATE["err"], STATE["score"] = [], [], "", None
        STATE["svc"] = {}
    if desk_cap(50, grid_per_row(term_w), panel_snapshot()) <= len(ROSTER):  # a constant pins this at 13
        print(f"FAIL desk cap: no panels drawn still caps at {desk_cap(50, grid_per_row(term_w), panel_snapshot())}")
        fails += 1
    # 📬 counts a role's inbox, so it belongs on the desk the mail flight lands on — one badge,
    # not one per concurrent loop, which would read as three messages queued for three people
    with LOCK:
        STATE["mail_pending"] = {"worker": 1}
    badged = sum(l.count("📬") for l, _ in render_frame(1, term_w, desks=build_desks()))
    if badged != 1:
        print(f"FAIL mail badge painted on {badged} desks")
        fails += 1
    with LOCK:  # leave no injected loop behind for later checks
        STATE["loops"], STATE["pending"], STATE["intercom"], STATE["err"] = [], [], [], ""
        STATE["mail_pending"] = {}
    # a looper desk tails its own run under LOOPER_LOG_DIR/<loopId>/<runId>/; a desk off a loop
    # tails its role in LOG_DIR, under the file prefix (REBASE writes `rebase-*`, not `rebaser-*`)
    global LOG_DIR, LOOPER_LOG_DIR
    real_log_dir, real_looper_dir = LOG_DIR, LOOPER_LOG_DIR
    lp = {"target": "x#9", "step": "code", "status": "running", "loopId": "loop_a", "runId": "run_b"}
    d9 = {"key": "worker", "id": "worker:x#9", "emoji": "🚢", "name": "WORKER·1", "kind": "looper:worker",
          "state": ("work", "code", "9"), "loop": lp}
    reb = {"key": "rebaser", "id": "rebaser", "emoji": "🔀", "name": "REBASE", "kind": "svc:notificator-rebaser",
           "state": ("work", "run", ""), "loop": None}
    with tempfile.TemporaryDirectory() as tmp:
        LOG_DIR, LOOPER_LOG_DIR = os.path.join(tmp, "agents"), os.path.join(tmp, "loops")
        run = os.path.join(LOOPER_LOG_DIR, "loop_a", "run_b")
        os.makedirs(run)
        os.makedirs(LOG_DIR)
        open(os.path.join(LOG_DIR, "worker-9-20260101T000000.log"), "w").close()
        open(os.path.join(run, "agent_c.stderr.log"), "w").close()
        if desk_tail(d9)[0] is not None:  # no stdout log yet — LOG_DIR must not be raided for one
            print(f"FAIL loop tail: {desk_tail(d9)[0]} is not this run's log")
            fails += 1
        open(os.path.join(run, "agent_c.stdout.log"), "w").write("hello\n")
        if desk_tail(d9)[0] != "agent_c.stdout.log":
            print(f"FAIL loop tail: worker desk got {desk_tail(d9)[0]}")
            fails += 1
        open(os.path.join(LOG_DIR, "rebase-83-20260101T000000.log"), "w").close()
        if desk_tail(reb)[0] != "rebase-83-20260101T000000.log":
            print(f"FAIL log prefix: REBASE got {desk_tail(reb)[0]}")
            fails += 1
    LOG_DIR, LOOPER_LOG_DIR = real_log_dir, real_looper_dir
    # the loop dict shape everything downstream keys off (loop_state on status, loop_order/
    # loop_num on target, loop_tail on loopId/runId), straight out of `looper ps --json`
    ps = json.dumps({"items": [
        {"type": "worker", "target": {"label": "SoulKyu/notificator#81"}, "currentStep": "code",
         "displayStatus": "running", "loopId": "lp1", "runId": "rn1"},
        {"type": "reviewer", "target": {"label": "SoulKyu/notificator#82"}, "currentStep": None,
         "status": "queued", "loopId": "lp2", "runId": None},
        {"type": "fixer", "target": None, "status": "running", "loopId": "lp3", "runId": "rn3"},
    ]})
    want = [("worker", "SoulKyu/notificator#81", "code", "running", "rn1"),
            ("reviewer", "SoulKyu/notificator#82", "-", "queued", None),
            ("fixer", "?", "-", "running", "rn3")]
    loops, ok = parse_ps(ps)
    got = [(l["type"], l["target"], l["step"], l["status"], l["runId"]) for l in loops]
    if got != want or not ok:
        print(f"FAIL ps parse: {got} (ok={ok})")
        fails += 1
    # a row with no `type` is not renderable: schema drift reads as "injoignable", not as a calm desk
    if parse_ps(json.dumps({"items": [{"target": {"label": "x#1"}}]})) != ([], False):
        print("FAIL ps parse: a typeless row should read as unreachable")
        fails += 1
    # valid JSON of the wrong shape is the other half of the blast radius: a bare list, a null,
    # or a `target` serialised as the plain label all used to raise AttributeError out of poll_fast
    # no stdout at all is the same verdict, and deliberately so: sh() returns "" for a missing
    # binary and for a 20 s timeout alike, neither of which is an idle factory
    for bad in ("[]", "null", '{"items": [{"type": "worker", "target": "owner/repo#1"}]}', "", "   \n"):
        if parse_ps(bad) != ([], False):
            print(f"FAIL ps parse: unshaped JSON {bad} → {parse_ps(bad)}")
            fails += 1
    # an idle factory parses fine — it must not read as unreachable (that is the looper:down alarm)
    if parse_ps('{"items": []}') != ([], True):
        print(f"FAIL ps parse: idle factory → {parse_ps('{\"items\": []}')}")
        fails += 1
    # an older CLI, or one printing a diagnostic to stdout, costs the loop desks and nothing else
    real_sh = sh
    globals()["sh"] = lambda cmd, timeout=20: ("error: unknown flag --json\n" if "looper ps" in cmd
                                               else "Id=notificator-qa.service\nActiveState=active\n")
    globals()["PS_FAIL"] = 0
    try:
        for _ in range(PS_FAIL_MIN):  # ... and must say so once debounced: stderr is dropped
            poll_fast()
    finally:
        globals()["sh"] = real_sh
    if STATE["loops"] or "notificator-qa" not in STATE["svc"]:
        print(f"FAIL ps parse: bad looper stdout froze the service poll: {STATE['svc']}")
        fails += 1
    if STATE["ps_ok"]:
        print("FAIL ps parse: unreadable looper stdout reported nothing")
        fails += 1
    globals()["sh"] = lambda cmd, timeout=20: ('{"items": []}' if "looper ps" in cmd else "")
    try:
        poll_fast()
    finally:
        globals()["sh"] = real_sh
    if not STATE["ps_ok"]:  # looper answered again: the alarm must not stick
        print("FAIL ps parse: looper:down survived a good poll")
        fails += 1
    with LOCK:
        STATE["loops"], STATE["svc"] = [], {}
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
