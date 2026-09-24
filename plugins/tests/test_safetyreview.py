#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import os
import re
import sys
import unittest

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib")]
from metald_tools import safetyreview

class TestScoreNeverLeaks(unittest.TestCase):
    def setUp(self):
        self.mode, self.score = safetyreview.REVIEW_MODE, safetyreview._unsafe_score
        safetyreview.REVIEW_MODE = "score"

    def tearDown(self):
        safetyreview.REVIEW_MODE, safetyreview._unsafe_score = self.mode, self.score

    def test_denial_reason_has_no_number(self):
        safetyreview._unsafe_score = lambda *a, **k: 0.7312
        logged = []
        allowed, reason = safetyreview.review("p", "c", logger=logged.append)
        self.assertFalse(allowed)
        self.assertIsNone(re.search(r"\d", reason), reason)
        self.assertTrue(any("0.7312" in m for m in logged), "operator log should keep the score")

    def test_allow_has_empty_reason(self):
        safetyreview._unsafe_score = lambda *a, **k: 0.01
        self.assertEqual(safetyreview.review("p", "c"), (True, ""))

    def test_unavailable_reason_has_no_number(self):
        safetyreview._unsafe_score = lambda *a, **k: None
        allowed, reason = safetyreview.review("p", "c")
        self.assertFalse(allowed)
        self.assertIsNone(re.search(r"\d", reason))

class TestVerdictNeedsPreamble(unittest.TestCase):
    def test_missing_preamble_fails_closed_without_a_request(self):
        mode, pre = safetyreview.REVIEW_MODE, safetyreview.PREAMBLE
        safetyreview.REVIEW_MODE, safetyreview.PREAMBLE = "verdict", ""
        try:
            self.assertEqual(safetyreview.review("p", "c"), (False, "safety check unavailable"))
        finally:
            safetyreview.REVIEW_MODE, safetyreview.PREAMBLE = mode, pre

    def test_requires_preamble_only_in_verdict_mode(self):
        mode = safetyreview.REVIEW_MODE
        try:
            safetyreview.REVIEW_MODE = "verdict"
            self.assertEqual(safetyreview.requires(), ["SAFETY_REVIEW_PREAMBLE"])
            safetyreview.REVIEW_MODE = "score"
            self.assertEqual(safetyreview.requires(), [])
        finally:
            safetyreview.REVIEW_MODE = mode

if __name__ == "__main__":
    unittest.main()
