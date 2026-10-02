# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""ComfyUI nodes that time song lyrics for read-along: Demucs vocal separation, MMS forced alignment, and
Whisper to write lyrics nobody wrote down. Their models are handed to ComfyUI's model management, so they
load for a lyrics job and give way to every other model on the GPU."""

from .nodes import NODE_CLASS_MAPPINGS, NODE_DISPLAY_NAME_MAPPINGS

__all__ = ["NODE_CLASS_MAPPINGS", "NODE_DISPLAY_NAME_MAPPINGS"]
