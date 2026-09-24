# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""A tiny scripted HTTP server for plugin tests: routes map a path prefix to
a handler returning (status, body_bytes)."""
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

class FakeServer:
    def __init__(self, routes):
        self.routes, self.seen = routes, []
        outer = self

        class H(BaseHTTPRequestHandler):
            def _go(self):
                body = self.rfile.read(int(self.headers.get("Content-Length", 0) or 0))
                outer.seen.append({"method": self.command, "path": self.path, "body": body,
                                   "headers": {k.lower(): v for k, v in self.headers.items()}})
                for prefix, fn in outer.routes.items():
                    if self.path.startswith(prefix):
                        status, out = fn(self.path, body)
                        break
                else:
                    status, out = 404, b"not found"
                if isinstance(out, (dict, list)):
                    out = json.dumps(out).encode()
                self.send_response(status)
                self.end_headers()
                self.wfile.write(out)
            do_GET = do_POST = do_PUT = _go

            def log_message(self, *a):
                pass

        self.httpd = HTTPServer(("127.0.0.1", 0), H)
        self.url = "http://127.0.0.1:%d" % self.httpd.server_port
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def close(self):
        self.httpd.shutdown()
