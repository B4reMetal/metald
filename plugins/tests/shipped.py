# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Shipped default text for a tool setting, from the env: section of examples/chatbot.yml."""
import os
import yaml

_EXAMPLE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "examples", "chatbot.yml")

def shipped(name):
    with open(_EXAMPLE) as fh:
        return yaml.safe_load(fh)["env"][name]
