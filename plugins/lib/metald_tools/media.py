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

def _flac_blocks(data: bytes):
    """Split a FLAC file into its metadata blocks and the offset where audio frames start, or None."""
    if len(data) < 8 or data[:4] != b"fLaC":
        return None
    i, blocks = 4, []
    while i + 4 <= len(data):
        header = data[i]
        length = int.from_bytes(data[i + 1:i + 4], "big")
        if i + 4 + length > len(data):
            return None                      # truncated / not what we think
        blocks.append((header & 0x7F, data[i + 4:i + 4 + length]))
        i += 4 + length
        if header >> 7:
            break
    else:
        return None
    if not blocks or blocks[0][0] != 0:
        return None                          # no STREAMINFO: refuse to touch it
    return blocks, i

def _flac_join(blocks, audio: bytes) -> bytes:
    out = bytearray(b"fLaC")
    for n, (btype, body) in enumerate(blocks):
        out.append((0x80 if n == len(blocks) - 1 else 0x00) | btype)
        out += len(body).to_bytes(3, "big")
        out += body
    return bytes(out) + audio

def strip_flac_metadata(data: bytes) -> bytes:
    """Drop every FLAC metadata block except STREAMINFO and SEEKTABLE.

    ComfyUI embeds the workflow in VORBIS_COMMENT. Returns the input unchanged on anything
    unparseable: a leak is better than a corrupt file.
    """
    parsed = _flac_blocks(data)
    if parsed is None:
        return data
    blocks, audio = parsed
    return _flac_join([b for b in blocks if b[0] in (0, 3)], data[audio:])

def tag_flac(data: bytes, tags: dict) -> bytes:
    """Replace a FLAC file's Vorbis comments with tags (e.g. LYRICS), leaving the audio untouched."""
    parsed = _flac_blocks(data)
    if parsed is None:
        return data
    blocks, audio = parsed
    vendor = b"metald"
    body = bytearray(len(vendor).to_bytes(4, "little") + vendor)
    items = [f"{k.upper()}={v}".encode() for k, v in tags.items() if v]
    body += len(items).to_bytes(4, "little")
    for item in items:
        body += len(item).to_bytes(4, "little") + item
    if len(body) >= 1 << 24:
        return data
    return _flac_join([b for b in blocks if b[0] != 4] + [(4, bytes(body))], data[audio:])

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

