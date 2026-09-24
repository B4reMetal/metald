#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
transcribe tool for metald.

Configure under env: in config.yml:
  WHISPER_URL        whisper.cpp server (default: http://127.0.0.1:8083)
  STT_MAX_SECONDS    longest audio accepted (default: 600 = 10 minutes)
  STT_MAX_BYTES      largest download accepted (default: 200 MB)
  STT_MAX_REPLY      transcript characters returned to the model (default: 2000)
"""

import sys
import os
import json
import socket
import ipaddress
import subprocess
import tempfile
import urllib.parse
import urllib.request
import urllib.error

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog
from metald_tools import urlguard

WHISPER_URL = os.environ.get("WHISPER_URL", "http://127.0.0.1:8083")
MAX_SECONDS = float(os.environ.get("STT_MAX_SECONDS", "600"))
MAX_BYTES = int(os.environ.get("STT_MAX_BYTES", str(200 * 1024 * 1024)))
MAX_REPLY = int(os.environ.get("STT_MAX_REPLY", "2000"))

FETCH_TIMEOUT = 120
WHISPER_TIMEOUT = 900  # 10 minutes of audio takes a while even on Metal

def print_schema():
    schema = {
        "title": "transcribe",
        "description": (
            "listen to an audio or video link someone posted and get back what "
            "was actually said. use it when a link to audio, a voice note, a "
            "song or a video is dropped in the channel and the content matters "
            "- don't guess at what's in it, transcribe it. returns the words, "
            "not a summary; say what you make of them in your own voice. long "
            "recordings take a while."
        ),
        "type": "object",
        "properties": {
            "url": {
                "type": "string",
                "description": "direct http(s) link to the audio or video file",
            },
        },
        "required": ["url"],
        "additionalProperties": False,
        # Needs outbound network access (the URL, and the local whisper server).
        "sandbox": {"allowNetwork": True},
    }
    print(json.dumps(schema, indent=2))

def fetch(url: str, dest: str):
    req = urllib.request.Request(url, headers={"User-Agent": "metald/transcribe"})
    # No redirect following: a public URL that redirects to 169.254.169.254 or
    # a LAN address would sail past the check above.
    with urlguard.opener().open(req, timeout=FETCH_TIMEOUT) as resp:
        size = 0
        with open(dest, "wb") as fh:
            while True:
                chunk = resp.read(1 << 20)
                if not chunk:
                    break
                size += len(chunk)
                if size > MAX_BYTES:
                    raise ValueError("file too large")
                fh.write(chunk)
    return size

def duration_of(path: str):
    out = subprocess.run(
        ["ffprobe", "-v", "error", "-show_entries", "format=duration",
         "-of", "default=nw=1:nk=1", path],
        capture_output=True, text=True, timeout=60)
    try:
        return float(out.stdout.strip())
    except ValueError:
        return None

def transcribe(url: str) -> str:
    parsed, err = urlguard.safe_url(url.strip())
    if err:
        toollog.log_detail("stt", f"rejected url {url[:120]!r}: {err}")
        return f"Error: {err}"

    with tempfile.TemporaryDirectory() as tmp:
        raw = os.path.join(tmp, "in")
        wav = os.path.join(tmp, "out.wav")
        try:
            size = fetch(url, raw)
        except ValueError:
            return f"Error: that file is too big (limit {MAX_BYTES // (1024*1024)} MB)"
        except (urllib.error.URLError, urllib.error.HTTPError, OSError) as e:
            toollog.log_detail("stt", f"fetch failed for {url[:120]!r}: {e}")
            return "Error: could not download that link"

        # Duration is checked BEFORE decoding: a ten-hour stream should be
        # refused on its metadata, not after spending ten minutes on ffmpeg.
        dur = duration_of(raw)
        if dur is None:
            return "Error: that does not look like audio or video"
        if dur > MAX_SECONDS:
            return (f"Error: that is {int(dur // 60)} minutes long - the limit is "
                    f"{int(MAX_SECONDS // 60)} minutes")

        # whisper wants 16 kHz mono PCM; ffmpeg also normalises the container,
        # so a video link works as well as a bare audio file.
        conv = subprocess.run(
            ["ffmpeg", "-loglevel", "error", "-y", "-i", raw,
             "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wav],
            capture_output=True, text=True, timeout=600)
        if conv.returncode != 0 or not os.path.exists(wav):
            toollog.log_detail("stt", f"ffmpeg failed: {conv.stderr[:200]}")
            return "Error: could not decode that audio"

        try:
            with open(wav, "rb") as fh:
                body, headers = multipart(fh.read())
            req = urllib.request.Request(
                WHISPER_URL.rstrip("/") + "/inference", data=body,
                headers=headers, method="POST")
            with urllib.request.urlopen(req, timeout=WHISPER_TIMEOUT) as resp:
                result = json.loads(resp.read().decode("utf-8", errors="replace"))
        except (urllib.error.URLError, OSError, json.JSONDecodeError) as e:
            # Detail names a local port; the channel gets nothing internal.
            toollog.log_detail("stt", f"whisper unreachable at {WHISPER_URL}: {e}")
            return "Error: the transcription backend is unavailable right now"

    text = (result.get("text") or "").strip()
    if not text:
        return "Nothing intelligible in that audio. Say so plainly."

    toollog.log_detail("stt", f"ok {url[:80]!r} {size} bytes, {dur:.0f}s -> {len(text)} chars")
    if len(text) > MAX_REPLY:
        text = text[:MAX_REPLY] + f"... [truncated, {int(dur // 60)}m of audio]"
    return f"transcript ({int(dur)}s of audio):\n{text}"

def multipart(data: bytes):
    import uuid
    b = uuid.uuid4().hex
    body = (f"--{b}\r\nContent-Disposition: form-data; name=\"file\"; "
            f"filename=\"a.wav\"\r\nContent-Type: audio/wav\r\n\r\n").encode()
    body += data
    body += f"\r\n--{b}\r\nContent-Disposition: form-data; name=\"response_format\"\r\n\r\njson\r\n--{b}--\r\n".encode()
    return body, {"Content-Type": f"multipart/form-data; boundary={b}"}

def main():
    if len(sys.argv) < 2:
        print("usage: stt.py --schema | --execute <json>", file=sys.stderr)
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
        print(transcribe(data.get("url") or ""))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
