#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Holds the station's latest state for the listening page. The curator, at home, sends it (POST /state
with the shared secret); the page reads it (GET /state, behind the proxy's sign-in). The home side starts
every connection, so the server never needs a way in.

RELAY_SECRET  the shared secret (required)
"""

import hmac
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SECRET = os.environ["RELAY_SECRET"]
STATE, LOCK = {"now": None, "next": [], "history": []}, threading.Lock()


class Handler(BaseHTTPRequestHandler):
    def reply(self, status, body):
        data = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/state":
            with LOCK:
                return self.reply(200, json.dumps(STATE).encode())
        self.reply(404, {"error": "not found"})

    def do_POST(self):
        got = self.headers.get("Authorization", "")
        if self.path != "/state" or not hmac.compare_digest(got.encode(), ("Bearer " + SECRET).encode()):
            return self.reply(403, {"error": "forbidden"})
        try:
            body = json.loads(self.rfile.read(min(int(self.headers.get("Content-Length") or 0), 1 << 20)))
        except ValueError:
            return self.reply(400, {"error": "bad json"})
        if not isinstance(body, dict):
            return self.reply(400, {"error": "bad state"})
        with LOCK:
            STATE.clear()
            STATE.update(body)
        self.reply(200, {"ok": True})

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
