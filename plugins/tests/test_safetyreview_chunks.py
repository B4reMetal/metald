# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

import os
import sys
import unittest
from unittest.mock import patch

sys.path[:0] = [os.path.join(os.path.dirname(__file__), "..", "lib")]

from metald_tools import safetyreview


class ChunkedReviewTests(unittest.TestCase):
    """A 25k-token emulator overflowed the 8k-token reviewer; long content is reviewed in parts."""

    def test_chunks_cover_everything_with_overlap(self):
        text = "".join(chr(ord("a") + i % 26) for i in range(50000))
        parts = safetyreview.chunks(text, 16000, 400)
        self.assertGreater(len(parts), 3)
        self.assertTrue(all(len(p) <= 16000 for p in parts))
        rebuilt = parts[0] + "".join(p[400:] for p in parts[1:])
        self.assertEqual(rebuilt, text)

    def test_short_content_is_one_part(self):
        self.assertEqual(safetyreview.chunks("short", 16000, 400), ["short"])

    def test_any_refused_part_refuses_all(self):
        text = "x" * 40000
        seen = []

        def one(policy, part, label, logger, threshold):
            seen.append(len(part))
            return (len(seen) != 2, "safety check" if len(seen) == 2 else "")

        with patch.object(safetyreview, "_review_one", side_effect=one), \
             patch.object(safetyreview, "CHUNK_CHARS", 16000):
            allowed, reason = safetyreview.review("policy", text)
        self.assertFalse(allowed)
        self.assertEqual(len(seen), 2, "review should stop at the first refused part")

    def test_every_part_is_reviewed_when_clean(self):
        calls = []
        with patch.object(safetyreview, "_review_one", side_effect=lambda *a: calls.append(1) or (True, "")), \
             patch.object(safetyreview, "CHUNK_CHARS", 16000):
            allowed, _ = safetyreview.review("policy", "y" * 40000)
        self.assertTrue(allowed)
        self.assertEqual(len(calls), len(safetyreview.chunks("y" * 40000, 16000, 400)))


if __name__ == "__main__":
    unittest.main()
