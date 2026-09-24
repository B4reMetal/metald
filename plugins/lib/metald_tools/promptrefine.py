# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Expand a rough image request into a detailed prompt before rendering."""

import os

from metald_tools import chat

DEFAULT_SYSTEM = (
    "You are a prompt engineer for a text-to-image diffusion model. "
    "Rewrite the user's request into a single, detailed, visually concrete "
    "image generation prompt: describe subject, composition, lighting, and "
    "style explicitly. If the request is vague, abstract, or "
    "self-referential (e.g. \"a picture of yourself\"), invent concrete, "
    "consistent visual specifics rather than leaving it undefined - a "
    "diffusion model has nothing to draw from an undefined subject. Reply "
    "with ONLY the rewritten prompt: no preamble, no quotes, no explanation."
)

def refine_prompt(user_prompt: str, api_url: str, model: str, api_key: str = "") -> str:
    """Expand a rough image request into a detailed prompt.

    Best-effort: returns the original prompt on any failure. IMAGE_PROMPT_REFINER
    overrides the system prompt.
    """
    system = os.environ.get("IMAGE_PROMPT_REFINER", "").strip() or DEFAULT_SYSTEM
    try:
        content, _ = chat.complete(api_url, model, [
            {"role": "system", "content": system},
            {"role": "user", "content": user_prompt},
        ], key=api_key, timeout=60, max_tokens=2000, reasoning_effort="low")
    except chat.ChatError:
        return user_prompt
    content = content.strip()
    content = content.strip('"').strip()  # some models wrap the reply in quotes anyway
    return content or user_prompt

