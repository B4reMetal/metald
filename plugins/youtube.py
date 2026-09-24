#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
youtube tool for metald.

Configure via environment variables (optional):
  YT_MAX_CHARS      transcript characters returned (default: 3000)
  YT_MAX_DURATION   longest video accepted, seconds (default: 7200)
"""

import sys
import os
import json
import re
import subprocess
import tempfile

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog

MAX_CHARS = int(os.environ.get("YT_MAX_CHARS", "3000"))
MAX_DURATION = int(os.environ.get("YT_MAX_DURATION", "7200"))
TIMEOUT = 120

YT_HOSTS = re.compile(
    r"^https?://(www\.|m\.|music\.)?(youtube\.com/|youtu\.be/)", re.I)

def print_schema():
    schema = {
        "title": "youtube",
        "description": (
            "read a youtube video: returns its title, channel and transcript. "
            "use it when someone posts a youtube link and what's in it "
            "matters - don't guess from the title. quote what was actually "
            "said. long videos are truncated."
        ),
        "type": "object",
        "properties": {
            "url": {"type": "string", "description": "the youtube video link"},
        },
        "required": ["url"],
        "additionalProperties": False,
        "sandbox": {"allowNetwork": True},
    }
    print(json.dumps(schema, indent=2))

def vtt_to_text(path):
    """Flatten a WEBVTT file into prose, dropping the lines auto-captions repeat."""
    out, last = [], None
    for raw in open(path, encoding="utf-8", errors="replace"):
        line = raw.strip()
        if (not line or line.startswith(("WEBVTT", "Kind:", "Language:"))
                or "-->" in line or line.isdigit()):
            continue
        line = re.sub(r"<[^>]+>", "", line).strip()
        if line and line != last:
            out.append(line)
            last = line
    return " ".join(out)

def read_video(url: str) -> str:
    url = (url or "").strip()
    if not url:
        return "Error: no url given"
    if not YT_HOSTS.match(url):
        return "Error: that is not a youtube link"

    if not shutil_which("yt-dlp"):
        toollog.log_detail("youtube", "yt-dlp is not installed")
        return "Error: the youtube backend is not configured"

    with tempfile.TemporaryDirectory() as tmp:
        try:
            meta = subprocess.run(
                ["yt-dlp", "-J", "--no-warnings", "--skip-download", url],
                capture_output=True, text=True, timeout=TIMEOUT)
            if meta.returncode != 0:
                toollog.log_detail("youtube", f"metadata failed: {meta.stderr[:200]}")
                return "Error: could not read that video"
            info = json.loads(meta.stdout)
        except (subprocess.TimeoutExpired, json.JSONDecodeError) as e:
            toollog.log_detail("youtube", f"metadata error: {e}")
            return "Error: could not read that video"

        title = info.get("title") or "untitled"
        channel = info.get("uploader") or info.get("channel") or "?"
        duration = info.get("duration") or 0
        if duration and duration > MAX_DURATION:
            return (f"{title} ({channel}) is {duration // 60} minutes long - "
                    f"too long to transcribe. Say so plainly.")

        try:
            subprocess.run(
                ["yt-dlp", "--skip-download", "--write-auto-subs", "--write-subs",
                 "--sub-langs", "en.*", "--sub-format", "vtt",
                 "--no-warnings", "-o", os.path.join(tmp, "v.%(ext)s"), url],
                capture_output=True, text=True, timeout=TIMEOUT)
        except subprocess.TimeoutExpired:
            return "Error: timed out fetching that video's captions"

        vtts = [f for f in os.listdir(tmp) if f.endswith(".vtt")]
        if not vtts:
            return (f"{title} ({channel})\n\nNo captions available for this video. "
                    f"Say so plainly rather than guessing its contents.")

        text = vtt_to_text(os.path.join(tmp, vtts[0]))

    if not text:
        return f"{title} ({channel})\n\nCaptions were empty. Say so plainly."

    mins = f"{duration // 60}:{duration % 60:02d}" if duration else "?"
    toollog.log_detail("youtube", f"ok {title[:60]!r} ({mins}) -> {len(text)} chars")
    head = f"{title}\n{channel} | {mins}\n"
    if len(text) > MAX_CHARS:
        text = text[:MAX_CHARS] + f"... [truncated, {len(text)} chars total]"
    return head + "\n" + text

def shutil_which(name):
    from shutil import which
    return which(name)

def main():
    if len(sys.argv) < 2:
        print("usage: youtube.py --schema | --execute <json>", file=sys.stderr)
        sys.exit(1)
    if sys.argv[1] == "--schema":
        print_schema()
        return
    if sys.argv[1] == "--execute":
        try:
            data = json.loads(sys.argv[2]) if len(sys.argv) > 2 else {}
        except json.JSONDecodeError:
            print("Error: could not read the request")
            return
        print(read_video(data.get("url") or ""))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
