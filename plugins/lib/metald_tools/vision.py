# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Look at images with a vision-capable model.\n\nVISION_API_URL / VISION_MODEL / VISION_API_KEY pick the endpoint. fetch_image\ngoes through urlguard, so a user-supplied URL cannot reach the LAN."""

import os
import re
import base64
import shutil
import subprocess
import urllib.request

from metald_tools import chat, toollog, urlguard

MAX_IMAGE_BYTES = 20 * 1024 * 1024  # 20MB, generous but bounded
FETCH_TIMEOUT = 20
API_TIMEOUT = 120

DEFAULT_API_URL = os.environ.get("VISION_API_URL", "http://localhost:8080/v1")
DEFAULT_MODEL = os.environ.get("VISION_MODEL", "local")
DEFAULT_API_KEY = os.environ.get("VISION_API_KEY", "")
DEFAULT_QUESTION = "Describe what's in this image in detail."

def fetch_image(source: str) -> tuple[bytes, str]:
    """Return (bytes, mime type) for an http(s) image URL, fetched through urlguard.

    URLs only: accepting local paths would let the model read any file.
    """
    parsed, err = urlguard.safe_url(source)
    if err:
        raise ValueError(err)

    req = urllib.request.Request(source, headers={"User-Agent": "metald-vision-tool"})
    # No redirects: a public url that 302s to a LAN address would defeat the
    # check above.
    with urlguard.opener().open(req, timeout=FETCH_TIMEOUT) as resp:
        data = resp.read(MAX_IMAGE_BYTES + 1)
        if len(data) > MAX_IMAGE_BYTES:
            raise ValueError(f"image exceeds max size of {MAX_IMAGE_BYTES} bytes")
        mime = resp.headers.get_content_type() or "image/jpeg"
        return data, mime

MAX_SIDE = int(os.environ.get("VISION_MAX_SIDE", "1536"))

def fit_for_model(image_bytes: bytes, mime: str) -> tuple:
    """Shrink an image to at most MAX_SIDE pixels on its longest side, as JPEG,
    so it fits the model's vision token budget. Returns the input unchanged if
    ffmpeg is missing or fails."""
    ffmpeg = shutil.which("ffmpeg")
    if not ffmpeg or MAX_SIDE <= 0:
        return image_bytes, mime
    scale = f"scale=w='min(iw,{MAX_SIDE})':h='min(ih,{MAX_SIDE})':force_original_aspect_ratio=decrease"
    try:
        r = subprocess.run([ffmpeg, "-v", "error", "-i", "pipe:0", "-vf", scale, "-frames:v", "1",
                            "-f", "image2", "-c:v", "mjpeg", "-q:v", "3", "pipe:1"],
                           input=image_bytes, capture_output=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired) as e:
        toollog.log_detail("vision", f"resize failed: {e}")
        return image_bytes, mime
    if r.returncode != 0 or not r.stdout:
        toollog.log_detail("vision", f"resize failed: {r.stderr.decode('utf-8', 'replace')[:200]}")
        return image_bytes, mime
    return r.stdout, "image/jpeg"

def describe_image_bytes(image_bytes: bytes, mime: str, question: str) -> str:
    """Ask the vision model about in-memory image bytes. Never raises -
    returns an "Error: ..." string on failure, so callers can relay the
    result as-is."""
    if not image_bytes:
        return "Error: image was empty"
    image_bytes, mime = fit_for_model(image_bytes, mime)
    data_url = f"data:{mime};base64,{base64.b64encode(image_bytes).decode('ascii')}"
    messages = [{"role": "user", "content": [
        {"type": "text", "text": question},
        {"type": "image_url", "image_url": {"url": data_url}},
    ]}]
    try:
        content, _ = chat.complete(DEFAULT_API_URL, DEFAULT_MODEL, messages,
                                   key=DEFAULT_API_KEY, timeout=API_TIMEOUT)
    except chat.ChatError as e:
        toollog.log_detail("vision", f"vision api failed: {e}")
        if "no choices" in str(e):
            return "Error: vision API returned no choices (is a vision-capable model loaded?)"
        if " returned 413" in str(e) or "exceed" in str(e):
            return "Error: that image is too large for the vision model"
        if " returned " in str(e):
            return "Error: the vision model could not process that image"
        return "Error: vision backend is unavailable right now"
    content = content.strip()
    if not content:
        return "Error: model returned no description (is the loaded model vision-capable?)"
    return content

def view_image(source: str, question: str) -> str:
    try:
        image_bytes, mime = fetch_image(source)
    except (urllib.error.URLError, urllib.error.HTTPError) as e:
        return f"Error: failed to fetch image: {e}"
    except ValueError as e:
        return f"Error: {e}"
    except OSError as e:
        return f"Error: failed to read image: {e}"

    return describe_image_bytes(image_bytes, mime, question)

SAFETY_PROMPT = os.environ.get("IMAGE_SAFETY_PROMPT", "")

def check_image_safety(image_bytes: bytes) -> tuple[bool, str]:
    """Runs the generated image through the vision model as a safety check
    before it's uploaded anywhere. Fails closed: if the check itself errors
    out (vision backend down, timeout, etc.), the image is treated as
    unsafe rather than uploaded unchecked.
    """
    if not SAFETY_PROMPT.strip():
        return False, "image safety check is not configured (set IMAGE_SAFETY_PROMPT)"
    verdict = describe_image_bytes(image_bytes, "image/png", SAFETY_PROMPT)
    if verdict.startswith("Error:"):
        return False, f"safety check itself failed: {verdict}"
    match = re.match(r"\s*([A-Za-z]+)", verdict)
    first_word = match.group(1).upper() if match else ""
    return first_word == "SAFE", verdict
