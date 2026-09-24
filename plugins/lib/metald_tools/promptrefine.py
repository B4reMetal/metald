# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Expand a rough image request into a detailed prompt before rendering."""

import os

from metald_tools import chat


def refine_prompt(user_prompt: str, api_url: str, model: str, api_key: str = "") -> str:
    """Expand a rough image request into a detailed prompt.

    Best-effort: returns the original prompt on any failure. IMAGE_PROMPT_REFINER
    is the system prompt.
    """
    system = os.environ.get("IMAGE_PROMPT_REFINER", "").strip()
    if not system:
        return user_prompt
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

