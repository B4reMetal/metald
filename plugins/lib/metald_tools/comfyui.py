# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Run a ComfyUI API-format workflow and get its output file.

    from metald_tools import comfyui
    data = comfyui.run(workflow, output=".mp4", timeout=540, log=my_logger)

Failures raise ComfyError with a message safe to show in a channel
("backend unreachable", "generation timed out", ...); the detail goes to
the operator log. COMFYUI_URL picks the server (default 127.0.0.1:8188).
"""

import os
import json
import time
import urllib.request
import urllib.error
import urllib.parse

from metald_tools import toollog

POLL_INTERVAL = 2

class ComfyError(RuntimeError):
    pass

def base_url() -> str:
    return os.environ.get("COMFYUI_URL", "http://127.0.0.1:8188").rstrip("/")

def _log(log):
    return log or (lambda m: toollog.log_detail("comfyui", m))

def submit(workflow: dict, log=None) -> str:
    """Queue a workflow ({"prompt": {...}}) and return its prompt id."""
    log = _log(log)
    req = urllib.request.Request(base_url() + "/prompt", data=json.dumps(workflow).encode("utf-8"),
                                 headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read())
    except urllib.error.HTTPError as e:
        log(f"comfyui rejected workflow: {e.read().decode('utf-8', 'replace')[:600]}")
        raise ComfyError("workflow rejected")
    except (urllib.error.URLError, OSError, ValueError) as e:
        log(f"comfyui unreachable at {base_url()}: {e}")
        raise ComfyError("backend unreachable")
    if result.get("node_errors"):
        log(f"node errors: {json.dumps(result['node_errors'])[:600]}")
        raise ComfyError("workflow rejected")
    return result["prompt_id"]

def _matches(output: str, key: str, item) -> bool:
    if not isinstance(item, dict) or not item.get("filename"):
        return False
    if output.startswith("."):
        return str(item["filename"]).endswith(output)
    return key == output

def wait(prompt_id: str, output: str = "images", timeout: float = 540, log=None) -> dict:
    """Poll until the job finishes and return its first output item
    ({"filename", "subfolder", "type"}). output is a node output key
    ("images", "audio") or a filename suffix (".mp4")."""
    log = _log(log)
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(base_url() + f"/history/{prompt_id}", timeout=30) as resp:
                entry = json.loads(resp.read()).get(prompt_id)
        except (urllib.error.URLError, OSError, ValueError) as e:
            log(f"history poll failed: {e}")
            raise ComfyError("backend unreachable")
        if entry:
            status = entry.get("status", {})
            if status.get("status_str") == "error":
                log(f"generation failed: {json.dumps(status.get('messages'))[:600]}")
                raise ComfyError("generation failed")
            found = None
            for node in entry.get("outputs", {}).values():
                for key, items in node.items():
                    for item in items if isinstance(items, list) else []:
                        if found is None and _matches(output, key, item):
                            found = item
            if found is not None:
                return found
            if status.get("completed"):
                raise ComfyError(f"generation produced no {output.lstrip('.')}")
        time.sleep(POLL_INTERVAL)
    raise ComfyError("generation timed out")

def fetch(item: dict, log=None) -> bytes:
    """Download an output item returned by wait()."""
    params = urllib.parse.urlencode({"filename": item["filename"], "subfolder": item.get("subfolder", ""),
                                     "type": item.get("type", "output")})
    try:
        with urllib.request.urlopen(base_url() + "/view?" + params, timeout=120) as resp:
            return resp.read()
    except (urllib.error.URLError, OSError) as e:
        _log(log)(f"could not fetch {item['filename']}: {e}")
        raise ComfyError("could not read the generated output")

def run(workflow: dict, output: str = "images", timeout: float = 540, log=None) -> bytes:
    """submit + wait + fetch."""
    return fetch(wait(submit(workflow, log), output, timeout, log), log)
