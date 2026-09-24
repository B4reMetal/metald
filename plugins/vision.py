#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
view_image tool for metald.

Configure via environment variables (all optional):
  VISION_API_URL   OpenAI-compatible endpoint (default: http://localhost:8080/v1)
  VISION_MODEL     model name (default: "local")
  VISION_API_KEY   bearer token, if the endpoint needs one
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
from metald_tools.vision import view_image, DEFAULT_QUESTION

def print_schema():
    schema = {
        "title": "view_image",
        "description": (
            "view an image from a URL or local file path using a vision-capable "
            "model, and describe it or answer a specific question about it. use "
            "this whenever a URL in the conversation looks like an image, or "
            "someone asks what's in a picture."
        ),
        "type": "object",
        "properties": {
            "source": {
                "type": "string",
                "description": "http(s) URL of the image",
            },
            "question": {
                "type": "string",
                "description": "what to look for or ask about the image (default: describe it in detail)",
            },
        },
        "required": ["source"],
        "additionalProperties": False,
        # This tool needs outbound network access (to fetch the image and to
        # reach the vision API), which the default sandbox policy blocks.
        "sandbox": {"allowNetwork": True},
    }
    print(json.dumps(schema, indent=2))

def main():
    if len(sys.argv) < 2:
        print("Usage: vision.py [--schema | --execute <json>]")
        sys.exit(1)

    option = sys.argv[1]

    if option == "--schema":
        print_schema()
        return

    if option == "--execute":
        if len(sys.argv) < 3:
            print("Error: Missing JSON input for execution")
            sys.exit(1)
        try:
            input_data = json.loads(sys.argv[2])
        except json.JSONDecodeError:
            print("Error: Invalid JSON input")
            sys.exit(1)

        source = input_data.get("source")
        if not source:
            print("Error: Missing required 'source' in JSON input")
            sys.exit(1)
        question = input_data.get("question") or DEFAULT_QUESTION

        print(view_image(source, question))
        return

    print("Usage: vision.py [--schema | --execute <json>]")
    sys.exit(1)

if __name__ == "__main__":
    main()
