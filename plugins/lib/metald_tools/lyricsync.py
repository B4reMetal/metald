# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Time each lyric line against the audio, for read-along lyrics.

With LYRICS_ALIGN=comfyui, the lyric nodes in ComfyUI (contrib/lyricalign: vocal separation + forced
alignment of the known words) do it. Otherwise, or if that fails, whisper transcribes the song in short
overlapping chunks (over a whole song its 30-second windows lose
the words under the music), and the heard words are matched to the known lyrics, so the lyrics keep
their own wording and only take their timing from the transcript. Lines nothing matched get a time
between their neighbours.
"""

import difflib
import json
import os
import re
import subprocess
import tempfile
import urllib.error
import urllib.request
import uuid

from metald_tools import comfyui, toollog

WHISPER_URL = os.environ.get("WHISPER_URL", "")
ALIGN = os.environ.get("LYRICS_ALIGN", "").strip().lower() == "comfyui"
CHUNK, STEP = 12.0, 10.0
TIMEOUT = 120


def _norm(word):
    return re.sub(r"[^a-z0-9']", "", word.lower()).strip("'")


def _post(wav_bytes):
    b = uuid.uuid4().hex
    fields = {"response_format": "verbose_json", "max_len": "1", "split_on_word": "true"}
    body = b"".join(f"--{b}\r\nContent-Disposition: form-data; name=\"{k}\"\r\n\r\n{v}\r\n".encode()
                    for k, v in fields.items())
    body += (f"--{b}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.wav\"\r\n"
             f"Content-Type: audio/wav\r\n\r\n").encode() + wav_bytes + f"\r\n--{b}--\r\n".encode()
    req = urllib.request.Request(WHISPER_URL.rstrip("/") + "/inference", data=body, method="POST",
                                 headers={"Content-Type": f"multipart/form-data; boundary={b}"})
    with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
        return json.loads(resp.read().decode("utf-8", errors="replace"))


def heard_words(audio_path):
    """[(word, start_seconds)] heard in the audio, in order."""
    words = []
    with tempfile.TemporaryDirectory() as tmp:
        wav = os.path.join(tmp, "a.wav")
        subprocess.run(["ffmpeg", "-loglevel", "error", "-y", "-i", audio_path, "-ar", "16000", "-ac", "1",
                        "-c:a", "pcm_s16le", wav], check=True, capture_output=True, timeout=300)
        duration = (os.path.getsize(wav) - 44) / 32000
        chunk = os.path.join(tmp, "c.wav")
        offset = 0.0
        while offset < duration:
            subprocess.run(["ffmpeg", "-loglevel", "error", "-y", "-ss", str(offset), "-t", str(CHUNK),
                            "-i", wav, "-c", "copy", chunk], check=True, capture_output=True, timeout=60)
            with open(chunk, "rb") as f:
                result = _post(f.read())
            # Chunks overlap by CHUNK - STEP seconds, split between them: whisper pins the first word of
            # a chunk to its very start, and cuts off the last one.
            lo = offset + (CHUNK - STEP) / 2 if offset else 0.0
            hi = offset + STEP + (CHUNK - STEP) / 2 if offset + STEP < duration else float("inf")
            for seg in result.get("segments", []):
                w, start = _norm(seg.get("text", "")), offset + float(seg.get("start", 0))
                if w and lo <= start < hi:
                    words.append((w, start))
            offset += STEP
    return words


def align(lyrics, words):
    """[(seconds, line)] for every lyric line, section labels included, in order."""
    lines = lyrics.splitlines()
    lyric_words = [(_norm(w), i) for i, line in enumerate(lines) if not line.strip().startswith("[")
                   for w in line.split() if _norm(w)]
    matcher = difflib.SequenceMatcher(None, [w for w, _ in lyric_words], [w for w, _ in words], autojunk=False)
    times = {}
    for block in matcher.get_matching_blocks():
        for k in range(block.size):
            times.setdefault(lyric_words[block.a + k][1], words[block.b + k][1])

    sung = [i for i, line in enumerate(lines) if line.strip() and not line.strip().startswith("[")]
    anchors = [(n, times[i]) for n, i in enumerate(sung) if i in times]
    if not anchors:
        return []

    def at(n):
        before = [a for a in anchors if a[0] <= n]
        after = [a for a in anchors if a[0] > n]
        if before and after:
            (n0, t0), (n1, t1) = before[-1], after[0]
            return t0 + (t1 - t0) * (n - n0) / (n1 - n0)
        return before[-1][1] if before else after[0][1]

    line_time = {i: at(n) for n, i in enumerate(sung)}
    # Matching can pair a repeated chorus with the wrong repeat; lines never go back in time.
    out, floor = [], 0.0
    for i, line in enumerate(lines):
        if not line.strip():
            continue
        # A section label takes the time of the line it introduces.
        t = line_time.get(i, next((line_time[j] for j in sung if j > i), floor))
        floor = max(floor, t)
        out.append((floor, line.strip()))
    return out


def to_lrc(timed):
    return "\n".join(f"[{int(t // 60):02d}:{t % 60:05.2f}]{line}" for t, line in timed)


def aligned(audio_path, lyrics):
    """[(seconds, line)] from ComfyUI's lyric aligner, or None if it is not set up or fails."""
    if not ALIGN:
        return None
    log = lambda m: toollog.log_detail("lyricsync", m)
    try:
        with open(audio_path, "rb") as f:
            name = comfyui.upload(f.read(), f"metald_align_{uuid.uuid4().hex}{os.path.splitext(audio_path)[1]}", log)
        out = comfyui.result({"prompt": {"1": {"class_type": "MetaldLyricAlign",
                                               "inputs": {"audio_file": name, "lyrics": lyrics}}}},
                             "lyrics", 540, log)
        return [(float(t), str(line)) for t, line in json.loads(out)["lines"]]
    except (OSError, ValueError, TypeError, KeyError, RuntimeError) as e:
        log(f"aligner failed, falling back to whisper: {e}")
        return None


def synced_lyrics(audio_path, lyrics):
    """LRC text for lyrics sung in audio_path, or "" when it can't be worked out."""
    if not lyrics.strip():
        return ""
    timed = aligned(audio_path, lyrics)
    if timed:
        toollog.log_detail("lyricsync", f"aligner timed {len(timed)} lines")
        return to_lrc(timed)
    if not WHISPER_URL:
        return ""
    try:
        words = heard_words(audio_path)
    except (OSError, ValueError, subprocess.SubprocessError, urllib.error.URLError) as e:
        toollog.log_detail("lyricsync", f"transcription failed: {e}")
        return ""
    timed = align(lyrics, words)
    toollog.log_detail("lyricsync", f"{len(words)} words heard, {len(timed)} lines timed")
    return to_lrc(timed)
