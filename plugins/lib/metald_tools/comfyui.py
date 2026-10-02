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
import sys
import json
import time
import urllib.request
import urllib.error
import urllib.parse

from metald_tools import toollog

POLL_INTERVAL = 2

# What a tool returns when an operator cancelled its GPU job, so the model doesn't take it as a failure
# to retry.
CANCELLED_REPLY = "Cancelled: an operator stopped this job on the GPU. Tell the user it was cancelled; don't retry it."


# What a tool returns when the GPU server is down, so the model says so instead of retrying or handing the
# job to a background task that would fail the same way.
OFFLINE_REPLY = ("Unavailable: the GPU server is offline right now, so songs, pictures, video and speech "
                 "can't be made. Say so plainly; don't retry it, and don't start a background task for it.")


def online() -> bool:
    """Whether ComfyUI answers; tools check before doing any other work for a job."""
    try:
        with urllib.request.urlopen(base_url() + "/system_stats", timeout=3):
            return True
    except (urllib.error.URLError, OSError, ValueError):
        return False


class ComfyError(RuntimeError):
    pass

def base_url() -> str:
    return os.environ.get("COMFYUI_URL", "http://127.0.0.1:8188").rstrip("/")

def _log(log):
    return log or (lambda m: toollog.log_detail("comfyui", m))

def submit(workflow: dict, log=None) -> str:
    """Queue a workflow ({"prompt": {...}}) and return its prompt id."""
    log = _log(log)
    # Name the tool that sent it, so the operator page can say what a queued job is for.
    tool = os.path.splitext(os.path.basename(sys.argv[0]))[0]
    workflow = {**workflow, "extra_data": {**workflow.get("extra_data", {}), "metald": {"tool": tool}}}
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
    if output.startswith("ui:"):  # a node's own UI output (text), not a file
        return key == output[3:]
    if not isinstance(item, dict) or not item.get("filename"):
        return False
    if output.startswith("."):
        return str(item["filename"]).endswith(output)
    return key == output

class ComfyCancelled(ComfyError):
    """The job was stopped while running, or removed while waiting (the operator page does both)."""


def _queued(prompt_id: str) -> bool:
    """Whether the job is still waiting or running; on doubt, assume it is."""
    try:
        with urllib.request.urlopen(base_url() + "/queue", timeout=30) as resp:
            q = json.loads(resp.read())
    except (urllib.error.URLError, OSError, ValueError):
        return True
    return any(len(job) > 1 and job[1] == prompt_id for job in q.get("queue_running", []) + q.get("queue_pending", []))


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
                if "execution_interrupted" in json.dumps(status.get("messages")):
                    raise ComfyCancelled("cancelled")
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
        elif not _queued(prompt_id):
            # Not finished and not queued: removed before it ran. Check history once more, in case it
            # finished between the two calls.
            try:
                with urllib.request.urlopen(base_url() + f"/history/{prompt_id}", timeout=30) as resp:
                    finished = json.loads(resp.read()).get(prompt_id)
            except (urllib.error.URLError, OSError, ValueError) as e:
                log(f"history poll failed: {e}")
                raise ComfyError("backend unreachable")
            if not finished:
                log("job removed from the queue")
                raise ComfyCancelled("cancelled")
            continue
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

def upload(data: bytes, name: str, log=None) -> str:
    """Put a file in ComfyUI's input folder (for a node that reads one); returns the name to use."""
    log = _log(log)
    boundary = "metald" + os.urandom(8).hex()
    body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"image\"; filename=\"{name}\"\r\n"
            "Content-Type: application/octet-stream\r\n\r\n").encode() + data + (
            f"\r\n--{boundary}\r\nContent-Disposition: form-data; name=\"overwrite\"\r\n\r\ntrue"
            f"\r\n--{boundary}--\r\n").encode()
    req = urllib.request.Request(base_url() + "/upload/image", data=body,
                                 headers={"Content-Type": f"multipart/form-data; boundary={boundary}"})
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return json.loads(resp.read())["name"]
    except (urllib.error.URLError, OSError, ValueError, KeyError) as e:
        log(f"upload to comfyui failed: {e}")
        raise ComfyError("backend unreachable")

def result(workflow: dict, key: str, timeout: float = 540, log=None):
    """submit + wait, for a node that answers in its UI output (text) rather than with a file."""
    return wait(submit(workflow, log), "ui:" + key, timeout, log)

def run(workflow: dict, output: str = "images", timeout: float = 540, log=None) -> bytes:
    """submit + wait + fetch."""
    return fetch(wait(submit(workflow, log), output, timeout, log), log)
