#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import os
import sys
import unittest

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib"), os.path.dirname(os.path.abspath(__file__))]
from metald_tools import comfyui
from fakeserver import FakeServer

class ComfyTest(unittest.TestCase):
    def setUp(self):
        self.polls = 0
        self.history = None
        self.logged = []
        self.srv = FakeServer({
            "/prompt": lambda p, b: (200, {"prompt_id": "abc"}),
            "/history/abc": self._history,
            "/view": lambda p, b: (200, b"FILEDATA:" + p.encode()),
        })
        self.addCleanup(self.srv.close)
        old = os.environ.get("COMFYUI_URL")
        os.environ["COMFYUI_URL"] = self.srv.url
        self.addCleanup(lambda: os.environ.__setitem__("COMFYUI_URL", old) if old else os.environ.pop("COMFYUI_URL", None))
        comfyui.POLL_INTERVAL = 0.01

    def _history(self, path, body):
        self.polls += 1
        return 200, self.history(self.polls) if self.history else {}

    def log(self, m):
        self.logged.append(m)

    def test_run_waits_then_fetches_matching_output(self):
        self.history = lambda n: {} if n < 3 else {"abc": {"status": {"completed": True},
            "outputs": {"9": {"audio": [{"filename": "a.flac", "subfolder": "s", "type": "output"}]}}}}
        data = comfyui.run({"prompt": {}}, "audio", timeout=5, log=self.log)
        self.assertTrue(data.startswith(b"FILEDATA:/view?"))
        self.assertIn(b"filename=a.flac", data)
        self.assertGreaterEqual(self.polls, 3)

    def test_suffix_match_finds_video_under_any_key(self):
        self.history = lambda n: {"abc": {"status": {"completed": True}, "outputs": {
            "1": {"images": [{"filename": "x.png"}]},
            "2": {"images": [{"filename": "clip.mp4", "subfolder": "video"}], "animated": [True]}}}}
        item = comfyui.wait("abc", ".mp4", timeout=5, log=self.log)
        self.assertEqual(item["filename"], "clip.mp4")

    def test_error_status_fails_fast_with_generic_message(self):
        self.history = lambda n: {"abc": {"status": {"status_str": "error", "messages": ["OOM at /secret/path"]}}}
        with self.assertRaises(comfyui.ComfyError) as cm:
            comfyui.wait("abc", "images", timeout=5, log=self.log)
        self.assertEqual(str(cm.exception), "generation failed")
        self.assertTrue(any("/secret/path" in m for m in self.logged), "detail goes to the log")
        self.assertEqual(self.polls, 1)

    def test_completed_without_output(self):
        self.history = lambda n: {"abc": {"status": {"completed": True}, "outputs": {}}}
        with self.assertRaises(comfyui.ComfyError) as cm:
            comfyui.wait("abc", "audio", timeout=5, log=self.log)
        self.assertEqual(str(cm.exception), "generation produced no audio")

    def test_timeout(self):
        with self.assertRaises(comfyui.ComfyError) as cm:
            comfyui.wait("abc", "images", timeout=0.1, log=self.log)
        self.assertEqual(str(cm.exception), "generation timed out")

    def test_node_errors_rejected(self):
        self.srv.routes["/prompt"] = lambda p, b: (200, {"node_errors": {"3": "bad input"}})
        with self.assertRaises(comfyui.ComfyError) as cm:
            comfyui.submit({"prompt": {}}, log=self.log)
        self.assertEqual(str(cm.exception), "workflow rejected")

    def test_unreachable_does_not_leak_address(self):
        os.environ["COMFYUI_URL"] = "http://127.0.0.1:1"
        with self.assertRaises(comfyui.ComfyError) as cm:
            comfyui.submit({"prompt": {}}, log=self.log)
        self.assertEqual(str(cm.exception), "backend unreachable")
        self.assertNotIn("127.0.0.1", str(cm.exception))

if __name__ == "__main__":
    unittest.main()
