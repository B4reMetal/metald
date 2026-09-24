#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
wikipedia tool for metald.

Configure via environment variables (both optional):
  WIKI_LANG        language edition (default: en)
  WIKI_MAX_CHARS   characters of summary returned (default: 1200)
"""

import sys
import os
import json
import urllib.parse
import urllib.request
import urllib.error

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog

LANG = os.environ.get("WIKI_LANG", "en")
MAX_CHARS = int(os.environ.get("WIKI_MAX_CHARS", "1200"))
UA = "metald-irc-bot/1.0 (https://github.com/pkdindustries/soulshack)"
TIMEOUT = 20

def print_schema():
    schema = {
        "title": "wikipedia",
        "description": (
            "look up a wikipedia article and get its summary. use it for "
            "people, places, bands, events, concepts - anything encyclopaedic "
            "that you would otherwise be recalling from memory. prefer it to "
            "guessing, and say what it says rather than what you assumed. "
            "returns the summary and a url; cite the url."
        ),
        "type": "object",
        "properties": {
            "title": {
                "type": "string",
                "description": "article title or search term, e.g. 'BABYMETAL' or 'Internet Relay Chat'",
            },
        },
        "required": ["title"],
        "additionalProperties": False,
        "sandbox": {"allowNetwork": True},
    }
    print(json.dumps(schema, indent=2))

def get(url):
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        return json.loads(resp.read().decode("utf-8", errors="replace"))

def search_title(term):
    """Resolve a loose term to a real article title, falling back to search on a miss."""
    url = (f"https://{LANG}.wikipedia.org/w/api.php?action=query&list=search"
           f"&srsearch={urllib.parse.quote(term)}&srlimit=1&format=json")
    data = get(url)
    hits = (data.get("query") or {}).get("search") or []
    return hits[0]["title"] if hits else None

def lookup(title: str) -> str:
    title = " ".join((title or "").split())
    if not title:
        return "Error: no article requested"

    approximate = None

    def summary(t):
        url = f"https://{LANG}.wikipedia.org/api/rest_v1/page/summary/{urllib.parse.quote(t.replace(' ', '_'))}"
        return get(url)

    try:
        try:
            data = summary(title)
        except urllib.error.HTTPError as e:
            if e.code != 404:
                raise
            found = search_title(title)
            if not found:
                return f"No wikipedia article for {title!r}. Say so plainly rather than inventing one."
            data = summary(found)
            approximate = found
    except (urllib.error.URLError, urllib.error.HTTPError, OSError, json.JSONDecodeError) as e:
        toollog.log_detail("wikipedia", f"lookup failed for {title[:80]!r}: {e}")
        return "Error: wikipedia is unavailable right now"

    # A disambiguation page is not an answer; saying so beats picking one of
    # its entries at random and presenting it as the subject.
    if data.get("type") == "disambiguation":
        return (f"{data.get('title')} is ambiguous on wikipedia - several things share that name. "
                f"Ask which one they mean.")

    extract = " ".join((data.get("extract") or "").split())
    if not extract:
        return f"No summary available for {title!r}. Say so plainly."

    page = (data.get("content_urls") or {}).get("desktop", {}).get("page", "")
    out = f"{data.get('title')}\n{extract[:MAX_CHARS]}"
    if approximate is not None:
        out = (f"(no exact article for {title!r}; wikipedia's closest match was "
               f"{data.get('title')!r} - say so if it looks unrelated)\n") + out
    if page:
        out += f"\n{page}"
    toollog.log_detail("wikipedia", f"ok {title[:60]!r} -> {data.get('title')!r} ({len(extract)} chars)")
    return out

def main():
    if len(sys.argv) < 2:
        print("usage: wikipedia.py --schema | --execute <json>", file=sys.stderr)
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
        print(lookup(data.get("title") or ""))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
