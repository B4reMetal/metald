# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Call an OpenAI-compatible chat endpoint.

ChatError messages carry internal detail (hosts, status bodies): log them
with toollog, never return them to the channel.
"""

import os
import json
import urllib.request
import urllib.error

class ChatError(RuntimeError):
    pass

def endpoint(prefix: str, default_url: str = "http://127.0.0.1:4000/v1",
             default_model: str = "chat") -> dict:
    """Read PREFIX_URL, PREFIX_MODEL and PREFIX_KEY, e.g. endpoint("VISION_API")."""
    return {
        "url": os.environ.get(prefix + "_URL", default_url),
        "model": os.environ.get(prefix + "_MODEL", default_model),
        "key": os.environ.get(prefix + "_KEY", ""),
    }

def request(url: str, payload: dict, key: str = "", timeout: float = 120) -> dict:
    """POST payload to url/chat/completions and return the decoded JSON."""
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Authorization"] = "Bearer " + key
    req = urllib.request.Request(url.rstrip("/") + "/chat/completions",
                                 data=json.dumps(payload).encode("utf-8"),
                                 headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8", errors="replace"))
    except urllib.error.HTTPError as e:
        raise ChatError(f"{url} returned {e.code}: {e.read().decode('utf-8', 'replace')[:300]}")
    except (urllib.error.URLError, OSError) as e:
        raise ChatError(f"{url} unreachable: {e}")
    except ValueError as e:
        raise ChatError(f"{url} returned invalid JSON: {e}")

def complete(url: str, model: str, messages: list, key: str = "", timeout: float = 120,
             **params) -> tuple:
    """Return (content, finish_reason). Extra params (max_tokens,
    temperature, reasoning_effort, ...) go into the request as-is."""
    body = request(url, {"model": model, "messages": messages, "stream": False, **params},
                   key=key, timeout=timeout)
    choices = body.get("choices") or []
    if not choices:
        raise ChatError("response had no choices")
    choice = choices[0]
    return (choice.get("message") or {}).get("content") or "", choice.get("finish_reason") or ""
