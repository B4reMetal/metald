# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Strip the workflow ComfyUI embeds in generated media before it is posted."""

import os
import shutil
import tempfile
import subprocess

def strip_id3(data: bytes) -> bytes:
    """Remove the leading ID3v2 tag, where ComfyUI embeds the whole workflow.

    Byte-level, so the audio is untouched. Returns the input unchanged if the
    header is unparseable: a leak is better than a corrupt file.
    """
    if len(data) < 10 or data[:3] != b"ID3":
        return data
    size = ((data[6] & 0x7F) << 21 | (data[7] & 0x7F) << 14
            | (data[8] & 0x7F) << 7 | (data[9] & 0x7F))
    end = 10 + size
    if size <= 0 or end >= len(data):
        return data
    # Only trust the computed offset if real audio starts there.
    if not (data[end] == 0xFF and (data[end + 1] & 0xE0) == 0xE0):
        return data
    return data[end:]

def strip_flac_metadata(data: bytes) -> bytes:
    """Drop every FLAC metadata block except STREAMINFO and SEEKTABLE.

    ComfyUI embeds the workflow in VORBIS_COMMENT. The last-block flag is reset
    on whichever block ends up last. Returns the input unchanged on anything
    unparseable: a leak is better than a corrupt file.
    """
    if len(data) < 8 or data[:4] != b"fLaC":
        return data

    KEEP = {0, 3}  # STREAMINFO, SEEKTABLE
    i = 4
    kept = []
    while i + 4 <= len(data):
        header = data[i]
        last = header >> 7
        btype = header & 0x7F
        length = int.from_bytes(data[i + 1:i + 4], "big")
        if i + 4 + length > len(data):
            return data                      # truncated / not what we think
        if btype in KEEP:
            kept.append((btype, data[i + 4:i + 4 + length]))
        i += 4 + length
        if last:
            break
    else:
        return data

    if not kept or kept[0][0] != 0:
        return data                          # no STREAMINFO: refuse to touch it

    out = bytearray(b"fLaC")
    for n, (btype, body) in enumerate(kept):
        is_last = 0x80 if n == len(kept) - 1 else 0x00
        out.append(is_last | btype)
        out += len(body).to_bytes(3, "big")
        out += body
    out += data[i:]                          # audio frames, untouched
    return bytes(out)

def strip_audio_metadata(data: bytes) -> bytes:
    """Remove generator metadata, dispatching on the container."""
    if data[:4] == b"fLaC":
        return strip_flac_metadata(data)
    return strip_id3(data)

def strip_mp4_metadata(data: bytes) -> bytes:
    """ComfyUI writes the whole workflow into the mp4's metadata. Remux
    without it (no re-encode). Raises rather than posting a leaky file."""
    ffmpeg = shutil.which("ffmpeg")
    if not ffmpeg:
        raise RuntimeError("ffmpeg not found")
    with tempfile.TemporaryDirectory() as d:
        src, dst = os.path.join(d, "in.mp4"), os.path.join(d, "out.mp4")
        with open(src, "wb") as fh:
            fh.write(data)
        r = subprocess.run([ffmpeg, "-v", "error", "-y", "-i", src, "-map", "0", "-map_metadata", "-1",
                            "-c", "copy", "-movflags", "+faststart", dst], capture_output=True, timeout=120)
        if r.returncode != 0:
            raise RuntimeError("remux failed: " + r.stderr.decode("utf-8", "replace")[:200])
        with open(dst, "rb") as fh:
            return fh.read()

