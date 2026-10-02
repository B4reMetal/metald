#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
speak tool for metald.

Configure under env: in config.yml:
  TTS_DELETES_AT     Zipline retention (default: 12h)
  voice settings     see metald_tools/speech.py (COMFYUI_URL, TTS_VOICES, TTS_MAX_CHARS, ...)
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
from metald_tools import comfyui, hosting, speech
from metald_tools.hosting import upload_file, HostingError

DELETES_AT = os.environ.get("TTS_DELETES_AT", "12h")
VOICES = speech.VOICES

def print_schema():
    schema = {
        "title": "speak",
        "description": (
            "say something out loud - turns your words into actual speech and "
            "gives you a url to the audio. use it when someone asks you to say "
            "something aloud, read something out, or when a spoken reply would "
            "land better than text. you must include the returned url in your "
            "reply or nobody hears it. takes a few seconds."
        ),
        "type": "object",
        "properties": {
            "text": {
                "type": "string",
                "description": (
                    "exactly what to say out loud, in plain words. no urls, no emoji - "
                    "they get read literally. the one markup understood is a timed "
                    "pause, written [pause 1.5s], for comic timing or a beat."
                ),
            },
            "style": {
                "type": "string",
                "description": (
                    "how to say it, in a few plain words: emotion, energy, pace, "
                    "manner. e.g. 'furious, almost shouting', 'a soft conspiratorial "
                    "whisper', 'amused, laughing through the words', 'dry and "
                    "unimpressed', 'late-night radio, smooth and low', 'excited "
                    "sports commentator'. match it to what you are saying; leave it "
                    "out for a natural delivery."
                ),
            },
            "language": {
                "type": "string",
                "enum": list(speech.MOSS_LANGUAGES),
                "description": "the language the text is in, if not English",
            },
            "voice": {
                "type": "string",
                "enum": sorted(VOICES) + ["default"],
                "description": (
                    "which voice to use. 'default' is the stock voice; the "
                    "others are accents - pick one that suits what you are "
                    "saying, or leave it out."
                ),
            },
        },
        "required": ["text"],
        "additionalProperties": False,
        # Needs outbound network access (ComfyUI, the hosting backend).
        "sandbox": {"allowNetwork": True},
        "requires": hosting.requires(),
    }
    print(json.dumps(schema, indent=2))

def speak(text: str, voice: str = "", style: str = "", language: str = "") -> str:
    try:
        audio = speech.synthesize(text, voice, style=style, language=language)
    except speech.SpeechError as e:
        return f"Error: {e}"

    name = f"metald_speech_{int(time.time() * 1000)}.flac"
    try:
        url = upload_file(audio, name, "audio/flac", deletes_at=DELETES_AT)
    except HostingError as e:
        toollog.log_detail("tts", f"upload failed: {e}")
        return "Error: speech generated but the upload failed"

    toollog.log_detail("tts", f"ok {url} ({len(audio)} bytes, {len(' '.join(text.split()))} chars, voice={voice or 'default'})")
    return f"url: {url}"

def main():
    if len(sys.argv) < 2:
        print("usage: tts.py --schema | --execute <json>", file=sys.stderr)
        sys.exit(1)
    if sys.argv[1] == "--schema":
        print_schema()
        return
    if sys.argv[1] == "--execute":
        if not comfyui.online():
            print(comfyui.OFFLINE_REPLY)
            return
        try:
            data = json.loads(sys.argv[2]) if len(sys.argv) > 2 else {}
        except json.JSONDecodeError:
            print("Error: could not read the request")
            return
        print(speak(data.get("text") or "", (data.get("voice") or "").strip(), str(data.get("style") or ""),
                    str(data.get("language") or "")))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
