#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""
generate_video tool for metald: LTX-2.5 via ComfyUI, video with its own audio.

Configure under env: in config.yml:
  COMFYUI_URL          ComfyUI server (default: http://127.0.0.1:8188)
  VIDEO_SAFETY_POLICY  review policy for the prompt (required)
  VIDEO_SAFETY_REVIEW  "off" disables the review
  VIDEO_MAX_SECONDS    longest clip (default: 8)
  VIDEO_DELETES_AT     Zipline retention (default: 1h)
  hosting              see metald_tools/hosting.py (UPLOAD_BACKEND, UPLOAD_HEADERS, ...)
Needs ffmpeg on PATH to strip the workflow ComfyUI embeds in the mp4.
"""

import sys
import os
import json
import time
import shutil
import tempfile
import subprocess
import urllib.request
import urllib.error
import urllib.parse

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog
from metald_tools import safetyreview
from metald_tools import comfyui, hosting
from metald_tools.hosting import upload_file, HostingError
from metald_tools.media import strip_mp4_metadata as strip_metadata

MAX_SECONDS = int(os.environ.get("VIDEO_MAX_SECONDS", "8"))
DELETES_AT = os.environ.get("VIDEO_DELETES_AT", "1h")
SAFETY_POLICY = os.environ.get("VIDEO_SAFETY_POLICY", "")
FPS = 24
POLL_TIMEOUT = 540

UNET = "ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors"
TEXT_ENCODER = "gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors"
VIDEO_VAE = "ltx-2.5-video-vae-bf16.safetensors"
AUDIO_VAE = "ltx-2.5-audio-vae-bf16.safetensors"
UPSCALER = "ltx-2.5-latent-spatial-upscaler-x2-bf16-1.0.safetensors"
NEGATIVE = os.environ.get("VIDEO_NEGATIVE_PROMPT", "")

SIZES = {"landscape": (1280, 720), "portrait": (720, 1280), "square": (960, 960)}

def requires() -> list:
    return ["VIDEO_SAFETY_POLICY", "VIDEO_NEGATIVE_PROMPT"] + hosting.requires() + safetyreview.requires("VIDEO_SAFETY_REVIEW")

def print_schema():
    schema = {
        "title": "video",
        "description": (
            "generate a short video clip WITH its own sound (ambience, effects, "
            "speech, music) and get back a public url. write the prompt like a "
            "shot description: subject, action, camera movement, lighting, "
            "setting, and what is heard - dialogue in quotes is spoken aloud. "
            "a detailed paragraph works far better than a few words. takes a "
            "minute or two. include the returned url in your reply."
        ),
        "type": "object",
        "properties": {
            "prompt": {"type": "string", "description": "detailed description of the shot and its sound"},
            "seconds": {"type": "integer", "description": f"clip length, 2-{MAX_SECONDS} (default 5)"},
            "aspect": {"type": "string", "enum": sorted(SIZES), "description": "default landscape"},
        },
        "required": ["prompt"],
        "additionalProperties": False,
        "sandbox": {"allowNetwork": True},
        "requires": requires(),
    }
    print(json.dumps(schema, indent=2))

def build_workflow(prompt: str, seconds: int, aspect: str, seed: int) -> dict:
    w, h = SIZES.get(aspect, SIZES["landscape"])
    frames = seconds * FPS + 1
    return {"prompt": {
        "unet": {"class_type": "UNETLoader", "inputs": {"unet_name": UNET, "weight_dtype": "default"}},
        "clip": {"class_type": "CLIPLoader", "inputs": {"clip_name": TEXT_ENCODER, "type": "ltxv", "device": "default"}},
        "vvae": {"class_type": "VAELoader", "inputs": {"vae_name": VIDEO_VAE}},
        "avae": {"class_type": "VAELoader", "inputs": {"vae_name": AUDIO_VAE}},
        "up": {"class_type": "LatentUpscaleModelLoader", "inputs": {"model_name": UPSCALER}},
        "pos": {"class_type": "CLIPTextEncode", "inputs": {"text": prompt, "clip": ["clip", 0]}},
        "neg": {"class_type": "CLIPTextEncode", "inputs": {"text": NEGATIVE, "clip": ["clip", 0]}},
        "cond": {"class_type": "LTXVConditioning", "inputs": {"positive": ["pos", 0], "negative": ["neg", 0], "frame_rate": FPS}},
        # stage 1: half resolution, 8 steps
        "vlat": {"class_type": "EmptyLTXVLatentVideo", "inputs": {"width": w // 2, "height": h // 2, "length": frames, "batch_size": 1}},
        "alat": {"class_type": "LTXVEmptyLatentAudio", "inputs": {"frames_number": frames, "frame_rate": FPS, "batch_size": 1, "audio_vae": ["avae", 0]}},
        "av1": {"class_type": "LTXVConcatAVLatent", "inputs": {"video_latent": ["vlat", 0], "audio_latent": ["alat", 0]}},
        "g1": {"class_type": "LTXVDualCFGGuider", "inputs": {"model": ["unet", 0], "positive": ["cond", 0], "negative": ["cond", 1], "video_cfg": 1, "audio_cfg": 1}},
        "n1": {"class_type": "RandomNoise", "inputs": {"noise_seed": seed}},
        "k1": {"class_type": "KSamplerSelect", "inputs": {"sampler_name": "euler_ancestral"}},
        "s1": {"class_type": "ManualSigmas", "inputs": {"sigmas": "1.0, 0.99375, 0.9875, 0.98125, 0.975, 0.909375, 0.725, 0.421875, 0.0"}},
        "run1": {"class_type": "SamplerCustomAdvanced", "inputs": {"noise": ["n1", 0], "guider": ["g1", 0], "sampler": ["k1", 0], "sigmas": ["s1", 0], "latent_image": ["av1", 0]}},
        # stage 2: upscale the video latent 2x, refine 3 steps
        "sep1": {"class_type": "LTXVSeparateAVLatent", "inputs": {"av_latent": ["run1", 0]}},
        "ups": {"class_type": "LTXVLatentUpsampler", "inputs": {"samples": ["sep1", 0], "upscale_model": ["up", 0], "vae": ["vvae", 0]}},
        "av2": {"class_type": "LTXVConcatAVLatent", "inputs": {"video_latent": ["ups", 0], "audio_latent": ["sep1", 1]}},
        "g2": {"class_type": "LTXVDualCFGGuider", "inputs": {"model": ["unet", 0], "positive": ["cond", 0], "negative": ["cond", 1], "video_cfg": 1, "audio_cfg": 1}},
        "n2": {"class_type": "RandomNoise", "inputs": {"noise_seed": seed + 1}},
        "k2": {"class_type": "KSamplerSelect", "inputs": {"sampler_name": "euler_ancestral"}},
        "s2": {"class_type": "ManualSigmas", "inputs": {"sigmas": "0.85, 0.7250, 0.4219, 0.0"}},
        "run2": {"class_type": "SamplerCustomAdvanced", "inputs": {"noise": ["n2", 0], "guider": ["g2", 0], "sampler": ["k2", 0], "sigmas": ["s2", 0], "latent_image": ["av2", 0]}},
        "sep2": {"class_type": "LTXVSeparateAVLatent", "inputs": {"av_latent": ["run2", 0]}},
        "img": {"class_type": "VAEDecodeTiled", "inputs": {"samples": ["sep2", 0], "vae": ["vvae", 0], "tile_size": 512, "overlap": 64, "temporal_size": 64, "temporal_overlap": 16}},
        "aud": {"class_type": "LTXVAudioVAEDecode", "inputs": {"samples": ["sep2", 1], "audio_vae": ["avae", 0]}},
        "vid": {"class_type": "CreateVideo", "inputs": {"images": ["img", 0], "audio": ["aud", 0], "fps": FPS}},
        "save": {"class_type": "SaveVideo", "inputs": {"video": ["vid", 0], "filename_prefix": "video/metald", "format": "mp4"}},
    }}

def generate(prompt: str, seconds: int, aspect: str) -> str:
    prompt = prompt.strip()
    if not prompt:
        return "Error: no prompt given"
    if safetyreview.enabled("VIDEO_SAFETY_REVIEW"):
        allowed, reason = safetyreview.review(SAFETY_POLICY, prompt, label="VIDEO REQUEST",
                                              logger=lambda m: toollog.log_detail("video_gen", m))
        if not allowed:
            toollog.log_detail("video_gen", f"refused ({reason}): {prompt[:300]}")
            return f"Refused: {reason}. Say so in your own voice; do not retry the same thing."
    seed = int(time.time() * 1000) % (2**31)
    t0 = time.time()
    try:
        video = strip_metadata(comfyui.run(build_workflow(prompt, seconds, aspect, seed), ".mp4",
                                           POLL_TIMEOUT, log=lambda m: toollog.log_detail("video_gen", m)))
    except RuntimeError as e:
        toollog.log_detail("video_gen", f"failed: {e}")
        return "Error: the video backend is unavailable right now"
    try:
        url = upload_file(video, f"metald_{int(time.time() * 1000)}.mp4", "video/mp4", deletes_at=DELETES_AT)
    except HostingError as e:
        toollog.log_detail("video_gen", f"upload failed: {e}")
        return "Error: clip generated but the upload failed"
    toollog.log_detail("video_gen", f"ok {url} ({len(video)} bytes, {seconds}s {aspect}, {time.time() - t0:.0f}s render)")
    return f"url: {url}"

def main():
    if len(sys.argv) < 2:
        print("usage: videogen.py --schema | --execute <json>", file=sys.stderr)
        sys.exit(1)
    if sys.argv[1] == "--schema":
        print_schema()
        return
    if sys.argv[1] == "--execute":
        if not SAFETY_POLICY.strip():
            print("Error: video safety policy is not configured (set VIDEO_SAFETY_POLICY)")
            return
        try:
            data = json.loads(sys.argv[2]) if len(sys.argv) > 2 else {}
        except json.JSONDecodeError:
            print("Error: could not read the request")
            return
        try:
            seconds = int(data.get("seconds") or 5)
        except (TypeError, ValueError):
            seconds = 5
        seconds = max(2, min(seconds, MAX_SECONDS))
        print(generate(data.get("prompt") or "", seconds, (data.get("aspect") or "landscape").strip()))
        return
    print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
    sys.exit(1)

if __name__ == "__main__":
    main()
