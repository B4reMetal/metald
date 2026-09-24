# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Upload generated files to a host and return a public URL.

Every media tool calls upload_file(). UPLOAD_BACKEND picks zipline (default),
imgbb (images only) or http (any host, configured from the environment), and
UPLOAD_HEADERS adds headers to every upload. examples/env.example lists every
setting. A custom plugin can add a backend with register_backend().
"""

import os
import json
import time
import uuid
import base64
import urllib.request
import urllib.error
import urllib.parse

UPLOAD_TIMEOUT = 90
UPLOAD_RETRIES = int(os.environ.get("UPLOAD_RETRIES", "3"))
UPLOAD_RETRY_DELAY = 3

class HostingError(Exception):
    pass

def extra_headers() -> dict:
    raw = os.environ.get("UPLOAD_HEADERS", "").strip()
    if not raw:
        return {}
    if raw.startswith("{"):
        try:
            pairs = json.loads(raw).items()
        except (ValueError, AttributeError):
            raise HostingError("UPLOAD_HEADERS is not valid JSON")
    else:
        pairs = []
        for part in raw.replace(";", "\n").splitlines():
            name, sep, value = part.partition(":")
            if sep and name.strip():
                pairs.append((name, value))
    return {str(k).strip(): os.path.expandvars(str(v).strip()) for k, v in pairs}

def multipart(fields: dict, file_field: str, filename: str, content_type: str, data: bytes) -> tuple:
    boundary = uuid.uuid4().hex
    out = b""
    for k, v in fields.items():
        out += (f"--{boundary}\r\n"
                f'Content-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n').encode("utf-8")
    out += (f"--{boundary}\r\n"
            f'Content-Disposition: form-data; name="{file_field}"; filename="{filename}"\r\n'
            f"Content-Type: {content_type}\r\n\r\n").encode("utf-8")
    out += data + f"\r\n--{boundary}--\r\n".encode("utf-8")
    return out, f"multipart/form-data; boundary={boundary}"

def send(url: str, body: bytes, headers: dict, method: str = "POST", label: str = "upload") -> bytes:
    headers = {**headers, **extra_headers()}
    req = urllib.request.Request(url, data=body, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=UPLOAD_TIMEOUT) as resp:
            return resp.read()
    except urllib.error.HTTPError as e:
        raise HostingError(f"{label} returned {e.code}: {e.read().decode('utf-8', 'replace')[:300]}")
    except (urllib.error.URLError, OSError) as e:
        raise HostingError(f"{label} request failed: {e}")

def find_url(body: bytes, where: str = "") -> str:
    text = body.decode("utf-8", "replace").strip()
    if where == "text":
        url = text
    else:
        try:
            doc = json.loads(text)
        except ValueError:
            doc = None
        paths = [where] if where else ["url", "link", "data.url", "files.0.url"]
        url = ""
        for path in paths:
            cur = doc
            for key in path.split("."):
                if isinstance(cur, list) and key.isdigit() and int(key) < len(cur):
                    cur = cur[int(key)]
                elif isinstance(cur, dict):
                    cur = cur.get(key)
                else:
                    cur = None
                    break
            if isinstance(cur, str) and cur:
                url = cur
                break
        if not url and not where and text.startswith(("http://", "https://")):
            url = text
    if not url.startswith(("http://", "https://")):
        raise HostingError(f"upload response has no url: {text[:200]}")
    return url

def upload_zipline(data: bytes, filename: str, content_type: str, deletes_at: str) -> str:
    base, token = os.environ.get("ZIPLINE_URL", ""), os.environ.get("ZIPLINE_TOKEN", "")
    if not base or not token:
        raise HostingError("ZIPLINE_URL and ZIPLINE_TOKEN must be set")
    body, ctype = multipart({}, "file", filename, content_type, data)
    headers = {"Authorization": token, "Content-Type": ctype}
    expiry = deletes_at or os.environ.get("ZIPLINE_DELETES_AT", "")
    if expiry:
        headers["x-zipline-deletes-at"] = expiry
    return find_url(send(base.rstrip("/") + "/api/upload", body, headers, label="zipline"), "files.0.url")

def upload_imgbb(data: bytes, filename: str, content_type: str, deletes_at: str) -> str:
    if not content_type.startswith("image/"):
        raise HostingError("the imgbb backend only hosts images")
    key = os.environ.get("IMGBB_API_KEY", "")
    if not key:
        raise HostingError("IMGBB_API_KEY is not set")
    form = urllib.parse.urlencode({"key": key, "image": base64.b64encode(data).decode("ascii"),
                                   "expiration": os.environ.get("IMGBB_EXPIRATION", "86400")}).encode()
    body = send("https://api.imgbb.com/1/upload", form,
                {"Content-Type": "application/x-www-form-urlencoded"}, label="imgbb")
    return find_url(body, "data.url")

def upload_http(data: bytes, filename: str, content_type: str, deletes_at: str) -> str:
    url = os.environ.get("UPLOAD_URL", "")
    if not url:
        raise HostingError("UPLOAD_URL is not set")
    url = url.replace("{filename}", urllib.parse.quote(filename))
    method = os.environ.get("UPLOAD_METHOD", "POST").upper()
    headers = {}
    if deletes_at and os.environ.get("UPLOAD_EXPIRY_HEADER"):
        headers[os.environ["UPLOAD_EXPIRY_HEADER"]] = deletes_at
    if os.environ.get("UPLOAD_BODY", "multipart").lower() == "raw":
        body = data
        headers["Content-Type"] = content_type
    else:
        fields = dict(urllib.parse.parse_qsl(os.environ.get("UPLOAD_FORM", "")))
        if deletes_at and os.environ.get("UPLOAD_EXPIRY_FIELD"):
            fields[os.environ["UPLOAD_EXPIRY_FIELD"]] = deletes_at
        body, headers["Content-Type"] = multipart(fields, os.environ.get("UPLOAD_FIELD", "file"),
                                                  filename, content_type, data)
    return find_url(send(url, body, headers, method=method, label="upload host"),
                    os.environ.get("UPLOAD_RESPONSE", ""))

BACKENDS = {"zipline": upload_zipline, "imgbb": upload_imgbb, "http": upload_http}
REQUIRES = {"zipline": ["ZIPLINE_URL", "ZIPLINE_TOKEN"], "imgbb": ["IMGBB_API_KEY"], "http": ["UPLOAD_URL"]}

def register_backend(name: str, fn, requires=()):
    """fn(data, filename, content_type, deletes_at) -> url. Call send() and
    find_url() from it to get UPLOAD_HEADERS and error handling for free."""
    BACKENDS[name] = fn
    REQUIRES[name] = list(requires)

def backend_name() -> str:
    return (os.environ.get("UPLOAD_BACKEND") or os.environ.get("IMAGE_HOST_BACKEND") or "zipline").strip().lower()

def requires() -> list:
    """Env vars the active backend needs, for a tool's schema "requires"."""
    return list(REQUIRES.get(backend_name(), []))

def upload_file(data: bytes, filename: str, content_type: str, deletes_at: str = "") -> str:
    """Upload through the configured backend, retrying transient failures.
    deletes_at is the tool's own expiry ("12h"); how it is sent depends on
    the backend."""
    name = backend_name()
    fn = BACKENDS.get(name)
    if fn is None:
        raise HostingError(f"unknown UPLOAD_BACKEND {name!r}, available: {sorted(BACKENDS)}")
    last = None
    for attempt in range(1, max(1, UPLOAD_RETRIES) + 1):
        try:
            return fn(data, filename, content_type, deletes_at)
        except HostingError as e:
            last = e
            if "must be set" in str(e) or "is not set" in str(e) or "only hosts" in str(e):
                break
            if attempt < UPLOAD_RETRIES:
                time.sleep(UPLOAD_RETRY_DELAY)
    raise HostingError(f"upload failed: {last}")
