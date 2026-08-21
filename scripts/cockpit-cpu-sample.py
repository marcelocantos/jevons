#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
"""TIME-delta CPU sampler for the Firefox Jevons cockpit tab.

Activity Monitor and ``ps %CPU`` are decaying averages. This script
samples ``ps TIME`` twice over a wall interval and reports actual
core-% for each Firefox Isolated Web Content (plugin-container) process,
plus whether a window titled ``Jevons`` is present.

    python3 scripts/cockpit-cpu-sample.py
    python3 scripts/cockpit-cpu-sample.py --seconds 30
    python3 scripts/cockpit-cpu-sample.py --watch --every 10
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time


def parse_ps_time(raw: str) -> float:
    """Parse macOS ``ps TIME`` (``[[dd-]hh:]mm:ss.ss``) into seconds."""
    raw = raw.strip()
    if not raw:
        return 0.0
    days = 0
    if "-" in raw:
        day_part, raw = raw.split("-", 1)
        days = int(day_part)
    parts = raw.split(":")
    if len(parts) == 3:
        hours, minutes, seconds = int(parts[0]), int(parts[1]), float(parts[2])
    elif len(parts) == 2:
        hours = 0
        minutes, seconds = int(parts[0]), float(parts[1])
    else:
        hours, minutes, seconds = 0, 0, float(parts[0])
    return days * 86400 + hours * 3600 + minutes * 60 + seconds


def plugin_containers() -> list[dict]:
    out = subprocess.check_output(
        ["ps", "-axo", "pid=,rss=,etime=,time=,command="],
        text=True,
    )
    rows = []
    for line in out.splitlines():
        if "plugin-container" not in line or "Firefox.app" not in line:
            continue
        if " -isForBrowser " not in line:
            continue
        pid_s, rss_s, etime, rest = line.strip().split(None, 3)
        time_s, _, command = rest.partition(" ")
        # TIME is the first token of rest; command follows.
        # rest is "TIME COMMAND"; TIME has no spaces.
        rows.append(
            {
                "pid": int(pid_s),
                "rss_kb": int(rss_s),
                "etime": etime,
                "cpu_seconds": parse_ps_time(time_s),
                "command": command.strip(),
            }
        )
    return rows


def jevons_window_titles() -> list[str]:
    script = """
tell application "System Events"
  if not (exists process "Firefox") then return ""
  tell process "Firefox"
    set names to name of windows
    set out to ""
    repeat with n in names
      set out to out & (n as text) & linefeed
    end repeat
    return out
  end tell
end tell
"""
    try:
        raw = subprocess.check_output(["osascript", "-e", script], text=True)
    except subprocess.CalledProcessError:
        return []
    return [ln.strip() for ln in raw.splitlines() if ln.strip()]


def sample_once(seconds: float) -> dict:
    t0 = time.monotonic()
    before = {row["pid"]: row for row in plugin_containers()}
    time.sleep(seconds)
    wall = time.monotonic() - t0
    after = plugin_containers()
    titles = jevons_window_titles()
    jevons = [t for t in titles if "Jevons" in t]
    tabs = []
    for row in after:
        prev = before.get(row["pid"])
        delta = row["cpu_seconds"] - (prev["cpu_seconds"] if prev else 0.0)
        cpu_pct = (delta / wall) * 100.0 if wall > 0 else 0.0
        tabs.append(
            {
                "pid": row["pid"],
                "cpu_pct": round(cpu_pct, 1),
                "cpu_seconds_delta": round(delta, 3),
                "rss_mb": round(row["rss_kb"] / 1024.0, 1),
                "etime": row["etime"],
            }
        )
    tabs.sort(key=lambda r: r["cpu_pct"], reverse=True)
    return {
        "wall_s": round(wall, 3),
        "jevons_windows": jevons,
        "firefox_windows": titles,
        "tabs": tabs,
    }


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--seconds", type=float, default=8.0, help="wall interval for TIME-delta")
    p.add_argument("--watch", action="store_true", help="repeat until Ctrl-C")
    p.add_argument("--every", type=float, default=10.0, help="seconds between watch samples (after each interval)")
    p.add_argument("--json", action="store_true", help="JSON lines instead of a table")
    args = p.parse_args()
    if args.seconds <= 0:
        print("--seconds must be > 0", file=sys.stderr)
        return 2

    def emit() -> None:
        snap = sample_once(args.seconds)
        if args.json:
            print(json.dumps(snap), flush=True)
            return
        jw = ", ".join(snap["jevons_windows"]) or "(no Jevons window)"
        print(f"wall={snap['wall_s']:.1f}s  windows={jw}", flush=True)
        if not snap["tabs"]:
            print("  (no Firefox plugin-container tabs)", flush=True)
            return
        print(f"  {'pid':>7}  {'cpu%':>7}  {'rssMB':>7}  etime", flush=True)
        for tab in snap["tabs"]:
            print(
                f"  {tab['pid']:7d}  {tab['cpu_pct']:6.1f}%  {tab['rss_mb']:7.1f}  {tab['etime']}",
                flush=True,
            )

    try:
        if args.watch:
            while True:
                emit()
                time.sleep(max(0.0, args.every))
        else:
            emit()
    except KeyboardInterrupt:
        return 130
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
