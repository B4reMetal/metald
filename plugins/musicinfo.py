#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
musicinfo tool for metald.

Configure under env: in config.yml (optional):
  MUSICINFO_MAX_RESULTS   results per lookup (default: 5)
"""

import sys
import os
import json
import time
import urllib.parse
import urllib.request
import urllib.error

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog

API = "https://musicbrainz.org/ws/2"
MAX_RESULTS = int(os.environ.get("MUSICINFO_MAX_RESULTS", "5"))
UA = "metald-irc-bot/1.0 (https://github.com/pkdindustries/soulshack)"
TIMEOUT = 20

def print_schema():
    schema = {
        "title": "musicinfo",
        "description": (
            "look up real music in musicbrainz: artists, their releases, or "
            "tracks. use it when someone asks about a band, an album, who "
            "made something, or what year a record came out - rather than "
            "recalling a discography you may be inventing. this is about "
            "EXISTING music; use the song tool to generate new music."
        ),
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "artist, album or track name to look up",
            },
            "kind": {
                "type": "string",
                "enum": ["artist", "release", "recording"],
                "description": "what to search for (default: artist)",
            },
        },
        "required": ["query"],
        "additionalProperties": False,
        "sandbox": {"allowNetwork": True},
    }
    print(json.dumps(schema, indent=2))

def get(path):
    req = urllib.request.Request(f"{API}/{path}", headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        return json.loads(resp.read().decode("utf-8", errors="replace"))

def fmt_artist(a):
    bits = [a.get("name", "?")]
    if a.get("disambiguation"):
        bits.append(f"({a['disambiguation']})")
    line = " ".join(bits)
    meta = []
    if a.get("country"):
        meta.append(a["country"])
    if a.get("type"):
        meta.append(a["type"])
    span = a.get("life-span") or {}
    if span.get("begin"):
        meta.append(f"from {span['begin']}" + (f" to {span['end']}" if span.get("end") else ""))
    tags = [t["name"] for t in (a.get("tags") or [])][:5]
    if tags:
        meta.append("tags: " + ", ".join(tags))
    return line + ("\n   " + " | ".join(meta) if meta else "")

def fmt_release(r):
    artist = ", ".join(c["artist"]["name"] for c in (r.get("artist-credit") or []) if c.get("artist"))
    meta = [x for x in (r.get("date"), r.get("country"), (r.get("release-group") or {}).get("primary-type")) if x]
    return f"{r.get('title','?')} - {artist or '?'}" + ("\n   " + " | ".join(meta) if meta else "")

def fmt_recording(r):
    artist = ", ".join(c["artist"]["name"] for c in (r.get("artist-credit") or []) if c.get("artist"))
    meta = []
    if r.get("length"):
        secs = int(r["length"] / 1000)
        meta.append(f"{secs // 60}:{secs % 60:02d}")
    if r.get("first-release-date"):
        meta.append(r["first-release-date"])
    return f"{r.get('title','?')} - {artist or '?'}" + ("\n   " + " | ".join(meta) if meta else "")

def lookup(query: str, kind: str = "artist") -> str:
    query = " ".join((query or "").split())
    if not query:
        return "Error: nothing to look up"
    if kind not in ("artist", "release", "recording"):
        kind = "artist"

    path = (f"{kind}?query={urllib.parse.quote(query)}&fmt=json&limit={MAX_RESULTS}")
    try:
        data = get(path)
    except urllib.error.HTTPError as e:
        toollog.log_detail("musicinfo", f"musicbrainz http {e.code} for {query[:60]!r}")
        if e.code == 503:
            return "Error: the music database is rate limiting us, try again shortly"
        return "Error: the music database returned an error"
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        toollog.log_detail("musicinfo", f"musicbrainz unreachable: {e}")
        return "Error: the music database is unavailable right now"

    key = {"artist": "artists", "release": "releases", "recording": "recordings"}[kind]
    items = data.get(key) or []
    if not items:
        return f"Nothing found for {query!r}. Say so plainly rather than inventing a discography."

    fmt = {"artist": fmt_artist, "release": fmt_release, "recording": fmt_recording}[kind]
    out = [f"{i}. {fmt(it)}" for i, it in enumerate(items[:MAX_RESULTS], 1)]
    toollog.log_detail("musicinfo", f"ok {kind} {query[:60]!r} -> {len(items)} results")
    # Courtesy rate limit: MusicBrainz asks for ~1 req/sec per client.
    time.sleep(1)
    return "\n".join(out)

def main():
    if len(sys.argv) < 2:
        print("usage: musicinfo.py --schema | --execute <json>", file=sys.stderr)
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
        print(lookup(data.get("query") or "", (data.get("kind") or "artist").strip()))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
