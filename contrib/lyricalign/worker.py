# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Give the radio's songs read-along lyrics: ask the radio (contrib/radio) which songs have no timed
lyrics, have ComfyUI's lyric nodes (comfyui/ here) time the lyrics they came with, or write them from the
vocals when they came with none, and send the result back. It runs beside ComfyUI because the radio
can't reach it; instead it holds a request open that the radio answers as soon as a song needs lyrics.

RADIO_API_URL  the radio API, e.g. https://radio.example.com/api (required)
RADIO_TOKEN    the radio's token (required)
COMFYUI_URL    ComfyUI with the lyric nodes installed (default: http://127.0.0.1:8188)
"""

import json
import os
import time
import uuid
import urllib.error
import urllib.parse
import urllib.request

API = os.environ["RADIO_API_URL"].rstrip("/")
TOKEN = os.environ["RADIO_TOKEN"]
COMFY = os.environ.get("COMFYUI_URL", "http://127.0.0.1:8188").rstrip("/")


class ComfyFailed(Exception):
    """ComfyUI ran the job and it failed: the song can't be done, so it isn't tried again."""


def radio(path, body=None, raw=False):
    req = urllib.request.Request(API + path, data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as r:
        return r.read() if raw else json.load(r)


def comfy(node, audio, name, **inputs):
    """Run one lyric node on the audio: upload it (the node deletes it once read), queue the node, and wait
    for its answer."""
    boundary = uuid.uuid4().hex
    body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"image\"; filename=\"{name}\"\r\n\r\n").encode() + \
        audio + f"\r\n--{boundary}--\r\n".encode()
    up = urllib.request.Request(COMFY + "/upload/image", data=body,
                                headers={"Content-Type": f"multipart/form-data; boundary={boundary}"})
    with urllib.request.urlopen(up, timeout=120) as r:
        uploaded = json.load(r)["name"]
    prompt = {"prompt": {"1": {"class_type": node, "inputs": {"audio_file": uploaded, **inputs}}},
              "extra_data": {"metald": {"tool": "lyricworker"}}}
    req = urllib.request.Request(COMFY + "/prompt", data=json.dumps(prompt).encode(),
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        pid = json.load(r)["prompt_id"]
    deadline = time.time() + 900
    while time.time() < deadline:
        with urllib.request.urlopen(f"{COMFY}/history/{pid}", timeout=30) as r:
            entry = json.load(r).get(pid)
        if entry:
            if entry.get("status", {}).get("status_str") == "error":
                raise ComfyFailed(json.dumps(entry["status"].get("messages"))[:300])
            for out in entry.get("outputs", {}).values():
                if out.get("lyrics"):
                    return json.loads(out["lyrics"][0])
            if entry.get("status", {}).get("completed"):
                raise ComfyFailed("no result")
        time.sleep(1)
    raise TimeoutError("comfyui took too long")


def lyrics_for(track):
    audio = radio("/track/" + urllib.parse.quote(track["file"]), raw=True)
    name = f"metald_lyrics_{uuid.uuid4().hex}{os.path.splitext(track['file'])[1]}"
    if track["lyrics"].strip():
        lines = comfy("MetaldLyricAlign", audio, name, lyrics=track["lyrics"])["lines"]
        return {"lines": lines} if lines else {"failed": True}
    out = comfy("MetaldLyricTranscribe", audio, name)
    if out.get("instrumental"):
        return {"instrumental": True}
    return {"lines": out["lines"], "transcribed": True} if out["lines"] else {"failed": True}


def main():
    print("lyrics worker for", API, flush=True)
    while True:
        try:
            todo = radio("/lyrics/pending?wait=55")["tracks"]
        except (urllib.error.URLError, OSError, ValueError) as e:
            print("radio unreachable:", e, flush=True)
            time.sleep(10)  # mostly the radio's API restarting
            continue
        for track in todo:
            try:
                result = lyrics_for(track)
            except ComfyFailed as e:
                print(track["file"], "failed:", e, flush=True)
                result = {"failed": True}  # ComfyUI couldn't do this song: don't try it forever
            except urllib.error.HTTPError as e:
                print(track["file"], "refused:", e.code, e.url, flush=True)
                time.sleep(60)
                break
            except (urllib.error.URLError, OSError, ValueError) as e:
                print(track["file"], "will retry:", e, flush=True)
                time.sleep(60)
                break
            try:
                radio("/lyrics", {"file": track["file"], **result})
            except (urllib.error.URLError, OSError, ValueError) as e:
                print(track["file"], "result not saved:", e, flush=True)
                break
            print(track["file"], next(k for k in ("instrumental", "failed", "transcribed", "lines") if k in result),
                  flush=True)


if __name__ == "__main__":
    main()
