#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
web_search tool for metald, backed by Exa (exa.ai).

Configure under env: in config.yml:
  EXA_API_KEY       required; the tool refuses to run without it
  EXA_MAX_RESULTS   hard ceiling on results per call (default: 5)
  EXA_MAX_CHARS     page text returned per result (default: 1200)
"""

import sys
import os
import json
import urllib.request
import urllib.error

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog

API_URL = "https://api.exa.ai/search"
API_KEY = os.environ.get("EXA_API_KEY", "")
MAX_RESULTS = int(os.environ.get("EXA_MAX_RESULTS", "5"))
MAX_CHARS = int(os.environ.get("EXA_MAX_CHARS", "1200"))
TIMEOUT = 45

def print_schema():
    schema = {
        "title": "web_search",
        "description": (
            "search the web and get back real page content, not just links. "
            "use it whenever a question turns on something you cannot know: "
            "current events, releases, prices, who someone is, whether a "
            "thing exists. returns titles, urls and an excerpt of each page "
            "- quote what it says and cite the url rather than paraphrasing "
            "from memory. if the excerpt is not enough, fetch the full page "
            "with the fetch tool."
        ),
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "what to search for, in plain words",
            },
            "num_results": {
                "type": "integer",
                "description": f"how many results to return (1-{MAX_RESULTS})",
                "minimum": 1,
                "maximum": MAX_RESULTS,
            },
        },
        "required": ["query"],
        "additionalProperties": False,
        "sandbox": {"allowNetwork": True},
        "requires": ["EXA_API_KEY"],
    }
    print(json.dumps(schema, indent=2))

def call_exa(payload: dict) -> dict:
    req = urllib.request.Request(
        API_URL,
        data=json.dumps(payload).encode("utf-8"),
        headers={"x-api-key": API_KEY, "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        return json.loads(resp.read().decode("utf-8", errors="replace"))

def search(query: str, num_results: int = 0) -> str:
    query = " ".join((query or "").split())
    if not query:
        return "Error: nothing to search for"
    if not API_KEY:
        # Detail names the variable; the channel gets nothing internal.
        toollog.log_detail("websearch", "EXA_API_KEY is not set")
        return "Error: the search backend is not configured"

    n = num_results if num_results else MAX_RESULTS
    n = max(1, min(n, MAX_RESULTS))

    try:
        data = call_exa({
            "query": query,
            "numResults": n,
            "contents": {"text": {"maxCharacters": MAX_CHARS}},
        })
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace")[:300]
        toollog.log_detail("websearch", f"exa http {e.code}: {body}")
        if e.code in (401, 403):
            return "Error: the search backend rejected our credentials"
        if e.code == 429:
            return "Error: search is rate limited right now, try again shortly"
        return "Error: the search backend returned an error"
    except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
        toollog.log_detail("websearch", f"exa unreachable: {e}")
        return "Error: the search backend is unavailable right now"

    results = data.get("results") or []
    if not results:
        return f"No results for {query!r}. Say so plainly rather than inventing an answer."

    cost = (data.get("costDollars") or {}).get("total")
    toollog.log_detail("websearch",
                       f"ok {query[:80]!r} -> {len(results)} results, cost=${cost}")

    out = []
    for i, r in enumerate(results, 1):
        title = (r.get("title") or "untitled").strip()
        url = (r.get("url") or "").strip()
        text = " ".join((r.get("text") or "").split())
        published = r.get("publishedDate") or ""

        block = f"{i}. {title}\n   {url}"
        if published:
            block += f"\n   published: {published[:10]}"
        if text:
            block += f"\n   {text[:MAX_CHARS]}"
        out.append(block)

    return "\n\n".join(out)

def main():
    if len(sys.argv) < 2:
        print("usage: websearch.py --schema | --execute <json>", file=sys.stderr)
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
        print(search(data.get("query") or "", int(data.get("num_results") or 0)))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
