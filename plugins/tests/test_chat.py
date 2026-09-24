#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import os
import sys
import json
import unittest

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib"), os.path.dirname(os.path.abspath(__file__))]
from metald_tools import chat, vision
from fakeserver import FakeServer

class ChatTest(unittest.TestCase):
    def setUp(self):
        self.reply = (200, {"choices": [{"message": {"content": "hi"}, "finish_reason": "stop"}]})
        self.srv = FakeServer({"/v1/chat/completions": lambda p, b: self.reply})
        self.addCleanup(self.srv.close)
        self.url = self.srv.url + "/v1"

    def test_complete_sends_params_and_key(self):
        content, finish = chat.complete(self.url, "m", [{"role": "user", "content": "x"}],
                                        key="k", max_tokens=5, reasoning_effort="none")
        self.assertEqual((content, finish), ("hi", "stop"))
        sent = json.loads(self.srv.seen[0]["body"])
        self.assertEqual((sent["model"], sent["max_tokens"], sent["reasoning_effort"]), ("m", 5, "none"))
        self.assertEqual(self.srv.seen[0]["headers"]["authorization"], "Bearer k")

    def test_no_key_no_auth_header(self):
        chat.complete(self.url, "m", [])
        self.assertNotIn("authorization", self.srv.seen[0]["headers"])

    def test_http_error_and_no_choices(self):
        self.reply = (500, b"boom")
        with self.assertRaises(chat.ChatError):
            chat.complete(self.url, "m", [])
        self.reply = (200, {"choices": []})
        with self.assertRaises(chat.ChatError):
            chat.complete(self.url, "m", [])

    def test_endpoint_reads_prefixed_env(self):
        os.environ.update({"ZZ_URL": "http://u", "ZZ_MODEL": "mm"})
        self.addCleanup(lambda: [os.environ.pop(k, None) for k in ("ZZ_URL", "ZZ_MODEL")])
        self.assertEqual(chat.endpoint("ZZ"), {"url": "http://u", "model": "mm", "key": ""})

    def test_vision_describe_never_raises(self):
        vision.DEFAULT_API_URL = self.url
        self.assertEqual(vision.describe_image_bytes(b"img", "image/png", "what"), "hi")
        msg = json.loads(self.srv.seen[-1]["body"])["messages"][0]["content"]
        self.assertTrue(msg[1]["image_url"]["url"].startswith("data:image/png;base64,"))
        self.reply = (500, b"boom")
        self.assertTrue(vision.describe_image_bytes(b"img", "image/png", "what").startswith("Error:"))

    def test_image_safety_check_fails_closed(self):
        vision.DEFAULT_API_URL = "http://127.0.0.1:1/v1"
        ok, _ = vision.check_image_safety(b"img")
        self.assertFalse(ok)

if __name__ == "__main__":
    unittest.main()
