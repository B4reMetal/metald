#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
fetch tool for metald, backed by Exa (exa.ai) /contents.

Configure under env: in config.yml:
  EXA_API_KEY       required
  EXA_FETCH_CHARS   characters of page text returned (default: 3000)
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

API_URL = "https://api.exa.ai/contents"
API_KEY = os.environ.get("EXA_API_KEY", "")
MAX_CHARS = int(os.environ.get("EXA_FETCH_CHARS", "3000"))
TIMEOUT = 60

def print_schema():
    schema = {
        "title": "fetch",
        "description": (
            "read a web page someone linked and get its actual text. use it "
            "when a url is posted and the content matters, or when a search "
            "excerpt was not enough to answer properly. returns the page "
            "text - quote it and cite the url. do not guess at what is on a "
            "page you have not read."
        ),
        "type": "object",
        "properties": {
            "url": {
                "type": "string",
                "description": "the http(s) page to read",
            },
        },
        "required": ["url"],
        "additionalProperties": False,
        "sandbox": {"allowNetwork": True},
        "requires": ["EXA_API_KEY"],
    }
    print(json.dumps(schema, indent=2))

def fetch(url: str) -> str:
    url = (url or "").strip()
    if not url:
        return "Error: no url given"

    parsed = urllib.parse.urlparse(url)
    if parsed.scheme not in ("http", "https"):
        return "Error: only http and https links"
    if not parsed.hostname:
        return "Error: that url has no host"

    if not API_KEY:
        toollog.log_detail("webfetch", "EXA_API_KEY is not set")
        return "Error: the fetch backend is not configured"

    req = urllib.request.Request(
        API_URL,
        data=json.dumps({"urls": [url], "text": {"maxCharacters": MAX_CHARS}}).encode("utf-8"),
        headers={"x-api-key": API_KEY, "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            data = json.loads(resp.read().decode("utf-8", errors="replace"))
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")[:300]
        toollog.log_detail("webfetch", f"exa http {e.code}: {body}")
        if e.code in (401, 403):
            return "Error: the fetch backend rejected our credentials"
        if e.code == 429:
            return "Error: fetching is rate limited right now, try again shortly"
        return "Error: the fetch backend returned an error"
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        toollog.log_detail("webfetch", f"exa unreachable: {e}")
        return "Error: the fetch backend is unavailable right now"

    results = data.get("results") or []
    if not results:
        # statuses carries the per-url reason, which is the useful half when
        # a page is paywalled, dead, or refuses robots.
        statuses = data.get("statuses") or []
        reason = ""
        if statuses:
            st = statuses[0]
            reason = st.get("error") or st.get("status") or ""
        toollog.log_detail("webfetch", f"no content for {url[:120]!r}: {reason}")
        return "Could not read that page. Say so plainly rather than guessing what it said."

    r = results[0]
    title = (r.get("title") or "").strip()
    text = " ".join((r.get("text") or "").split())
    if not text:
        return "That page had no readable text. Say so plainly."

    cost = (data.get("costDollars") or {}).get("total")
    toollog.log_detail("webfetch", f"ok {url[:80]!r} -> {len(text)} chars, cost=${cost}")

    head = f"{title}\n{url}" if title else url
    return f"{head}\n\n{text[:MAX_CHARS]}"

def main():
    if len(sys.argv) < 2:
        print("usage: webfetch.py --schema | --execute <json>", file=sys.stderr)
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
        print(fetch(data.get("url") or ""))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
