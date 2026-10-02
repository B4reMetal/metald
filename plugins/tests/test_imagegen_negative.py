# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

import os
import sys
import unittest
from unittest.mock import patch

sys.path[:0] = [os.path.join(os.path.dirname(__file__), ".."), os.path.join(os.path.dirname(__file__), "..", "lib")]

import imagegen


def sampler(wf):
    return wf["prompt"]["6"]["inputs"]


def encoder(wf):
    return wf["prompt"]["4"]["inputs"]


class NegativePromptTests(unittest.TestCase):
    """At cfg 1 the sampler ignores the negative, so a negative must come with more guidance."""

    def test_no_negative_keeps_fast_path(self):
        with patch.object(imagegen, "DEFAULT_NEGATIVE", ""):
            wf = imagegen.build_workflow("a cat", 1024, 1024)
        self.assertEqual(encoder(wf)["negative_prompt"], "")
        self.assertEqual(sampler(wf)["cfg"], imagegen.GEN_CFG)

    def test_negative_raises_guidance(self):
        with patch.object(imagegen, "DEFAULT_NEGATIVE", ""):
            wf = imagegen.build_workflow("a cat", 1024, 1024, "text, watermark")
        self.assertEqual(encoder(wf)["negative_prompt"], "text, watermark")
        self.assertEqual(sampler(wf)["cfg"], imagegen.NEGATIVE_CFG)
        self.assertGreater(sampler(wf)["cfg"], 1.0)

    def test_default_and_request_negatives_combine(self):
        with patch.object(imagegen, "DEFAULT_NEGATIVE", "extra fingers"):
            self.assertEqual(imagegen.combined_negative("a hat"), "extra fingers, a hat")
            self.assertEqual(imagegen.combined_negative(""), "extra fingers")

    def test_negative_is_bounded(self):
        with patch.object(imagegen, "DEFAULT_NEGATIVE", ""):
            self.assertLessEqual(len(imagegen.combined_negative("x" * 5000)), imagegen.MAX_NEGATIVE)

    def test_schema_offers_negative(self):
        import io, json
        from contextlib import redirect_stdout
        buf = io.StringIO()
        with redirect_stdout(buf):
            imagegen.print_schema()
        props = json.loads(buf.getvalue())["properties"]
        self.assertIn("negative", props)
        self.assertNotIn("negative", json.loads(buf.getvalue())["required"])


if __name__ == "__main__":
    unittest.main()
