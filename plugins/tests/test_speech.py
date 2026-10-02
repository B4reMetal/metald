#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import importlib
import os
import sys
import unittest

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [os.path.join(_plugins, "lib")]
from metald_tools import comfyui, speech


def load(engine):
    os.environ.update(TTS_ENGINE=engine, TTS_VOICES="aussie=voice_aussie.wav")
    return importlib.reload(speech)


class SpeechEngineTest(unittest.TestCase):
    def setUp(self):
        self.addCleanup(setattr, comfyui, "run", comfyui.run)
        self.sent = []

    def fake(self, fail_moss=None):
        def run(wf, output, timeout, log=None):
            node = next(n for n in wf["prompt"].values() if n["class_type"] not in ("SaveAudio", "LoadAudio", "MOSSLoadModel"))
            self.sent.append(node)
            if fail_moss and node["class_type"].startswith("MOSS"):
                raise fail_moss
            return b"fLaC"
        comfyui.run = run

    def test_moss_clones_the_voice_with_the_style(self):
        sp = load("moss")
        self.fake()
        sp.synthesize("g'day legends", "aussie", style="  energetic   radio DJ ", language="English")
        node = self.sent[0]
        self.assertEqual(node["class_type"], "MOSSVoiceClone")
        self.assertEqual(node["inputs"]["instruction"], "energetic radio DJ")
        self.assertGreaterEqual(node["inputs"]["max_new_tokens"], 256)

    def test_moss_without_a_known_voice_speaks_in_its_own(self):
        sp = load("moss")
        self.fake()
        sp.synthesize("hello", "nobody", language="Klingon")
        self.assertEqual(self.sent[0]["class_type"], "MOSSSpeak")
        self.assertEqual(self.sent[0]["inputs"]["language"], "English")

    def test_a_moss_failure_falls_back_to_chatterbox_without_pause_marks(self):
        sp = load("moss")
        self.fake(fail_moss=comfyui.ComfyError("generation failed"))
        sp.synthesize("and the winner is [pause 1.5s] nobody", "aussie")
        self.assertEqual(self.sent[-1]["class_type"], "FL_ChatterboxTTS")
        self.assertEqual(self.sent[-1]["inputs"]["text"], "and the winner is ... nobody")

    def test_a_cancelled_moss_job_is_not_retried(self):
        sp = load("moss")
        self.fake(fail_moss=comfyui.ComfyCancelled("cancelled"))
        with self.assertRaises(sp.SpeechError):
            sp.synthesize("hello", "aussie")
        self.assertNotIn("FL_ChatterboxTTS", [n["class_type"] for n in self.sent])

    def test_chatterbox_engine_ignores_style(self):
        sp = load("chatterbox")
        self.fake()
        sp.synthesize("hello", "aussie", 0.9, 0.3, style="furious")
        self.assertEqual(self.sent[0]["class_type"], "FL_ChatterboxTTS")
        self.assertEqual(self.sent[0]["inputs"]["exaggeration"], 0.9)

    def test_the_length_cap_grows_with_text_and_pauses(self):
        sp = load("moss")
        self.assertEqual(sp.moss_frames("hi"), 256)
        self.assertGreater(sp.moss_frames("word " * 200), sp.moss_frames("word " * 50))
        self.assertGreater(sp.moss_frames("x " * 100 + "[pause 9s]"), sp.moss_frames("x " * 100))


if __name__ == "__main__":
    unittest.main()
