#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
generate_image tool for metald.

Configure under env: in config.yml:
  COMFYUI_URL          base URL of the ComfyUI server (default: http://127.0.0.1:8188)
  IMAGE_GEN_UNET       diffusion model (default: qwen_image_2.1_int8_convrot.safetensors)
  IMAGE_GEN_CLIP       text encoder (default: qwen3vl_8b_int8_convrot.safetensors)
  IMAGE_GEN_VAE        VAE (default: qwen_image_2.1_vae_bf16.safetensors)
  IMAGE_GEN_STEPS      sampler steps (default: 25)
  IMAGE_GEN_CFG        classifier-free guidance (default: 1.0)
  hosting              see metald_tools/hosting.py (UPLOAD_BACKEND, UPLOAD_HEADERS, ...)
"""

import sys
import os
import json
import re
import time
import uuid
import base64
import urllib.request
import urllib.error
import urllib.parse

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog
from metald_tools import comfyui, hosting
from metald_tools.hosting import HostingError, upload_file
from metald_tools.promptrefine import refine_prompt as _refine
from metald_tools import vision
from metald_tools.vision import check_image_safety


UNET_NAME = os.environ.get("IMAGE_GEN_UNET", "qwen_image_2.1_int8_convrot.safetensors")
CLIP_NAME = os.environ.get("IMAGE_GEN_CLIP", "qwen3vl_8b_int8_convrot.safetensors")
VAE_NAME = os.environ.get("IMAGE_GEN_VAE", "qwen_image_2.1_vae_bf16.safetensors")

# 25 steps at cfg 1 with euler/simple, per the ComfyUI template.
GEN_STEPS = int(os.environ.get("IMAGE_GEN_STEPS", "25"))
GEN_CFG = float(os.environ.get("IMAGE_GEN_CFG", "1.0"))

POLL_TIMEOUT = 540  # render is ~35-55s, but with concurrent requests it can queue behind a song or video on the shared GPU
UPLOAD_TIMEOUT = 60  # imgbb can be slow on some images; was 30, too tight

def requires() -> list:
    return ["IMAGE_SAFETY_PROMPT", "IMAGE_PROMPT_REFINER"] + hosting.requires()

def print_schema():
    schema = {
        "title": "picture",
        "description": (
            "generate an image from a text prompt and get back a public url. "
            "your prompt is automatically expanded into a more detailed, "
            "visually concrete prompt before generation - a rough idea is "
            "fine, you don't need to over-engineer it yourself. you must "
            "include that exact url in your reply so the image actually "
            "gets posted - takes 30-60 seconds, so let the user know you're "
            "working on it if it fits the conversation."
        ),
        "type": "object",
        "properties": {
            "prompt": {
                "type": "string",
                "description": "what to generate - a rough idea is fine, it gets expanded automatically",
            },
            "width": {"type": "integer", "description": "image width in pixels (default 1024)"},
            "height": {"type": "integer", "description": "image height in pixels (default 1024)"},
        },
        "required": ["prompt"],
        "additionalProperties": False,
        # Needs outbound network access (ComfyUI, the hosting backend).
        "sandbox": {"allowNetwork": True},
        "requires": requires(),
    }
    print(json.dumps(schema, indent=2))

def build_workflow(prompt: str, width: int, height: int) -> dict:
    return {
        "prompt": {
            # Plain UNETLoader, not UnetLoaderGGUF: these are safetensors.
            "1": {"class_type": "UNETLoader", "inputs": {
                "unet_name": UNET_NAME, "weight_dtype": "default",
            }},
            "2": {"class_type": "CLIPLoader", "inputs": {
                "clip_name": CLIP_NAME, "type": "qwen_image", "device": "default",
            }},
            "3": {"class_type": "VAELoader", "inputs": {"vae_name": VAE_NAME}},
            "4": {"class_type": "TextEncodeQwenImage21", "inputs": {
                "clip": ["2", 0],
                "prompt": prompt,
                "negative_prompt": "",
                "resolution": 1024,
            }},
            "5": {"class_type": "EmptyLatentImage", "inputs": {
                "width": width, "height": height, "batch_size": 1,
            }},
            "6": {"class_type": "KSampler", "inputs": {
                "seed": int(time.time() * 1000) % (2**32),
                "steps": GEN_STEPS, "cfg": GEN_CFG,
                "sampler_name": "euler", "scheduler": "simple",
                "denoise": 1.0,
                "model": ["1", 0],
                "positive": ["4", 0], "negative": ["4", 1],
                "latent_image": ["5", 0],
            }},
            "7": {"class_type": "VAEDecode", "inputs": {"samples": ["6", 0], "vae": ["3", 0]}},
            "8": {"class_type": "SaveImage", "inputs": {
                "filename_prefix": "metald_gen", "images": ["7", 0],
            }},
        }
    }

def upload_image(image_bytes: bytes) -> str:
    return upload_file(image_bytes, f"metald_{int(time.time() * 1000)}.png", "image/png")

def refine_prompt(user_prompt: str) -> str:
    return _refine(user_prompt, vision.DEFAULT_API_URL, vision.DEFAULT_MODEL, vision.DEFAULT_API_KEY)

def generate_image(prompt: str, width: int, height: int) -> str:
    refined_prompt = refine_prompt(prompt)
    try:
        image_bytes = comfyui.run(build_workflow(refined_prompt, width, height), "images",
                                  POLL_TIMEOUT, log=lambda m: toollog.log_detail("image_gen", m))
    except comfyui.ComfyError as e:
        if str(e) == "backend unreachable":
            return "Error: image backend is unavailable right now"
        return "Error: image generation failed"

    is_safe, verdict = check_image_safety(image_bytes)
    if not is_safe:
        return (
            f"Error: generated image did not pass the safety check and was "
            f"not posted ({verdict[:200]}). try a different prompt."
        )

    try:
        url = upload_image(image_bytes)
    except (HostingError, urllib.error.URLError, urllib.error.HTTPError, OSError) as e:
        # HostingError embeds the host's URL and raw response body.
        toollog.log_detail("image_gen", f"upload failed: {e}")
        return "Error: image generated but the upload failed"

    return f"url: {url}"

def main():
    if len(sys.argv) < 2:
        print("Usage: image_gen.py [--schema | --execute <json>]")
        sys.exit(1)

    option = sys.argv[1]

    if option == "--schema":
        print_schema()
        return

    if option == "--execute":
        if len(sys.argv) < 3:
            print("Error: Missing JSON input for execution")
            sys.exit(1)
        try:
            input_data = json.loads(sys.argv[2])
        except json.JSONDecodeError:
            print("Error: Invalid JSON input")
            sys.exit(1)

        prompt = input_data.get("prompt")
        if not prompt:
            print("Error: Missing required 'prompt' in JSON input")
            sys.exit(1)
        try:
            # 1024 is Qwen-Image 2.1's native size.
            width = int(input_data.get("width") or 1024)
            height = int(input_data.get("height") or 1024)
        except (TypeError, ValueError):
            width, height = 768, 768

        print(generate_image(prompt, width, height))
        return

    print("Usage: image_gen.py [--schema | --execute <json>]")
    sys.exit(1)

if __name__ == "__main__":
    main()
