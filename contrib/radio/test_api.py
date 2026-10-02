#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
import importlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request

os.environ.update(RADIO_TOKEN="t", RADIO_SOURCE_PREFIX="https://files.example.com/u/", RADIO_QUEUE_MAX="2",
                  RADIO_LIBRARY_MAX="2")
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import api


class ApiCase(unittest.TestCase):
    def setUp(self):
        importlib.reload(api)
        root = tempfile.mkdtemp()
        api.UPLOADS, api.LIBRARY, api.META = (os.path.join(root, d) for d in ("uploads", "library", "meta"))
        for d in (api.UPLOADS, api.LIBRARY, api.META):
            os.mkdir(d)
        api.KEYS = os.path.join(root, "keys.json")
        self.pending, self.sent, self.playing = [], [], ""

        def telnet(cmd):
            self.sent.append(cmd)
            if cmd == "pending":
                return "\n".join(self.pending)
            if cmd == "now":
                return self.playing + "\n1" if self.playing else ""
            return "7"
        api.telnet = telnet

    def upload(self, name):
        with open(os.path.join(api.UPLOADS, name), "wb") as f:
            f.write(b"x")


class PushTest(ApiCase):
    def test_queues_a_hosted_file_with_its_lyrics(self):
        self.upload("a1.flac")
        status, out = api.push({"url": "https://files.example.com/u/a1.flac", "title": "t", "lyrics": "la"})
        self.assertEqual((status, out["position"]), (200, 1))
        self.assertIn("q.push " + os.path.join(api.LIBRARY, "a1.flac"), self.sent)
        self.assertEqual(api.meta_for("/library/a1.flac")["lyrics"], "la")

    def test_embedded_lyrics_win(self):
        sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "plugins", "lib"))
        from metald_tools import media
        flac = b"fLaC" + bytes([0x80]) + (34).to_bytes(3, "big") + b"\x00" * 34 + b"\xff\xf8"
        with open(os.path.join(api.UPLOADS, "s.flac"), "wb") as f:
            f.write(media.tag_flac(flac, {"LYRICS": "as sung", "SYNCEDLYRICS": "[00:01.50][Verse]\n[01:02.00]as sung"}))
        api.push({"url": "https://files.example.com/u/s.flac", "lyrics": "made up"})
        meta = api.meta_for("/library/s.flac")
        self.assertEqual(meta["lyrics"], "as sung")
        self.assertEqual(meta["synced"], [[1.5, "[Verse]"], [62.0, "as sung"]])

    def test_library_credits_the_requester(self):
        sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "plugins", "lib"))
        from metald_tools import media
        flac = b"fLaC" + bytes([0x80]) + (34).to_bytes(3, "big") + b"\x00" * 34 + b"\xff\xf8"
        with open(os.path.join(api.UPLOADS, "q.flac"), "wb") as f:
            f.write(media.tag_flac(flac, {"REQUESTED_BY": "alice"}))
        api.push({"url": "https://files.example.com/u/q.flac"})
        track = next(t for t in api.library() if t["file"] == "q.flac")
        self.assertEqual(track["requested_by"], "alice")

    def test_downloads_are_counted_once_per_download(self):
        self.upload("d.flac")
        api.push({"url": "https://files.example.com/u/d.flac"})
        api.count_download("/d.flac", None)
        api.count_download("/radio/dl/d.flac?x=1", "bytes=0-")
        api.count_download("/d.flac", "bytes=5000-")  # resuming the same download
        api.count_download("/../meta/d.flac.json", None)
        api.count_download("/missing.flac", None)
        self.assertEqual(api.meta_for("/library/d.flac")["downloads"], 2)
        api.push({"url": "https://files.example.com/u/d.flac"})  # requeued: the count stays
        self.assertEqual(next(t for t in api.library() if t["file"] == "d.flac")["downloads"], 2)

    def test_library_search_matches_title_requester_and_lyrics(self):
        sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "plugins", "lib"))
        from metald_tools import media
        flac = b"fLaC" + bytes([0x80]) + (34).to_bytes(3, "big") + b"\x00" * 34 + b"\xff\xf8"
        with open(os.path.join(api.UPLOADS, "k.flac"), "wb") as f:
            f.write(media.tag_flac(flac, {"REQUESTED_BY": "carol", "LYRICS": "the kernel panics at dawn"}))
        api.push({"url": "https://files.example.com/u/k.flac", "title": "Oops Linux"})
        self.upload("o.flac")
        api.push({"url": "https://files.example.com/u/o.flac", "title": "other"})
        found = lambda q: [t["file"] for t in api.library(q)]
        self.assertEqual(found("linux"), ["k.flac"])
        self.assertEqual(found("CAROL"), ["k.flac"])
        self.assertEqual(found("panics dawn"), ["k.flac"])
        self.assertEqual(found("panics other"), [])
        self.assertEqual(len(found("")), 2)

    def test_saved_timings_beat_the_files(self):
        sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "plugins", "lib"))
        from metald_tools import media
        flac = b"fLaC" + bytes([0x80]) + (34).to_bytes(3, "big") + b"\x00" * 34 + b"\xff\xf8"
        with open(os.path.join(api.UPLOADS, "r.flac"), "wb") as f:
            f.write(media.tag_flac(flac, {"LYRICS": "la", "SYNCEDLYRICS": "[00:09.00]la"}))
        api.push({"url": "https://files.example.com/u/r.flac"})
        path = os.path.join(api.META, "r.flac.json")
        meta = json.load(open(path))
        json.dump({**meta, "synced": [[2.0, "la"]]}, open(path, "w"))  # re-timed later
        api.push({"url": "https://files.example.com/u/r.flac"})
        self.assertEqual(api.meta_for("/library/r.flac")["synced"], [[2.0, "la"]])

    def test_dj_clips_skip_the_crossfade(self):
        self.upload("dj.flac")
        api.push({"url": "https://files.example.com/u/dj.flac", "dj": True})
        self.assertIn('q.push annotate:liq_cross_duration="0.":' + os.path.join(api.LIBRARY, "dj.flac"), self.sent)

    def test_requeue_from_the_library_keeps_what_it_had(self):
        self.upload("dj2.flac")
        api.push({"url": "https://files.example.com/u/dj2.flac", "title": "botty (dj)", "dj": True})
        os.remove(os.path.join(api.UPLOADS, "dj2.flac"))  # the file host has expired it since
        self.sent.clear()
        status, out = api.push({"file": "dj2.flac"})
        self.assertEqual((status, out["queued"]), (200, "botty (dj)"))
        self.assertIn('q.push annotate:liq_cross_duration="0.":' + os.path.join(api.LIBRARY, "dj2.flac"), self.sent)
        self.assertEqual(api.push({"file": "../etc/passwd"})[0], 404)
        self.assertEqual(api.push({"file": "nothere.flac"})[0], 404)
        self.assertEqual([t["title"] for t in api.library()], ["botty (dj)"])

    def test_refusals(self):
        self.upload("a1.flac")
        for url, status in [("https://evil.example/u/a1.flac", 400),
                            ("https://files.example.com/u/../../etc/passwd", 400),
                            ("https://files.example.com/u/a1.exe", 400),
                            ("https://files.example.com/u/gone.mp3", 404)]:
            self.assertEqual(api.push({"url": url})[0], status, url)
        self.pending = ["/library/x.mp3", "/library/y.mp3"]
        self.assertEqual(api.push({"url": "https://files.example.com/u/a1.flac"})[0], 429)
        self.assertNotIn("q.push", " ".join(self.sent))

    def test_pending_strips_annotations(self):
        self.pending = ['annotate:liq_cross_duration="0.":/library/dj.flac', "/library/song.flac"]
        self.assertEqual(api.pending(), ["/library/dj.flac", "/library/song.flac"])

    def test_retention_spares_queued_and_playing(self):
        day = 86400
        for name, age in [("old.mp3", 10), ("queued.mp3", 10), ("playing.mp3", 10), ("fresh.mp3", 1)]:
            path = os.path.join(api.LIBRARY, name)
            open(path, "wb").write(b"x")
            open(os.path.join(api.META, name + ".json"), "w").write("{}")
            os.utime(path, (1e9 - age * day, 1e9 - age * day))
        open(os.path.join(api.META, "orphan.mp3.json"), "w").write("{}")
        self.pending = [os.path.join(api.LIBRARY, "queued.mp3")]
        self.playing = os.path.join(api.LIBRARY, "playing.mp3")
        api.LIBRARY_MAX = 10
        api.prune(now=1e9)
        self.assertEqual(sorted(os.listdir(api.LIBRARY)), ["fresh.mp3", "playing.mp3", "queued.mp3"])
        self.assertEqual(sorted(os.listdir(api.META)), ["fresh.mp3.json", "playing.mp3.json", "queued.mp3.json"])

    def test_caps_remove_the_oldest_first(self):
        for i, name in enumerate(["a.mp3", "b.mp3", "c.mp3"]):
            path = os.path.join(api.LIBRARY, name)
            open(path, "wb").write(b"x" * 1024)
            os.utime(path, (1e9 + i, 1e9 + i))
        api.prune(now=1e9 + 10)  # LIBRARY_MAX is 2
        self.assertEqual(sorted(os.listdir(api.LIBRARY)), ["b.mp3", "c.mp3"])
        api.LIBRARY_MAX, api.LIBRARY_MAX_MB = 10, 1.5 / 1024  # 1.5 KB: room for one file
        api.prune(now=1e9 + 10)
        self.assertEqual(os.listdir(api.LIBRARY), ["c.mp3"])

FLAC = b"fLaC" + bytes([0x80]) + (34).to_bytes(3, "big") + b"\x00" * 34 + b"\xff\xf8"


class KeysTest(ApiCase):
    def setUp(self):
        super().setUp()
        self.fetched = []
        api.fetch = lambda url, *cap: self.fetched.append(url) or FLAC
        self.real_needs_lyrics, api.needs_lyrics = api.needs_lyrics, lambda meta: False  # holds: HoldTest

    def key(self, name="alice"):
        status, out = api.create_key(name)
        self.assertEqual(status, 200)
        return out

    def test_a_key_works_until_revoked_and_is_stored_hashed(self):
        k = self.key()
        self.assertEqual(api.key_from("Bearer " + k["key"]), k["id"])
        self.assertNotIn(k["key"], open(api.KEYS).read())
        self.assertIsNone(api.key_from("Bearer rk_wrong"))
        self.assertIsNone(api.key_from(k["key"]))  # not as a bearer token
        api.revoke_key(k["id"])
        self.assertIsNone(api.key_from("Bearer " + k["key"]))
        listed = api.list_keys()[0]
        self.assertTrue(listed["revoked"])
        self.assertNotIn("hash", listed)

    def test_a_request_fetches_queues_and_credits_the_key(self):
        k = self.key("carol")
        status, out = api.request(k["id"], {"url": "https://example.com/music/Night_Drive.flac"})
        self.assertEqual(status, 200)
        self.assertEqual(self.fetched, ["https://example.com/music/Night_Drive.flac"])
        name = next(n for n in os.listdir(api.LIBRARY))
        meta = api.meta_for(os.path.join(api.LIBRARY, name))
        self.assertEqual((meta["title"], meta["by"], meta["requested_by"], meta["key_id"]),
                         ("Night Drive", "carol", "carol", k["id"]))
        self.assertEqual(api.list_keys()[0]["uses"], 1)

    def test_timed_lyrics_are_taken_as_sent(self):
        kid = self.key()["id"]
        lrc = "[ar:someone]\n[00:12.50]first <00:13.00>line\n[00:20.00][01:05.25]chorus\n[00:30.00]"
        self.assertEqual(api.request(kid, {"url": "https://example.com/a.flac", "synced": lrc})[0], 200)
        meta = api.meta_for(os.path.join(api.LIBRARY, os.listdir(api.LIBRARY)[0]))
        self.assertEqual(meta["synced"], [[12.5, "first line"], [20.0, "chorus"], [30.0, ""], [65.25, "chorus"]])
        self.assertEqual(meta["lyrics"], "first line\nchorus\nchorus")
        self.assertEqual(api.lyrics_pending(), [])

    def test_lrc_pasted_as_lyrics_and_json_timings_count_too(self):
        self.assertEqual(api.timed_lyrics({"lyrics": "[00:01.00]a\n[00:02.00]b"}), [[1.0, "a"], [2.0, "b"]])
        self.assertEqual(api.timed_lyrics({"lyrics": "[Verse]\nplain words"}), [])
        self.assertEqual(api.timed_lyrics({"synced": [[3, "b"], [1.5, " a "]]}), [[1.5, "a"], [3.0, "b"]])
        status, out = api.request(self.key()["id"], {"url": "https://example.com/a.flac", "synced": [["x", 1]]})
        self.assertEqual(status, 400)

    def test_a_key_can_requeue_a_song_as_it_was(self):
        self.upload("old.flac")
        api.push({"url": "https://files.example.com/u/old.flac", "title": "oldie", "lyrics": "la"},
                 {"requested_by": "dave"})
        self.upload("dj.flac")
        api.push({"url": "https://files.example.com/u/dj.flac", "dj": True})
        kid = self.key("carol")["id"]
        self.sent.clear()
        for song in ("old", "old.flac"):
            status, out = api.request(kid, {"song": song, "title": "renamed"})
            self.assertEqual((status, out["queued"]), (200, "oldie"))
        meta = api.meta_for("/library/old.flac")
        self.assertEqual((meta["by"], meta["requested_by"], meta["lyrics"]), ("carol", "dave", "la"))
        self.assertIn("q.push " + os.path.join(api.LIBRARY, "old.flac"), self.sent)
        self.assertEqual(self.fetched, [])
        for song in ("dj", "nope", "../keys"):
            self.assertEqual(api.request(kid, {"song": song})[0], 404, song)
        self.assertEqual(api.list_keys()[0]["uses"], 2)

    def test_the_same_file_again_plays_the_stored_copy(self):
        kid = self.key("carol")["id"]
        first = api.request(kid, {"url": "https://example.com/a.flac", "title": "first"})
        again = api.request(kid, {"url": "https://other.example.com/copy.flac", "title": "copy"})
        self.assertEqual(len(os.listdir(api.LIBRARY)), 1)
        self.assertEqual((first[0], again[0], again[1]["queued"]), (200, 200, "first"))
        self.assertEqual(again[1]["duplicate_of"], os.path.splitext(os.listdir(api.LIBRARY)[0])[0])

    def test_the_same_upload_under_two_links_is_stored_once(self):
        self.upload("one.flac")
        self.upload("two.flac")  # same bytes
        api.push({"url": "https://files.example.com/u/one.flac", "title": "one"})
        api.push({"url": "https://files.example.com/u/two.flac"})
        self.assertEqual(os.listdir(api.LIBRARY), ["one.flac"])

    def test_songs_stored_before_fingerprints_get_one(self):
        self.upload("old.flac")
        api.push({"url": "https://files.example.com/u/old.flac"})
        path = os.path.join(api.META, "old.flac.json")
        meta = api.meta_for("/library/old.flac")
        del meta["sha256"]
        with open(path, "w") as f:
            json.dump(meta, f)
        api.fingerprint_library()
        self.assertEqual(len(api.meta_for("/library/old.flac")["sha256"]), 64)

    def test_a_request_can_name_who_asked(self):
        kid = self.key("botty")["id"]
        api.request(kid, {"url": "https://example.com/a.flac", "requested_by": "  dave\n  smith "})
        meta = api.meta_for(os.path.join(api.LIBRARY, os.listdir(api.LIBRARY)[0]))
        self.assertEqual((meta["requested_by"], meta["by"]), ("dave smith", "botty"))
        api.request(kid, {"song": os.listdir(api.LIBRARY)[0], "requested_by": "mallory"})  # requeue keeps it
        self.assertEqual(api.meta_for(os.path.join(api.LIBRARY, os.listdir(api.LIBRARY)[0]))["requested_by"],
                         "dave smith")

    def test_a_request_with_no_audio_is_refused(self):
        api.fetch = lambda url, *cap: b"<html>not a song</html>"
        status, out = api.request(self.key()["id"], {"url": "https://example.com/x.mp3"})
        self.assertEqual(status, 400)
        self.assertIn("isn't an audio file", out["error"])
        self.assertEqual(os.listdir(api.LIBRARY), [])

    def test_a_key_may_have_only_so_many_songs_waiting(self):
        api.KEY_WAITING_MAX = 1
        kid = self.key()["id"]
        self.assertEqual(api.request(kid, {"url": "https://example.com/a.flac"})[0], 200)
        self.pending = [os.path.join(api.LIBRARY, n) for n in os.listdir(api.LIBRARY)]
        status, out = api.request(kid, {"url": "https://example.com/b.flac"})
        self.assertEqual(status, 429)
        self.assertIn("waiting", out["error"])

    def test_requests_can_be_limited_to_addresses(self):
        self.assertTrue(api.request_allowed("198.51.100.7"))  # no list: open
        api.REQUEST_ALLOW = [api.ipaddress.ip_network("203.0.113.0/24")]
        self.assertTrue(api.request_allowed("203.0.113.9"))
        self.assertTrue(api.request_allowed("6.6.6.6, 203.0.113.9"))  # only the proxy's entry counts
        self.assertFalse(api.request_allowed("203.0.113.9, 198.51.100.7"))
        self.assertFalse(api.request_allowed(""))

    def test_zero_turns_a_key_limit_off(self):
        api.KEY_WAITING_MAX = api.KEY_DAILY_MAX = 0
        kid = self.key()["id"]
        for i in range(3):
            self.assertEqual(api.request(kid, {"url": f"https://example.com/{i}.flac"})[0], 200)
            self.pending = [os.path.join(api.LIBRARY, n) for n in os.listdir(api.LIBRARY)][:1]

    def test_private_addresses_are_refused(self):
        for host in ("127.0.0.1", "10.1.2.3", "192.168.1.10", "169.254.169.254", "localhost", "::1"):
            with self.assertRaises(api.FetchError, msg=host):
                api.public_address(host)

    def test_file_type_comes_from_the_bytes(self):
        self.assertEqual(api.audio_kind(FLAC), "flac")
        self.assertEqual(api.audio_kind(b"ID3\x04rest"), "mp3")
        self.assertEqual(api.audio_kind(b"\x00\x00\x00\x20ftypM4A "), "m4a")
        self.assertIsNone(api.audio_kind(b"<!doctype html>"))

    def test_a_waiting_worker_is_woken_by_a_new_track(self):
        self.upload("w.flac")
        got = []
        t = threading.Thread(target=lambda: got.append(api.lyrics_pending_wait(10)))
        t0 = api.time.monotonic()
        t.start()
        threading.Event().wait(0.3)
        api.push({"url": "https://files.example.com/u/w.flac"})
        t.join(5)
        self.assertEqual([x["file"] for x in got[0]], ["w.flac"])
        self.assertLess(api.time.monotonic() - t0, 3)

    def test_lyrics_worker_round_trip(self):
        self.upload("s.flac")
        api.push({"url": "https://files.example.com/u/s.flac", "lyrics": "la la"})
        self.upload("i.flac")
        api.push({"url": "https://files.example.com/u/i.flac"})
        todo = {t["file"]: t for t in api.lyrics_pending()}
        self.assertEqual(todo["s.flac"]["lyrics"], "la la")
        api.save_lyrics({"file": "s.flac", "lines": [[1.5, "la la"]]})
        api.save_lyrics({"file": "i.flac", "instrumental": True})
        self.assertEqual(api.lyrics_pending(), [])
        self.assertEqual(api.meta_for("/library/s.flac")["synced"], [[1.5, "la la"]])
        self.assertEqual(api.save_lyrics({"file": "../keys.json", "lines": []})[0], 404)


class DjTest(ApiCase):
    def setUp(self):
        super().setUp()
        self.caps = []
        api.fetch = lambda url, cap=None: self.caps.append(cap) or FLAC
        self.kid = api.create_key("botty")[1]["id"]

    def test_an_interlude_plays_whole_and_is_gone_once_played(self):
        status, out = api.request(self.kid, {"url": "https://example.com/hi.mp3", "dj": True})
        self.assertEqual((status, out["queued"]), (200, "botty (dj)"))
        self.assertEqual(self.caps, [api.DJ_FETCH_MAX_MB])
        name = os.listdir(api.LIBRARY)[0]
        self.assertIn('q.push annotate:liq_cross_duration="0.":' + os.path.join(api.LIBRARY, name), self.sent)
        self.pending = [os.path.join(api.LIBRARY, name)]
        api.prune(time.time() + 600)
        self.assertEqual(os.listdir(api.LIBRARY), [name])  # still queued
        self.pending = []
        api.prune(time.time() + 60)
        self.assertEqual(os.listdir(api.LIBRARY), [name])  # just queued: grace period
        api.prune(time.time() + 600)
        self.assertEqual(os.listdir(api.LIBRARY), [])
        self.assertEqual(os.listdir(api.META), [])

    @unittest.skipUnless(shutil.which("ffmpeg"), "needs ffmpeg")
    def test_an_interlude_is_levelled_with_the_songs(self):
        quiet = os.path.join(api.UPLOADS, "quiet.wav")
        subprocess.run(["ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=300:duration=6",
                        "-af", "volume=0.02", quiet], check=True)
        api.push({"url": "https://files.example.com/u/quiet.wav", "dj": True})
        out = subprocess.run(["ffmpeg", "-nostats", "-i", os.path.join(api.LIBRARY, "quiet.wav"),
                              "-af", "ebur128", "-f", "null", "-"], capture_output=True, text=True).stderr
        lufs = float(out.rsplit("I:", 1)[1].split("LUFS")[0])
        self.assertAlmostEqual(lufs, api.DJ_LUFS, delta=1.5)
        self.assertTrue(api.meta_for("/library/quiet.wav")["levelled"])

    def test_an_interlude_is_never_reused(self):
        api.request(self.kid, {"url": "https://example.com/hi.mp3", "dj": True})
        name = os.listdir(api.LIBRARY)[0]
        self.assertEqual(api.request(self.kid, {"song": name})[0], 404)
        self.assertEqual(api.request(self.kid, {"song": name, "dj": True})[0], 400)
        api.request(self.kid, {"url": "https://example.com/again.mp3", "dj": True})  # same bytes: a new clip
        self.assertEqual(len(os.listdir(api.LIBRARY)), 2)
        self.assertEqual(api.library("botty"), [t for t in api.library("botty") if t["dj"]])

    def test_songs_are_not_touched_by_the_interlude_cleanup(self):
        api.request(self.kid, {"url": "https://example.com/song.flac"})
        api.prune(time.time() + 600)
        self.assertEqual(len(os.listdir(api.LIBRARY)), 1)


class HoldTest(ApiCase):
    def setUp(self):
        super().setUp()
        api.fetch = lambda url, *cap: FLAC
        api.QUEUE_MAX = 5

    def key(self):
        return api.create_key("alice")[1]

    def pushed(self):
        return [c for c in self.sent if c.startswith("q.push")]

    def release(self):
        api.release_ready()

    def test_the_first_song_waits_for_its_lyrics_and_later_ones_wait_behind_it(self):
        kid = self.key()["id"]
        api.fetch = lambda url, *cap: FLAC + url.encode()  # different files
        status, out = api.request(kid, {"url": "https://example.com/first.flac"})
        self.assertEqual((status, out["waiting_for_lyrics"]), (200, api.LYRICS_WAIT))
        api.request(kid, {"url": "https://example.com/second.flac", "synced": "[00:01.00]a\n[00:02.00]b"})
        self.assertEqual(self.pushed(), [])  # second has lyrics, but waits its turn
        first = api.HELD[0][0]
        self.assertEqual(api.lyrics_pending()[0]["file"], first)  # the worker does the held one first
        api.save_lyrics({"file": first, "lines": [[1.0, "la"]]})
        self.release()
        second = next(n for n in os.listdir(api.LIBRARY) if n != first)
        self.assertEqual([c.rsplit("/", 1)[-1] for c in self.pushed()], [first, second])
        self.assertNotIn("held_until", api.meta_for(os.path.join(api.LIBRARY, first)))

    def test_a_song_with_others_ahead_goes_straight_in(self):
        self.pending = ["/library/someone-else.flac"]
        api.request(self.key()["id"], {"url": "https://example.com/x.flac"})
        self.assertEqual(len(self.pushed()), 1)
        self.assertEqual(api.HELD, [])

    def test_the_wait_has_a_limit(self):
        api.LYRICS_WAIT = -1  # already over
        api.request(self.key()["id"], {"url": "https://example.com/x.flac"})
        self.assertEqual(len(api.HELD), 1)
        self.release()
        self.assertEqual(len(self.pushed()), 1)

    def test_a_player_restart_mid_release_keeps_everything_held(self):
        api.LYRICS_WAIT = -1
        kid = self.key()["id"]
        api.fetch = lambda url, *cap: FLAC + url.encode()
        for n in ("a", "b", "c"):
            api.request(kid, {"url": f"https://example.com/{n}.flac"})
        held = list(api.HELD)
        api.telnet = lambda cmd: (_ for _ in ()).throw(OSError("restarting"))
        self.release()
        self.assertEqual(api.HELD, held)

    def test_holds_survive_a_restart(self):
        api.request(self.key()["id"], {"url": "https://example.com/x.flac"})
        held = list(api.HELD)
        api.HELD.clear()
        api.resume_holds()
        self.assertEqual(api.HELD, held)


class EventsTest(ApiCase):
    def setUp(self):
        super().setUp()
        api.listeners = lambda: 3
        self.server = api.ThreadingHTTPServer(("127.0.0.1", 0), api.Handler)
        self.server.daemon_threads = True
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

    def read_events(self, stream, n):
        out = []
        while len(out) < n:
            event = data = None
            while (line := stream.readline().decode().rstrip("\n")) or event is None:
                if line.startswith("event: "):
                    event = line[7:]
                elif line.startswith("data: "):
                    data = json.loads(line[6:])
            out.append((event, data))
        return out

    def test_a_page_gets_a_snapshot_then_what_is_broadcast(self):
        self.upload("a.flac")
        api.push({"url": "https://files.example.com/u/a.flac", "title": "first"})
        self.playing = os.path.join(api.LIBRARY, "a.flac")
        self.pending = [os.path.join(api.LIBRARY, "a.flac")]
        url = f"http://127.0.0.1:{self.server.server_address[1]}/events"
        with urllib.request.urlopen(url, timeout=5) as stream:
            self.assertEqual(stream.headers["Content-Type"], "text/event-stream")
            got = dict(self.read_events(stream, 2))
            self.assertEqual((got["now"]["title"], got["now"]["listeners"]), ("first", 3))
            self.assertEqual(got["queue"]["files"], ["a.flac"])
            api.broadcast(api.sse("queue", {"queue": [], "files": []}))
            self.assertEqual(self.read_events(stream, 1), [("queue", {"queue": [], "files": []})])
        for _ in range(50):  # the handler notices the page left on its next write
            api.broadcast(b": x\n\n")
            with api.subscribers_lock:
                if not api.subscribers:
                    break
            threading.Event().wait(0.05)
        self.assertFalse(api.subscribers)

    def test_a_full_page_drops_messages_instead_of_growing(self):
        q = api.Queue(maxsize=2)
        with api.subscribers_lock:
            api.subscribers.add(q)
        self.addCleanup(api.subscribers.discard, q)
        for i in range(5):
            api.broadcast(api.sse("queue", {"queue": [str(i)]}))
        self.assertEqual(q.qsize(), 2)

    def get(self, path):
        url = f"http://127.0.0.1:{self.server.server_address[1]}{path}"
        try:
            with urllib.request.urlopen(url, timeout=5) as r:
                return r.status, json.load(r)
        except urllib.error.HTTPError as e:
            return e.code, json.load(e)

    def test_a_track_is_described_by_its_file_name(self):
        self.upload("a.flac")
        api.push({"url": "https://files.example.com/u/a.flac", "title": "first", "lyrics": "la"})
        status, meta = self.get("/meta/a.flac")
        self.assertEqual((status, meta["title"], meta["lyrics"]), (200, "first", "la"))
        self.assertEqual(self.get("/meta/gone.flac")[0], 404)
        self.assertEqual(self.get("/meta/..%2Fkeys.json")[0], 404)


class SecondsTest(ApiCase):
    def test_a_flac_is_measured_from_its_header_once(self):
        path = os.path.join(api.LIBRARY, "a.flac")
        info = bytearray(34)
        info[10:13] = bytes([0x0A, 0xC4, 0x40])  # 44100 Hz, in the top 20 bits
        info[14:18] = (44100 * 90).to_bytes(4, "big")  # 90 s of samples
        with open(path, "wb") as f:
            f.write(b"fLaC" + bytes(4) + bytes(info))
        self.assertEqual(api.seconds(path), 90.0)
        api.flac_seconds = lambda p: self.fail("measured the same file twice")
        self.assertEqual(api.seconds(path), 90.0)
        self.assertIsNone(api.seconds(os.path.join(api.LIBRARY, "gone.flac")))


class ListeningTest(ApiCase):
    def test_heartbeats_count_until_they_stop(self):
        api.heard_from("a" * 16, now=100)
        api.heard_from("b" * 16, now=110)
        api.heard_from("a" * 16, now=120)  # the same page again is still one listener
        self.assertEqual(api.hls_listeners(now=125), 2)
        self.assertEqual(api.hls_listeners(now=145), 1)  # b stopped 35 s ago
        self.assertEqual(api.hls_listeners(now=200), 0)


if __name__ == "__main__":
    unittest.main()
