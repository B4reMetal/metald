#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

import os
import sys
import unittest

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib")]
import musicgen
from metald_tools import lyricist, comfyui
from metald_tools import safetyreview

class TestGenerateWiring(unittest.TestCase):
    """generate() with every backend stubbed: what reaches the workflow and
    what comes back."""

    def setUp(self):
        self.calls = {}
        for mod, name in ((comfyui, "run"), (musicgen, "upload_file"),
                          (safetyreview, "review"), (lyricist, "write")):
            self.addCleanup(setattr, mod, name, getattr(mod, name))
        def render(wf, output, timeout, log=None):
            self.calls.setdefault("lyrics", wf["prompt"]["2"]["inputs"]["lyrics"])
            return b"fLaC"
        comfyui.run = render
        musicgen.upload_file = lambda *a, **k: "https://files.example.com/u/x.flac"
        safetyreview.review = lambda policy, content, **k: self.calls.setdefault("reviewed", content) and (True, "")
        lyricist.write = lambda brief, draft, style, seconds, logger=None: "[Verse]\nimproved: " + draft
        os.environ.pop("LYRICIST", None)

    def test_draft_goes_through_lyricist_and_review_sees_final_words(self):
        out = musicgen.generate("folk", "", "my draft", False, 60)
        self.assertEqual(self.calls["lyrics"], "[Verse]\nimproved: my draft")
        self.assertIn("improved: my draft", self.calls["reviewed"])
        self.assertIn("url: https://files.example.com/u/x.flac", out)
        self.assertIn("lyrics as sung", out)

    def test_instrumental_skips_lyricist(self):
        lyricist.write = lambda *a, **k: self.fail("lyricist called for an instrumental")
        out = musicgen.generate("folk", "old dog", "draft", True, 60)
        self.assertEqual(self.calls["lyrics"], "")
        self.assertEqual(out, "url: https://files.example.com/u/x.flac")

    def test_lyricist_down_falls_back_to_draft(self):
        def boom(*a, **k):
            raise lyricist.LyricistError("unavailable")
        lyricist.write = boom
        musicgen.generate("folk", "", "[Verse]\nmy draft", False, 60)
        self.assertEqual(self.calls["lyrics"], "[Verse]\nmy draft")

    def test_no_draft_is_an_error_before_any_model_call(self):
        lyricist.write = lambda *a, **k: self.fail("lyricist called without a draft")
        out = musicgen.generate("folk", "old dog", "", False, 60)
        self.assertTrue(out.startswith("Error: write draft lyrics"))
        self.assertNotIn("lyrics", self.calls)

    def test_refused_song_is_not_rendered(self):
        safetyreview.review = lambda *a, **k: (False, "slur")
        out = musicgen.generate("folk", "", "my draft", False, 60)
        self.assertTrue(out.startswith("Refused: slur"))
        self.assertNotIn("lyrics", self.calls)

if __name__ == "__main__":
    unittest.main()
