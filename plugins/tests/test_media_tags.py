#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import os
import sys
import unittest

_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib")]
from metald_tools import media


def block(btype, body, last=False):
    return bytes([(0x80 if last else 0) | btype]) + len(body).to_bytes(3, "big") + body


def comments(data):
    blocks, _ = media._flac_blocks(data)
    body = [b for t, b in blocks if t == 4][0]
    n = int.from_bytes(body[:4], "little")
    i = 4 + n
    count = int.from_bytes(body[i:i + 4], "little")
    i += 4
    out = []
    for _ in range(count):
        length = int.from_bytes(body[i:i + 4], "little")
        out.append(body[i + 4:i + 4 + length].decode())
        i += 4 + length
    return out


AUDIO = b"\xff\xf8audio-frames"
FLAC = b"fLaC" + block(0, b"\x00" * 34) + block(4, b"workflow-json", last=True) + AUDIO


class FlacTagTest(unittest.TestCase):
    def test_strip_then_tag_keeps_audio_and_only_our_tags(self):
        tagged = media.tag_flac(media.strip_flac_metadata(FLAC), {"LYRICS": "[Verse]\nla la", "TITLE": ""})
        self.assertTrue(tagged.endswith(AUDIO))
        self.assertEqual(comments(tagged), ["LYRICS=[Verse]\nla la"])
        self.assertNotIn(b"workflow-json", tagged)

    def test_not_flac_is_untouched(self):
        self.assertEqual(media.tag_flac(b"ID3garbage", {"LYRICS": "x"}), b"ID3garbage")


if __name__ == "__main__":
    unittest.main()
