#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
post_cat_picture tool for metald.

Configure via environment variables (all optional, shared with vision.py):
  VISION_API_URL   base URL of an OpenAI-compatible API (default: http://localhost:8080/v1)
  VISION_MODEL     model name to send in the request (default: "local")
  VISION_API_KEY   bearer token, if needed
"""

import sys
import os
import json
import base64
import urllib.request
import urllib.error

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog
from metald_tools import vision

CAT_API_URL = "https://api.thecatapi.com/v1/images/search"
REQUEST_TIMEOUT = 20

ROAST_PROMPT = os.environ.get("CAT_PIC_ROAST_PROMPT", "")

def build_roast_prompt(nick: str) -> str:
    who = nick if nick else "whoever asked for this"
    return ROAST_PROMPT.format(who=who)

def print_schema():
    schema = {
        "title": "post_cat_picture",
        "description": (
            "fetch a random cat picture and get a rude, sarcastic roast that "
            "mocks BOTH the cat and the person who asked for it. pass their "
            "nick so the roast targets them by name. returns the image url "
            "and the roast. include the url in your reply so the picture "
            "actually gets posted, and deliver the roast as your own words - "
            "rewrite it in your voice, don't quote it and don't label it "
            "('the roast:' etc). this tool has ALREADY looked at the image, "
            "so do not call view_image on the url it returns."
        ),
        "type": "object",
        "properties": {
            "nick": {
                "type": "string",
                "description": "the nickname of the person who requested the cat picture, so the roast can target them",
            },
        },
        "additionalProperties": False,
        # Needs outbound network access (cat API, image fetch, vision API).
        "sandbox": {"allowNetwork": True},
        "requires": ["CAT_PIC_ROAST_PROMPT"],
    }
    print(json.dumps(schema, indent=2))

def fetch_random_cat_url() -> str:
    req = urllib.request.Request(CAT_API_URL, headers={"User-Agent": "metald-cat-tool"})
    with urllib.request.urlopen(req, timeout=REQUEST_TIMEOUT) as resp:
        data = json.loads(resp.read())
    if not data or not isinstance(data, list) or "url" not in data[0]:
        raise ValueError("cat API returned no image")
    return data[0]["url"]

def post_cat_picture(nick: str = "") -> str:
    try:
        cat_url = fetch_random_cat_url()
    except (urllib.error.URLError, urllib.error.HTTPError) as e:
        toollog.log_detail("cat_pic", f"thecatapi unreachable: {e}")
        return "Error: the cat picture service is unavailable right now"
    except (ValueError, json.JSONDecodeError) as e:
        toollog.log_detail("cat_pic", f"bad response from thecatapi: {e}")
        return "Error: the cat picture service returned something unusable"

    roast = vision.view_image(cat_url, build_roast_prompt(nick))
    if roast.startswith("Error:"):
        # Still hand back the URL even if the roast failed - better to post
        # the picture than nothing.
        return f"url: {cat_url}\nroast: (couldn't get a roast: {roast})"

    return f"url: {cat_url}\nroast: {roast}"

def main():
    if len(sys.argv) < 2:
        print("Usage: cat_pic.py [--schema | --execute <json>]")
        sys.exit(1)

    option = sys.argv[1]

    if option == "--schema":
        print_schema()
        return

    if option == "--execute":
        nick = ""
        if len(sys.argv) >= 3:
            try:
                input_data = json.loads(sys.argv[2])
                nick = input_data.get("nick") or ""
            except json.JSONDecodeError:
                pass
        print(post_cat_picture(nick))
        return

    print("Usage: cat_pic.py [--schema | --execute <json>]")
    sys.exit(1)

if __name__ == "__main__":
    main()
