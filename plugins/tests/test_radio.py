#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import importlib
import json
import os
import sys
import unittest

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib"), os.path.dirname(os.path.abspath(__file__))]
from fakeserver import FakeServer


def load(server):
    os.environ.update(RADIO_API_URL=server.url, RADIO_TOKEN="t0k", RADIO_PAGE_URL="https://example.com/radio/",
                      RADIO_QUEUED_BY="botty")
    import radio
    return importlib.reload(radio)


class RadioTest(unittest.TestCase):
    def test_queue_sends_song_with_token(self):
        srv = FakeServer({"/push": lambda p, b: (200, {"queued": "blue hair", "position": 2})})
        self.addCleanup(srv.close)
        out = load(srv).run({"action": "queue", "url": "https://example.com/u/a.flac",
                             "title": "blue hair", "lyrics": "[Verse]\nla"})
        self.assertIn('queued "blue hair" at position 2', out)
        self.assertIn("https://example.com/radio/", out)
        sent = srv.seen[0]
        self.assertEqual(sent["headers"]["authorization"], "Bearer t0k")
        self.assertEqual(json.loads(sent["body"])["lyrics"], "[Verse]\nla")
        self.assertEqual(json.loads(sent["body"])["by"], "botty")

    def test_server_refusal_is_passed_on(self):
        srv = FakeServer({"/push": lambda p, b: (429, {"error": "the queue is full (15 tracks)"})})
        self.addCleanup(srv.close)
        out = load(srv).run({"action": "queue", "url": "https://example.com/u/a.flac"})
        self.assertEqual(out, "Refused: the queue is full (15 tracks)")

    def test_blocked_or_broken_server_is_generic(self):
        srv = FakeServer({"/push": lambda p, b: (403, b"forbidden")})
        self.addCleanup(srv.close)
        self.assertEqual(load(srv).run({"action": "queue", "url": "https://example.com/u/a.flac"}),
                         "Error: the radio is unavailable right now")

    def test_now_and_list(self):
        srv = FakeServer({"/now": lambda p, b: (200, {"title": "t", "by": "bob", "listeners": 3}),
                          "/queue": lambda p, b: (200, {"queue": ["one", "two"]})})
        self.addCleanup(srv.close)
        radio = load(srv)
        self.assertTrue(radio.run({}).startswith("now playing: t, queued by bob, 3 listening"))
        self.assertIn("1. one\n2. two", radio.run({"action": "list"}))

    def test_search_lists_songs_with_ids_and_downloads(self):
        tracks = [{"file": "dj1.flac", "title": "botty (dj)", "dj": True},
                  {"file": "k1.flac", "title": "kernel blues", "requested_by": "carol", "seconds": 211.9,
                   "queued_at": 86400, "downloads": 2}]
        srv = FakeServer({"/library": lambda p, b: (200, {"tracks": tracks})})
        self.addCleanup(srv.close)
        out = load(srv).run({"action": "search", "query": "kernel  panic"})
        self.assertEqual(srv.seen[0]["path"], "/library?q=kernel%20panic")
        self.assertIn('1 song matching "kernel panic"', out)
        self.assertIn('id k1 · "kernel blues" · requested by carol · 3:31 · Jan 02 · 2 downloads · '
                      "https://example.com/radio/dl/k1.flac", out)
        self.assertNotIn("dj1", out)

    def test_search_with_no_match(self):
        srv = FakeServer({"/library": lambda p, b: (200, {"tracks": []})})
        self.addCleanup(srv.close)
        self.assertEqual(load(srv).run({"action": "search", "query": "nope"}), 'no songs on the radio match "nope"')

    def test_queue_by_id_uses_the_library_copy(self):
        srv = FakeServer({"/push": lambda p, b: (200, {"queued": "kernel blues", "position": 1})})
        self.addCleanup(srv.close)
        radio = load(srv)
        radio.reachable = lambda url: self.fail("checked a link for a library song")
        out = radio.run({"action": "queue", "song": "k1", "intro": ""})
        self.assertIn('queued "kernel blues"', out)
        body = json.loads(srv.seen[0]["body"])
        self.assertEqual(body["file"], "k1.flac")
        self.assertNotIn("url", body)
        self.assertTrue(radio.run({"action": "queue", "song": "../x"}).startswith("Error: that isn't a song id"))

    def test_silent_radio(self):
        srv = FakeServer({"/now": lambda p, b: (200, {"listeners": 0}),
                          "/queue": lambda p, b: (200, {"queue": []})})
        self.addCleanup(srv.close)
        radio = load(srv)
        self.assertTrue(radio.run({}).startswith("nothing is playing"))
        self.assertTrue(radio.run({"action": "list"}).startswith("nothing queued, so the radio is silent"))

    def test_intro_is_spoken_and_queued_before_the_song(self):
        srv = FakeServer({"/push": lambda p, b: (200, {"queued": json.loads(b)["title"], "position": 1}),
                          "/u/": lambda p, b: (200, b"audio")})
        self.addCleanup(srv.close)
        radio = load(srv)
        spoken = []
        radio.speech.synthesize = lambda text, voice="", *delivery, **how: spoken.append((text, how.get("style"))) and False or b"fLaC-speech"
        radio.lyricsync.synced_lyrics = lambda path, lyrics: ""
        radio.upload_file = lambda data, name, ctype, deletes_at=None: "https://example.com/u/dj.flac"
        out = radio.run({"action": "queue", "url": srv.url + "/u/a.flac", "title": "song",
                         "intro": "That was loud.  Here's a quiet one."})
        self.assertIn('queued "song" with a spoken intro', out)
        pushes = [json.loads(r["body"]) for r in srv.seen if r["path"] == "/push"]
        self.assertEqual([p["url"] for p in pushes], ["https://example.com/u/dj.flac", srv.url + "/u/a.flac"])
        self.assertEqual(pushes[0]["title"], "botty (dj)")
        self.assertTrue(pushes[0]["dj"])
        self.assertNotIn("dj", pushes[1])
        self.assertEqual(spoken, [("That was loud. Here's a quiet one.", radio.DJ_STYLE)])
        spoken.clear()
        radio.run({"action": "queue", "url": srv.url + "/u/a.flac", "intro": "Night owls, this one's for you.",
                   "intro_style": "late-night radio, smooth and low"})
        self.assertEqual(spoken[0][1], "late-night radio, smooth and low")

    def test_no_intro_for_a_missing_song(self):
        srv = FakeServer({"/push": lambda p, b: (200, {"queued": "x", "position": 1})})
        self.addCleanup(srv.close)
        radio = load(srv)
        radio.speech.synthesize = lambda text, voice="", *delivery, **how: self.fail("spoke an intro for a missing song")
        out = radio.run({"action": "queue", "url": srv.url + "/u/gone.flac", "intro": "hello"})
        self.assertTrue(out.startswith("Refused"))
        self.assertFalse([r for r in srv.seen if r["path"] == "/push"])

    def test_intro_is_a_required_field(self):
        import contextlib, io
        srv = FakeServer({})
        self.addCleanup(srv.close)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            load(srv).print_schema()
        self.assertIn("intro", json.loads(out.getvalue())["required"])


if __name__ == "__main__":
    unittest.main()
