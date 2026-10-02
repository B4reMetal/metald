#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import json
import os
import sys
import unittest

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib"), os.path.dirname(os.path.abspath(__file__))]
from metald_tools import lyricsync
from fakeserver import FakeServer

LYRICS = "[Verse]\nthe cat sat on the mat\nand then it ran away\n[Chorus]\nla la la\nsing it loud tonight"


def heard(*pairs):
    return [(w, t) for t, text in pairs for w in text.split()]


class AlignTest(unittest.TestCase):
    def test_lines_take_their_first_heard_word_and_labels_follow(self):
        words = heard((3.0, "the cat sat on the mat"), (7.5, "and then it ran away"),
                      (12.0, "la la la"), (15.0, "sing it loud tonight"))
        self.assertEqual(lyricsync.align(LYRICS, words), [
            (3.0, "[Verse]"), (3.0, "the cat sat on the mat"), (7.5, "and then it ran away"),
            (12.0, "[Chorus]"), (12.0, "la la la"), (15.0, "sing it loud tonight")])

    def test_unheard_line_is_placed_between_its_neighbours(self):
        words = heard((3.0, "the cat sat on the mat"), (15.0, "sing it loud tonight"))
        times = dict((line, t) for t, line in lyricsync.align(LYRICS, words))
        self.assertTrue(3.0 < times["and then it ran away"] < times["la la la"] < 15.0)

    def test_misheard_words_still_anchor_and_time_never_goes_back(self):
        words = heard((3.0, "the bat sat on the hat"), (2.0, "and then it ran away"), (12.0, "la la"))
        timed = lyricsync.align(LYRICS, words)
        self.assertEqual([t for t, _ in timed], sorted(t for t, _ in timed))

    def test_nothing_heard(self):
        self.assertEqual(lyricsync.align(LYRICS, []), [])

    def test_lrc(self):
        self.assertEqual(lyricsync.to_lrc([(75.5, "hi")]), "[01:15.50]hi")


class AlignerTest(unittest.TestCase):
    def setUp(self):
        import tempfile
        f = tempfile.NamedTemporaryFile(suffix=".flac", delete=False)
        f.write(b"fLaC-audio")
        f.close()
        self.audio = f.name
        self.addCleanup(os.remove, f.name)
        for name in ("ALIGN", "WHISPER_URL"):
            self.addCleanup(setattr, lyricsync, name, getattr(lyricsync, name))
        for name in ("upload", "result"):
            self.addCleanup(setattr, lyricsync.comfyui, name, getattr(lyricsync.comfyui, name))
        self.uploads, self.workflows = [], []
        lyricsync.comfyui.upload = lambda data, name, log=None: self.uploads.append((data, name)) or name
        lyricsync.ALIGN, lyricsync.WHISPER_URL = True, ""

    def test_aligner_timings_become_lrc(self):
        def result(wf, key, timeout, log=None):
            self.workflows.append(wf)
            return json.dumps({"lines": [[1.5, "[Verse]"], [1.5, "the cat sat"]]})
        lyricsync.comfyui.result = result
        self.assertEqual(lyricsync.synced_lyrics(self.audio, "[Verse]\nthe cat sat"),
                         "[00:01.50][Verse]\n[00:01.50]the cat sat")
        data, name = self.uploads[0]
        node = self.workflows[0]["prompt"]["1"]
        self.assertEqual((data, node["class_type"], node["inputs"]["audio_file"], node["inputs"]["lyrics"]),
                         (b"fLaC-audio", "MetaldLyricAlign", name, "[Verse]\nthe cat sat"))
        self.assertTrue(name.endswith(".flac"))

    def test_aligner_failure_without_whisper_gives_nothing(self):
        def result(wf, key, timeout, log=None):
            raise lyricsync.comfyui.ComfyError("generation failed")
        lyricsync.comfyui.result = result
        self.assertEqual(lyricsync.synced_lyrics(self.audio, "the cat sat"), "")

    def test_off_means_whisper_only(self):
        lyricsync.ALIGN = False
        lyricsync.comfyui.result = lambda *a, **k: self.fail("asked the aligner while it is off")
        self.assertEqual(lyricsync.synced_lyrics(self.audio, "the cat sat"), "")


if __name__ == "__main__":
    unittest.main()
