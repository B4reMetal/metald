#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import os
import sys
import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib")]
from metald_tools import hosting

class Recorder(BaseHTTPRequestHandler):
    reply = b'{"url": "https://files.example.com/x.png"}'
    seen = []

    def _handle(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        Recorder.seen.append({"method": self.command, "path": self.path,
                              "headers": {k.lower(): v for k, v in self.headers.items()}, "body": body})
        self.send_response(200)
        self.end_headers()
        self.wfile.write(Recorder.reply)

    do_POST = do_PUT = _handle

    def log_message(self, *a):
        pass

class HostingTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = HTTPServer(("127.0.0.1", 0), Recorder)
        cls.base = "http://127.0.0.1:%d" % cls.server.server_port
        threading.Thread(target=cls.server.serve_forever, daemon=True).start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()

    def setUp(self):
        Recorder.seen = []
        Recorder.reply = b'{"url": "https://files.example.com/x.png"}'
        keep = {k: v for k, v in os.environ.items()}
        self.addCleanup(lambda: (os.environ.clear(), os.environ.update(keep)))
        for k in list(os.environ):
            if k.startswith(("UPLOAD_", "ZIPLINE_", "IMGBB_", "IMAGE_HOST")):
                del os.environ[k]
        os.environ["UPLOAD_RETRIES"] = "1"
        hosting.UPLOAD_RETRIES = 1

    def test_http_backend_multipart_with_headers_and_expiry(self):
        os.environ.update({"UPLOAD_BACKEND": "http", "UPLOAD_URL": self.base + "/up",
                           "UPLOAD_HEADERS": "X-Api-Key: $MY_KEY; X-Other: 1", "MY_KEY": "sekrit",
                           "UPLOAD_EXPIRY_HEADER": "X-Expires", "UPLOAD_FORM": "public=true"})
        url = hosting.upload_file(b"PNGDATA", "a.png", "image/png", deletes_at="12h")
        self.assertEqual(url, "https://files.example.com/x.png")
        r = Recorder.seen[0]
        self.assertEqual(r["headers"]["x-api-key"], "sekrit")
        self.assertEqual(r["headers"]["x-other"], "1")
        self.assertEqual(r["headers"]["x-expires"], "12h")
        self.assertIn(b'name="public"', r["body"])
        self.assertIn(b'filename="a.png"', r["body"])
        self.assertIn(b"PNGDATA", r["body"])

    def test_http_backend_raw_put_filename_in_url_and_text_reply(self):
        Recorder.reply = b"https://cdn.example.com/a b.flac\n"
        os.environ.update({"UPLOAD_BACKEND": "http", "UPLOAD_URL": self.base + "/files/{filename}",
                           "UPLOAD_METHOD": "PUT", "UPLOAD_BODY": "raw", "UPLOAD_RESPONSE": "text"})
        url = hosting.upload_file(b"FLAC", "a b.flac", "audio/flac")
        self.assertEqual(url, "https://cdn.example.com/a b.flac")
        r = Recorder.seen[0]
        self.assertEqual((r["method"], r["path"], r["body"]), ("PUT", "/files/a%20b.flac", b"FLAC"))
        self.assertEqual(r["headers"]["content-type"], "audio/flac")

    def test_json_path_response(self):
        Recorder.reply = b'{"result": {"links": [{"href": "https://h.example.com/1"}]}}'
        os.environ.update({"UPLOAD_BACKEND": "http", "UPLOAD_URL": self.base,
                           "UPLOAD_RESPONSE": "result.links.0.href"})
        self.assertEqual(hosting.upload_file(b"x", "x.png", "image/png"), "https://h.example.com/1")

    def test_response_without_url_is_an_error(self):
        Recorder.reply = b'{"ok": true}'
        os.environ.update({"UPLOAD_BACKEND": "http", "UPLOAD_URL": self.base})
        with self.assertRaises(hosting.HostingError):
            hosting.upload_file(b"x", "x.png", "image/png")

    def test_zipline_gets_extra_headers_and_they_override(self):
        Recorder.reply = b'{"files": [{"url": "https://zl.example.com/u/a.flac"}]}'
        os.environ.update({"ZIPLINE_URL": self.base, "ZIPLINE_TOKEN": "tok",
                           "UPLOAD_HEADERS": '{"X-Zipline-Folder": "bot", "Authorization": "override"}'})
        url = hosting.upload_file(b"x", "a.flac", "audio/flac", deletes_at="1h")
        self.assertEqual(url, "https://zl.example.com/u/a.flac")
        h = Recorder.seen[0]["headers"]
        self.assertEqual(Recorder.seen[0]["path"], "/api/upload")
        self.assertEqual(h["x-zipline-folder"], "bot")
        self.assertEqual(h["authorization"], "override")
        self.assertEqual(h["x-zipline-deletes-at"], "1h")

    def test_imgbb_refuses_non_images_without_a_request(self):
        os.environ.update({"UPLOAD_BACKEND": "imgbb", "IMGBB_API_KEY": "k"})
        with self.assertRaises(hosting.HostingError):
            hosting.upload_file(b"x", "a.flac", "audio/flac")

    def test_requires_follows_backend(self):
        self.assertEqual(hosting.requires(), ["ZIPLINE_URL", "ZIPLINE_TOKEN"])
        os.environ["IMAGE_HOST_BACKEND"] = "imgbb"
        self.assertEqual(hosting.requires(), ["IMGBB_API_KEY"])
        os.environ["UPLOAD_BACKEND"] = "http"
        self.assertEqual(hosting.requires(), ["UPLOAD_URL"])

    def test_register_backend(self):
        hosting.register_backend("mine", lambda d, f, c, e: "https://mine.example.com/" + f, requires=["MINE_KEY"])
        self.addCleanup(hosting.BACKENDS.pop, "mine")
        os.environ["UPLOAD_BACKEND"] = "mine"
        self.assertEqual(hosting.requires(), ["MINE_KEY"])
        self.assertEqual(hosting.upload_file(b"x", "a.png", "image/png"), "https://mine.example.com/a.png")

    def test_unknown_backend(self):
        os.environ["UPLOAD_BACKEND"] = "nope"
        with self.assertRaises(hosting.HostingError):
            hosting.upload_file(b"x", "a.png", "image/png")

    def test_bad_json_headers(self):
        os.environ["UPLOAD_HEADERS"] = "{not json"
        with self.assertRaises(hosting.HostingError):
            hosting.extra_headers()

if __name__ == "__main__":
    unittest.main()
