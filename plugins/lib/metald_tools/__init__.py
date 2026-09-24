# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Shared helpers for metald tools: logging, safety review, URL guard,
lyricist, prompt refiner, hosting and media metadata stripping.

Any plugin can use them: `from metald_tools import safetyreview`. The bot
puts plugins/lib on PYTHONPATH for every tool it runs."""
