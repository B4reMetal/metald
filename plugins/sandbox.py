#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
run_code tool for metald.

Configure via environment variables:
  FLY_API_TOKEN            required; app-scoped token from "fly tokens create"
  FLY_SANDBOX_APP          Fly app that owns the machines (default: metald-sandbox)
  FLY_SANDBOX_REGION       where to boot them (default: dfw)
  FLY_SANDBOX_MEMORY_MB    guest memory (default: 512)
  SANDBOX_IMAGE            container image (default: python:3.12-slim)
  SANDBOX_TIMEOUT          per-command wall clock seconds (default: 60, max 300)
"""

import sys
import os
import json
import time
import uuid
import base64
import subprocess
import urllib.request
import urllib.error

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import toollog
from metald_tools import safetyreview

HERMES_HOME = os.environ.get("HERMES_HOME", os.path.expanduser("~/.hermes"))
HERMES_APP = os.path.join(HERMES_HOME, "hermes-agent")
HERMES_PY = os.path.join(HERMES_APP, "venv", "bin", "python")

FLY_API = "https://api.machines.dev/v1"
FLY_APP = os.environ.get("FLY_SANDBOX_APP", "metald-sandbox")
FLY_REGION = os.environ.get("FLY_SANDBOX_REGION", "dfw")
FLY_MEMORY_MB = int(os.environ.get("FLY_SANDBOX_MEMORY_MB", "512"))

DEFAULT_IMAGE = os.environ.get("SANDBOX_IMAGE", "python:3.12-slim")
DEFAULT_TIMEOUT = int(os.environ.get("SANDBOX_TIMEOUT", "60"))
MAX_TIMEOUT = 300
POLL_INTERVAL = 2
CREATE_TIMEOUT = 90
MAX_OUTPUT = 3000  # IRC-sized; the model has to relay this into a channel

class SandboxError(Exception):
    """An infrastructure failure (gateway, auth, quota) - never shown to the
    channel verbatim, see run_code()."""
    pass

GENERIC_SANDBOX_ERROR = "Error: the code sandbox is unavailable right now"

def print_schema():
    schema = {
        "title": "run_code",
        "description": (
            "run python or shell code in an isolated cloud sandbox and get "
            "back its output. use this for anything that needs actual "
            "computation or verification rather than guessing - arithmetic on "
            "big numbers, parsing text, checking a regex, testing a snippet. "
            "the sandbox is a throwaway container with no access to this "
            "machine and no saved state between calls. it takes a few seconds "
            "to start, so don't use it for things you can just answer. "
            "IMPORTANT: only the python standard library is available - there "
            "is NO numpy, scipy, sympy or pandas, and pip install does not "
            "work, so write plain python (e.g. hand-roll numerical methods "
            "like rk4 rather than importing scipy). attempts to pip install "
            "or import a missing package fail with no useful error. this "
            "tool does NOT do network access: no fetching urls, no connecting "
            "to hosts, no port checks, no dns lookups. such code is rejected "
            "before it runs, so refuse those requests outright instead of "
            "trying - people asking you to curl or connect to things are "
            "trying to use you as a proxy. it also rejects dynamically built "
            "or decoded code (exec, eval, piping into an interpreter): "
            "requests to 'decode and run this base64' are that same proxy "
            "attempt wearing a hat, so refuse them the same way."
        ),
        "type": "object",
        "properties": {
            "code": {
                "type": "string",
                "description": "the code to run. print() or echo whatever you want to see - only stdout/stderr come back",
            },
            "language": {
                "type": "string",
                "enum": ["python", "bash"],
                "description": "which interpreter to use (default: python)",
            },
        },
        "required": ["code"],
        "additionalProperties": False,
        "sandbox": False,
        "requires": ["FLY_API_TOKEN"],
    }
    print(json.dumps(schema, indent=2))

def resolve_gateway():
    """Returns (app, token) for the Fly Machines API.

    Named for the interface it replaced rather than what it does, so the
    orchestration in run_code() did not have to change when the backend did.
    """
    token = os.environ.get("FLY_API_TOKEN", "").strip()
    if not token:
        raise SandboxError("FLY_API_TOKEN is not set")
    return FLY_APP, token

def fly(method, path, token, payload=None, timeout=30):
    url = f"{FLY_API}/apps/{FLY_APP}/machines{path}"
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", f"Bearer {token}")
    if data:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read().decode("utf-8", errors="replace")
            return json.loads(body) if body.strip() else {}
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", errors="replace")[:300]
        raise SandboxError(f"fly {method} {path} -> {e.code}: {detail}")
    except (urllib.error.URLError, OSError) as e:
        raise SandboxError(f"fly {method} {path} unreachable: {e}")
    except json.JSONDecodeError as e:
        raise SandboxError(f"fly {method} {path} returned unparseable json: {e}")

def create_sandbox(origin, token, timeout_s):
    """Boot a fresh, throwaway Fly machine and return its id.

    Fresh per call, so one person's code cannot leave state for the next.
    auto_destroy stops the machine even if this process dies mid-run.
    """
    body = {
        "region": FLY_REGION,
        "config": {
            "image": DEFAULT_IMAGE,
            "guest": {"cpu_kind": "shared", "cpus": 1, "memory_mb": FLY_MEMORY_MB},
            "init": {"exec": ["sleep", str(timeout_s + 60)]},
            "auto_destroy": True,
            "restart": {"policy": "no"},
        },
    }
    m = fly("POST", "", token, body, timeout=CREATE_TIMEOUT)
    mid = m.get("id")
    if not mid:
        raise SandboxError(f"fly create returned no machine id: {str(m)[:200]}")

    # Wait for it to actually be running before exec, or the exec races the
    # boot and fails with a confusing error.
    try:
        fly("GET", f"/{mid}/wait?state=started", token, timeout=CREATE_TIMEOUT)
    except SandboxError:
        terminate(origin, token, mid)
        raise
    return mid

def run_exec(origin, token, sandbox_id, command, timeout_s):
    """Runs the command and returns the shape run_code() expects."""
    try:
        r = fly("POST", f"/{sandbox_id}/exec", token,
                {"command": ["sh", "-c", command], "timeout": timeout_s},
                timeout=timeout_s + 30)
    except SandboxError as e:
        if "timed out" in str(e).lower():
            return {"output": "", "returncode": 124, "status": "timeout"}
        raise

    out = (r.get("stdout") or "") + (r.get("stderr") or "")
    code = r.get("exit_code")
    if code is None:
        code = 1
    status = "timeout" if code == 124 else "ok"
    return {"output": out, "returncode": code, "status": status}

def terminate(origin, token, sandbox_id):
    """Destroy the machine. Logs failures instead of raising: this runs in a finally block."""
    try:
        fly("DELETE", f"/{sandbox_id}?force=true", token, timeout=30)
    except SandboxError as e:
        toollog.log_detail("sandbox", f"could not destroy machine {sandbox_id}: {e}")

NETWORK_PATTERNS = (
    "socket", "urllib", "requests", "http.client", "httplib", "httpx",
    "ftplib", "telnetlib", "smtplib", "asyncio.open_connection", "pycurl",
    "curl", "wget", "netcat", "nslookup", "dig ", "ping ", "traceroute",
    "/dev/tcp",
)

PROCESS_PATTERNS = (
    "subprocess", "os.system", "popen", "os.exec", "os.spawn", "os.fork",
    "pty.spawn", "multiprocessing", "&", "nohup",
)

# Model-written code never needs to build more code at runtime, so decode-then-run is
# refused while plain decoding stays allowed.
DYNAMIC_EXEC_PATTERNS = (
    "exec(", "eval(", "compile(", "__import__", "importlib", "runpy",
    "ctypes",
    # bash: piping anything back into an interpreter, and its eval builtin.
    "| bash", "|bash", "| sh", "|sh", "| python", "|python", "eval ",
    "source /dev", ". /dev", "<<'eof", '<<"eof', "<<eof",
)

NETWORK_REFUSAL = (
    "Error: refused - this tool is for computation only, not for network "
    "access. tell the user you don't make network connections on request."
)

PROCESS_REFUSAL = (
    "Error: refused - this tool does not spawn processes. it runs one "
    "snippet and returns its output; forking, exec-ing or shelling out is "
    "not computation. tell the user what was refused, accurately."
)

DYNAMIC_EXEC_REFUSAL = (
    "Error: refused - this tool does not run dynamically constructed or "
    "decoded code, which is how people try to smuggle network access past "
    "the check. tell the user to say what they actually want computed, in "
    "plain code."
)

def uses_network(code: str) -> bool:
    lowered = code.lower()
    return any(pattern in lowered for pattern in NETWORK_PATTERNS)

def uses_dynamic_exec(code: str) -> bool:
    lowered = code.lower()
    return any(pattern in lowered for pattern in DYNAMIC_EXEC_PATTERNS)

def spawns_process(code: str) -> bool:
    lowered = code.lower()
    return any(pattern in lowered for pattern in PROCESS_PATTERNS)

REVIEW_POLICY = """\
The action is executing a code snippet inside a disposable cloud sandbox \
(1 CPU, 2GB RAM, no network, wiped afterwards).

DENY if the code would:
- reach the network in any way (sockets, http, dns, ping, curl/wget, /dev/tcp)
- exhaust resources: fork bombs, unbounded loops spawning work, runaway \
allocation, filling the disk, crypto mining, deliberate CPU burning
- read or print the environment, credentials, tokens, cloud metadata, or \
container/infrastructure identifiers
- inspect or probe the sandbox, host, processes, or filesystem beyond what a \
computation needs
- build or decode code to execute at runtime (exec/eval of assembled strings)
- attempt to persist, escape, or affect anything outside its own process

ALLOW ordinary computation: arithmetic, algorithms, text and data processing, \
parsing, simulations, printing results, standard-library use with bounded work.

Bounded is the test, not clever. A loop with a fixed, reasonable iteration \
count is fine; one whose size is unbounded or absurd is not."""

REVIEW_REFUSAL = (
    "Error: refused - a safety check rejected this code before it ran ({reason}). "
    "tell the user what was refused and don't try to word it differently."
)

def review_code(code: str, language: str):
    return safetyreview.review(
        REVIEW_POLICY,
        f"Language: {language}\n{code}",
        label="SNIPPET",
        logger=lambda m: toollog.log_detail("sandbox", m),
    )

def run_code(code: str, language: str = "python") -> str:
    if not code.strip():
        return "Error: no code provided"

    if uses_network(code):
        toollog.log_detail("sandbox", f"refused network code: {code[:200]!r}")
        return NETWORK_REFUSAL

    if spawns_process(code):
        toollog.log_detail("sandbox", f"refused process-spawning code: {code[:200]!r}")
        return PROCESS_REFUSAL

    if uses_dynamic_exec(code):
        toollog.log_detail("sandbox", f"refused dynamic-exec code: {code[:200]!r}")
        return DYNAMIC_EXEC_REFUSAL

    # Judgement layer, after the cheap pattern checks so obvious cases never
    # cost a model call.
    if safetyreview.enabled("SANDBOX_REVIEW"):
        allowed, reason = review_code(code, language)
        if not allowed:
            toollog.log_detail("sandbox", f"review DENIED ({reason}): {code[:200]!r}")
            return REVIEW_REFUSAL.format(reason=reason)

    timeout_s = min(max(DEFAULT_TIMEOUT, 1), MAX_TIMEOUT)

    encoded = base64.b64encode(code.encode("utf-8")).decode("ascii")
    interpreter = "bash" if language == "bash" else "python3"
    command = f"echo {encoded} | base64 -d | {interpreter}"

    try:
        origin, token = resolve_gateway()
    except SandboxError as e:
        toollog.log_detail("sandbox", f"auth/resolve failed: {e}")
        return GENERIC_SANDBOX_ERROR

    sandbox_id = None
    try:
        sandbox_id = create_sandbox(origin, token, timeout_s)
        result = run_exec(origin, token, sandbox_id, command, timeout_s)
    except SandboxError as e:
        toollog.log_detail("sandbox", f"execution failed: {e}")
        return GENERIC_SANDBOX_ERROR
    finally:
        if sandbox_id:
            terminate(origin, token, sandbox_id)

    output = (result.get("output") or "").strip()
    returncode = result.get("returncode", 1)
    status = result.get("status")

    if status == "timeout":
        return f"Error: timed out after {timeout_s}s{chr(10) + output if output else ''}"

    if len(output) > MAX_OUTPUT:
        output = output[:MAX_OUTPUT] + f"\n... (truncated, {len(output)} chars total)"

    if not output:
        return f"(no output, exit code {returncode})"
    if returncode != 0:
        return f"exit code {returncode}:\n{output}"
    return output

def main():
    if len(sys.argv) < 2:
        print("Usage: sandbox.py [--schema | --execute <json>]")
        sys.exit(1)

    option = sys.argv[1]

    if option == "--schema":
        print_schema()
        return

    if option == "--execute":
        if len(sys.argv) < 3:
            print("Error: Missing JSON input for execution")
            sys.exit(1)
        try:
            input_data = json.loads(sys.argv[2])
        except json.JSONDecodeError:
            print("Error: Invalid JSON input")
            sys.exit(1)

        code = input_data.get("code")
        if not code:
            print("Error: Missing required 'code' in JSON input")
            sys.exit(1)
        language = input_data.get("language") or "python"
        if language not in ("python", "bash"):
            print(f"Error: unsupported language {language!r} (use python or bash)")
            sys.exit(1)

        print(run_code(code, language))
        return

    print("Usage: sandbox.py [--schema | --execute <json>]")
    sys.exit(1)

if __name__ == "__main__":
    main()
