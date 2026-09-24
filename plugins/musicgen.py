#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
generate_music tool for metald.

Configure under env: in config.yml:
  COMFYUI_URL          base URL of the ComfyUI server (default: http://127.0.0.1:8188)
  MUSIC_GEN_CKPT       YuE2 checkpoint (default: yue2_3b_bf16.safetensors)
  MUSIC_GEN_STEPS      sampler steps (default: 32)
  MUSIC_GEN_MAX_SECONDS  upper bound on song length in seconds (default: 300 = 5:00)
  MUSIC_DELETES_AT     Zipline retention for generated music (default: 12h)
  MUSIC_SAFETY_REVIEW  "off" disables the lyric/style safety check
  LYRICIST_*           lyricist sub-step, see lyricist.py
  hosting              see metald_tools/hosting.py (UPLOAD_BACKEND, UPLOAD_HEADERS, ...)
"""

import sys
import os
import json
import time
import urllib.request
import urllib.error
import urllib.parse

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog
from metald_tools import safetyreview
from metald_tools import lyricist
from metald_tools.media import strip_audio_metadata as strip_metadata
from metald_tools import comfyui, hosting
from metald_tools.hosting import upload_file, HostingError

CKPT_NAME = os.environ.get("MUSIC_GEN_CKPT", "yue2_3b_bf16.safetensors")
GEN_STEPS = int(os.environ.get("MUSIC_GEN_STEPS", "32"))

# max_duration is a hard ceiling that stops the song mid-phrase, so the default is the maximum.
MAX_SECONDS = float(os.environ.get("MUSIC_GEN_MAX_SECONDS", "300"))

DELETES_AT = os.environ.get("MUSIC_DELETES_AT", "12h")


# Must stay below metald's apitimeout, or the agent gives up while the render is still running.
POLL_TIMEOUT = 540
UPLOAD_TIMEOUT = 90

SAFETY_POLICY = os.environ.get("MUSIC_SAFETY_POLICY", "")

def requires() -> list:
    req = ["MUSIC_SAFETY_POLICY"] + hosting.requires() + safetyreview.requires("MUSIC_SAFETY_REVIEW")
    if lyricist.enabled():
        req += ["LYRICIST_PROMPT", "LYRICIST_FORMAT"]
    return req

def print_schema():
    schema = {
        "title": "song",
        "description": (
            "generate a song from a style description and get back a public url "
            "to the track. it can sing: write the lyrics yourself and a lyricist "
            "edits them before they are sung - you must supply lyrics unless "
            "instrumental is set. the result "
            "includes the lyrics as sung, for your reference - do not paste them "
            "all into the channel unless asked. you must include the returned url in your reply so "
            "the track actually gets posted. it takes a minute or two, so say "
            "you're working on it if it fits the conversation. do not comment "
            "on the length of the song or how long the link lasts."
        ),
        "type": "object",
        "properties": {
            "style": {
                "type": "string",
                "description": (
                    "the genre, instruments, mood and vocal character - e.g. "
                    "'slow doom metal, downtuned guitars, growled male vocals'. "
                    "be concrete; this drives everything."
                ),
            },
            "lyrics": {
                "type": "string",
                "description": (
                    "your draft of the words, with [Verse] and [Chorus] tags on "
                    "their own lines. the lyricist keeps the ideas, voice and "
                    "best lines and improves the rest. required unless "
                    "instrumental."
                ),
            },
            "brief": {
                "type": "string",
                "description": (
                    "optional notes for the lyricist: what you were going for, "
                    "lines that must stay, names that must appear. say 'use "
                    "exactly' to sing the draft verbatim."
                ),
            },
            "instrumental": {
                "type": "boolean",
                "description": "true for no vocals at all.",
            },
        },
        "required": ["style"],
        "additionalProperties": False,
        # Needs outbound network access (ComfyUI, the hosting backend).
        "sandbox": {"allowNetwork": True},
        "requires": requires(),
    }
    print(json.dumps(schema, indent=2))

def build_workflow(style: str, lyrics: str, seconds: float, seed: int) -> dict:
    return {
        "prompt": {
            "1": {"class_type": "CheckpointLoaderSimple", "inputs": {"ckpt_name": CKPT_NAME}},
            # Stage one: write the score. Both stages get style AND lyrics -
            # the score has to know where the words go.
            "2": {"class_type": "YuE2GenerateABC", "inputs": {
                "clip": ["1", 1], "style": style, "lyrics": lyrics,
                "seed": seed, "mode": "full", "max_abc_tokens": 8192,
                "temperature": 0.7, "top_p": 0.9, "top_k": 30,
                "repetition_penalty": 1.005, "penalty_window": 100,
            }},
            # Stage two: render the score. Outputs CONDITIONING and, as output
            # 1, the duration it actually chose.
            "3": {"class_type": "YuE2GenerateMusic", "inputs": {
                "clip": ["1", 1], "style": style, "lyrics": lyrics,
                "abc": ["2", 0], "seed": seed, "mode": "full",
                "max_duration": seconds,
                "temperature": 1.0, "top_p": 0.95, "top_k": 100,
                "repetition_penalty": 1.2,
            }},
            "4": {"class_type": "ConditioningZeroOut", "inputs": {"conditioning": ["3", 0]}},
            # seconds is wired FROM the music node, never hardcoded: the sampler raises "latent
            # duration must match the seconds output of YuE2 Text Encode" on any mismatch.
            "5": {"class_type": "EmptyYuE2LatentAudio", "inputs": {
                "seconds": ["3", 1], "batch_size": 1,
            }},
            "6": {"class_type": "KSampler", "inputs": {
                "seed": seed, "steps": GEN_STEPS, "cfg": 1.0,
                "sampler_name": "dpm_2", "scheduler": "sgm_uniform", "denoise": 1.0,
                "model": ["1", 0], "positive": ["3", 0], "negative": ["4", 0],
                "latent_image": ["5", 0],
            }},
            "7": {"class_type": "VAEDecodeAudio", "inputs": {"samples": ["6", 0], "vae": ["1", 2]}},
            "8": {"class_type": "SaveAudio", "inputs": {
                "audio": ["7", 0], "filename_prefix": "metald_music",
            }},
        }
    }

def generate(style: str, brief: str, lyrics: str, instrumental: bool, seconds: float) -> str:
    style = style.strip()
    brief = brief.strip()
    lyrics = lyrics.strip()
    if not style:
        return "Error: no style given"

    if instrumental:
        lyrics = ""
    elif not lyrics:
        return "Error: write draft lyrics first, or set instrumental"
    elif lyricist.enabled():
        try:
            lyrics = lyricist.write(brief, lyrics, style, seconds,
                                    logger=lambda m: toollog.log_detail("music_gen", m))
        except lyricist.LyricistError:
            toollog.log_detail("music_gen", "lyricist failed; singing the draft as given")

    if safetyreview.enabled("MUSIC_SAFETY_REVIEW"):
        content = f"STYLE: {style}\n\nLYRICS:\n{lyrics or '(instrumental)'}"
        allowed, reason = safetyreview.review(
            SAFETY_POLICY, content, label="SONG REQUEST",
            logger=lambda m: toollog.log_detail("music_gen", m))
        if not allowed:
            toollog.log_detail("music_gen", f"refused ({reason}): {content[:300]}")
            return f"Refused: {reason}. Say so in your own voice; do not retry the same thing."

    seed = int(time.time() * 1000) % (2**31)
    try:
        audio = comfyui.run(build_workflow(style, lyrics, seconds, seed), "audio", POLL_TIMEOUT,
                            log=lambda m: toollog.log_detail("music_gen", m))
    except RuntimeError:
        # Detail is in the tool log; the channel gets nothing internal.
        return "Error: the music backend is unavailable right now"

    before = len(audio)
    audio = strip_metadata(audio)
    if len(audio) != before:
        toollog.log_detail("music_gen",
                           f"stripped {before - len(audio)} bytes of ComfyUI workflow metadata")

    name = f"metald_{int(time.time() * 1000)}.flac"
    try:
        url = upload_file(audio, name, "audio/flac", deletes_at=DELETES_AT)
    except HostingError as e:
        toollog.log_detail("music_gen", f"upload failed: {e}")
        return "Error: track generated but the upload failed"

    toollog.log_detail("music_gen",
                       f"ok {url} ({len(audio)} bytes, style={style[:60]!r}, sung={bool(lyrics)})")
    if not lyrics:
        return f"url: {url}"
    return f"url: {url}\n\nlyrics as sung (reference only):\n{lyrics}"

def main():
    if len(sys.argv) < 2:
        print("usage: music_gen.py --schema | --execute <json>", file=sys.stderr)
        sys.exit(1)

    if sys.argv[1] == "--schema":
        print_schema()
        return

    if sys.argv[1] == "--execute":
        if not SAFETY_POLICY.strip():
            print("Error: music safety policy is not configured (set MUSIC_SAFETY_POLICY)")
            return
        if lyricist.enabled() and not lyricist.configured():
            print("Error: lyricist prompt is not configured (set LYRICIST_PROMPT, or LYRICIST=off)")
            return
        try:
            data = json.loads(sys.argv[2]) if len(sys.argv) > 2 else {}
        except json.JSONDecodeError:
            print("Error: could not read the request")
            return
        seconds = float(data.get("seconds") or MAX_SECONDS)
        seconds = max(10.0, min(seconds, MAX_SECONDS))
        print(generate(data.get("style") or "", data.get("brief") or "",
                       data.get("lyrics") or "", bool(data.get("instrumental")), seconds))
        return

    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
