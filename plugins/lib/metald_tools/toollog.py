# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
Operator-only logging for metald tools.
"""

import os
import tempfile
import time

LOG_PATH = os.environ.get("METALD_TOOL_LOG") or os.path.join(tempfile.gettempdir(), "metald-tool-errors.log")

def log_detail(tool: str, message: str) -> None:
    """Record the real reason a tool failed, for the operator only.

    Never raises: a logging failure must not turn into a tool failure.
    """
    try:
        stamp = time.strftime("%Y-%m-%d %H:%M:%S")
        with open(LOG_PATH, "a", encoding="utf-8") as fh:
            fh.write(f"{stamp} [{tool}] {message}\n")
    except OSError:
        pass
