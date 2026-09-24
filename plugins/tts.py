#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
speak tool for metald.

Configure via environment variables (see .env at the repo root):
  COMFYUI_URL          ComfyUI server (default: http://127.0.0.1:8188)
  TTS_VOICES         extra voices, name=clip.wav,... (clips in ComfyUI's input/)
  TTS_MAX_CHARS      longest text accepted (default: 800)
  TTS_DELETES_AT     Zipline retention (default: 12h)
  TTS_EXAGGERATION   delivery intensity, 0.25-2.0 (default: 0.5)
  TTS_CFG_WEIGHT     pacing/guidance, lower = slower and more expressive (default: 0.5)
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
from metald_tools import comfyui, hosting
from metald_tools.hosting import upload_file, HostingError
from metald_tools.media import strip_audio_metadata as strip_metadata

MAX_CHARS = int(os.environ.get("TTS_MAX_CHARS", "800"))
DELETES_AT = os.environ.get("TTS_DELETES_AT", "12h")
EXAGGERATION = float(os.environ.get("TTS_EXAGGERATION", "0.5"))

CFG_WEIGHT = float(os.environ.get("TTS_CFG_WEIGHT", "0.5"))

def parse_voices(raw: str) -> dict:
    """TTS_VOICES: comma-separated name=file pairs; each file is a short
    reference clip in ComfyUI's input folder (voice_scottish_f=voice_scottish_f.wav)."""
    out = {}
    for part in raw.split(","):
        name, _, fname = part.partition("=")
        if name.strip() and fname.strip():
            out[name.strip()] = fname.strip()
    return out

VOICES = parse_voices(os.environ.get("TTS_VOICES", ""))

POLL_TIMEOUT = 540  # can queue behind other renders on the shared GPU; under apitimeout (10m)

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
                    "exactly what to say out loud, in plain words. no markup, "
                    "no urls, no emoji - they get read literally."
                ),
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

def build_workflow(text: str, seed: int, voice: str = "") -> dict:
    wf = {
        "prompt": {
            "1": {"class_type": "FL_ChatterboxTTS", "inputs": {
                "text": text,
                "exaggeration": EXAGGERATION,
                "cfg_weight": CFG_WEIGHT,
                "temperature": 0.8,
                "seed": seed,
                # Keep the model resident: a cold load takes ~45s, a warm run ~5s.
                "keep_model_loaded": True,
            }},
            "2": {"class_type": "SaveAudio", "inputs": {
                "audio": ["1", 0], "filename_prefix": "metald_speech",
            }},
        }
    }
    # audio_prompt is optional on the node: wired only when a voice is asked
    # for, so the stock voice stays the zero-config path.
    if voice and voice in VOICES:
        wf["prompt"]["3"] = {"class_type": "LoadAudio",
                             "inputs": {"audio": VOICES[voice]}}
        wf["prompt"]["1"]["inputs"]["audio_prompt"] = ["3", 0]
    return wf

def speak(text: str, voice: str = "") -> str:
    text = " ".join(text.split())
    if not text:
        return "Error: nothing to say"
    if len(text) > MAX_CHARS:
        return f"Error: too long to read out ({len(text)} chars, limit {MAX_CHARS})"

    seed = int(time.time() * 1000) % (2**31)
    try:
        audio = comfyui.run(build_workflow(text, seed, voice), "audio", POLL_TIMEOUT,
                            log=lambda m: toollog.log_detail("tts", m))
    except RuntimeError:
        # Detail is in the tool log; the channel gets nothing internal.
        return "Error: the speech backend is unavailable right now"

    before = len(audio)
    audio = strip_metadata(audio)
    if len(audio) != before:
        toollog.log_detail("tts", f"stripped {before - len(audio)} bytes of workflow metadata")

    name = f"metald_speech_{int(time.time() * 1000)}.flac"
    try:
        url = upload_file(audio, name, "audio/flac", deletes_at=DELETES_AT)
    except HostingError as e:
        toollog.log_detail("tts", f"upload failed: {e}")
        return "Error: speech generated but the upload failed"

    toollog.log_detail("tts", f"ok {url} ({len(audio)} bytes, {len(text)} chars, voice={voice or 'default'})")
    return f"url: {url}"

def main():
    if len(sys.argv) < 2:
        print("usage: tts.py --schema | --execute <json>", file=sys.stderr)
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
        print(speak(data.get("text") or "", (data.get("voice") or "").strip()))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
