#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""The station's curator: indexes a music library, keeps Liquidsoap a few tracks ahead, and reports what
is playing to the relay that serves the listening page.

Picks follow simple rules: a track doesn't come back within TRACK_COOLDOWN_HOURS, an artist not within
the last ARTIST_COOLDOWN tracks, an album not within the last ALBUM_COOLDOWN; among what's left, tracks
that haven't played for longest are likeliest.

HTTP (inside the compose network only):
  POST /track {"filename"}   Liquidsoap, when a track starts
  POST /skip                 skip the current track
  GET  /state                what the relay gets

Settings (environment):
  MUSIC_DIR (/music), DATA_DIR (/data), CACHE_DIR (empty = play from MUSIC_DIR; else a RAM disk shared
  with Liquidsoap, which each queued track is copied to first, so slow disks can't stall playback), LIQ_HOST (liquidsoap), LIQ_PORT (1234), QUEUE_AHEAD (2),
  TRACK_COOLDOWN_HOURS (72), ARTIST_COOLDOWN (8), ALBUM_COOLDOWN (25), RESCAN_HOURS (6),
  RELAY_URL (e.g. http://10.0.0.1:8102; empty = don't report), RELAY_SECRET
"""

import json
import os
import random
import re
import shutil
import socket
import sqlite3
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import mutagen

MUSIC = os.environ.get("MUSIC_DIR", "/music")
DATA = os.environ.get("DATA_DIR", "/data")
CACHE = os.environ.get("CACHE_DIR", "").rstrip("/")
LIQ = (os.environ.get("LIQ_HOST", "liquidsoap"), int(os.environ.get("LIQ_PORT", "1234")))
AHEAD = int(os.environ.get("QUEUE_AHEAD", "2"))
TRACK_COOLDOWN = float(os.environ.get("TRACK_COOLDOWN_HOURS", "72")) * 3600
ARTIST_COOLDOWN = int(os.environ.get("ARTIST_COOLDOWN", "8"))
ALBUM_COOLDOWN = int(os.environ.get("ALBUM_COOLDOWN", "25"))
RESCAN = float(os.environ.get("RESCAN_HOURS", "6")) * 3600
RELAY = os.environ.get("RELAY_URL", "").rstrip("/")
RELAY_SECRET = os.environ.get("RELAY_SECRET", "")
AUDIO = (".flac", ".mp3", ".m4a", ".ogg", ".opus")

LOCK = threading.Lock()  # one writer at a time on the database
FILLING = threading.Lock()  # one fill at a time: the loop and a track start both call it


def db():
    conn = sqlite3.connect(os.path.join(DATA, "station.db"), timeout=30)
    conn.row_factory = sqlite3.Row
    return conn


def setup():
    os.makedirs(DATA, exist_ok=True)
    with db() as c:
        c.executescript("""
            CREATE TABLE IF NOT EXISTS tracks (path TEXT PRIMARY KEY, mtime REAL, title TEXT, artist TEXT,
                album TEXT, albumartist TEXT, year INTEGER, seconds REAL, lyrics TEXT);
            CREATE TABLE IF NOT EXISTS plays (id INTEGER PRIMARY KEY, path TEXT, started REAL);
            CREATE TABLE IF NOT EXISTS queued (path TEXT PRIMARY KEY, at REAL);
            CREATE INDEX IF NOT EXISTS plays_started ON plays(started);
        """)


def first(tags, *keys):
    for k in keys:
        v = tags.get(k)
        if v:
            v = v[0] if isinstance(v, list) else v
            return str(v).strip()
    return ""


def read_tags(path):
    f = mutagen.File(path, easy=True)
    if f is None:
        return None
    tags = f.tags or {}
    year = first(tags, "originaldate", "date", "year")[:4]
    return {"title": first(tags, "title") or os.path.splitext(os.path.basename(path))[0],
            "artist": first(tags, "artist", "albumartist"), "album": first(tags, "album"),
            "albumartist": first(tags, "albumartist", "artist"), "year": int(year) if year.isdigit() else None,
            "seconds": round(getattr(f.info, "length", 0) or 0, 1), "lyrics": lyrics_of(path)}


def lyrics_of(path):
    """The lyrics tag, whichever format's name it goes by; a name a format doesn't allow (a FLAC
    comment can't be called USLT::eng) just doesn't match."""
    raw = mutagen.File(path)  # lyrics aren't in the "easy" key set
    if raw is None or raw.tags is None:
        return ""
    for key in ("LYRICS", "lyrics", "UNSYNCEDLYRICS", "USLT::eng", "\xa9lyr"):
        try:
            v = raw.tags[key]
        except (KeyError, ValueError):
            continue
        return str(v[0] if isinstance(v, list) else v)
    return ""


def scan():
    """Bring the index up to date: new and changed files read, vanished ones dropped."""
    seen, added = set(), 0
    with db() as c:
        known = {r["path"]: r["mtime"] for r in c.execute("SELECT path, mtime FROM tracks")}
    for root, _, files in os.walk(MUSIC):
        for name in files:
            if not name.lower().endswith(AUDIO) or name.startswith("."):
                continue
            path = os.path.join(root, name)
            seen.add(path)
            try:
                mtime = os.path.getmtime(path)
                if known.get(path) == mtime:
                    continue
                t = read_tags(path)
            except Exception as e:  # one unreadable file mustn't stop the scan
                print("skipped", path, repr(e)[:120], flush=True)
                continue
            if t:
                with LOCK, db() as c:
                    c.execute("INSERT OR REPLACE INTO tracks VALUES (?,?,?,?,?,?,?,?,?)",
                              (path, mtime, t["title"], t["artist"], t["album"], t["albumartist"], t["year"],
                               t["seconds"], t["lyrics"]))
                added += 1
    gone = set(known) - seen
    with LOCK, db() as c:
        c.executemany("DELETE FROM tracks WHERE path = ?", [(p,) for p in gone])
        total = c.execute("SELECT COUNT(*) FROM tracks").fetchone()[0]
    print(f"scan: {total} tracks ({added} new or changed, {len(gone)} gone)", flush=True)


def telnet(command):
    with socket.create_connection(LIQ, timeout=5) as s:
        s.sendall(command.encode() + b"\nquit\n")
        out = b""
        while chunk := s.recv(4096):
            out += chunk
    return "\n".join(l for l in out.decode(errors="replace").splitlines() if l not in ("END", "Bye!")).strip()


def pick():
    """The next track by the cooldown rules; None if the library is empty."""
    now = time.time()
    with db() as c:
        recent = c.execute("SELECT p.path, t.artist, t.albumartist, t.album FROM plays p JOIN tracks t USING (path) "
                           "ORDER BY p.started DESC LIMIT ?", (max(ARTIST_COOLDOWN, ALBUM_COOLDOWN),)).fetchall()
        queued = c.execute("SELECT q.path, t.artist, t.albumartist, t.album FROM queued q JOIN tracks t USING (path)").fetchall()
        lately = list(queued) + list(recent)
        artists = {(r["artist"] or "").lower() for r in lately[:ARTIST_COOLDOWN]} - {""}
        albums = {((r["albumartist"] or "").lower(), (r["album"] or "").lower()) for r in lately[:ALBUM_COOLDOWN]}
        blocked = {r["path"] for r in queued} | {r["path"] for r in c.execute(
            "SELECT DISTINCT path FROM plays WHERE started > ?", (now - TRACK_COOLDOWN,))}
        last = dict(c.execute("SELECT path, MAX(started) FROM plays GROUP BY path").fetchall())
        rows = c.execute("SELECT path, artist, albumartist, album FROM tracks").fetchall()
    def allowed(r, strict):
        if r["path"] in blocked:
            return False
        if not strict:
            return True
        return ((r["artist"] or "").lower() not in artists
                and ((r["albumartist"] or "").lower(), (r["album"] or "").lower()) not in albums)
    for strict in (True, False):  # a small library may not satisfy every rule; relax rather than stop
        pool = [r for r in rows if allowed(r, strict)]
        if pool:
            # Never played counts as played long ago; weight grows with time since the last play.
            weights = [min(now - last.get(r["path"], 0), 90 * 86400) + 3600 for r in pool]
            return random.choices(pool, weights)[0]["path"]
    return rows[0]["path"] if rows else None


def cached(path, rowid):
    """The copy Liquidsoap should play: in CACHE if there is one (written fully before it's named)."""
    if not CACHE:
        return path
    dest = os.path.join(CACHE, f"{rowid}{os.path.splitext(path)[1]}")
    if not os.path.exists(dest):
        shutil.copyfile(path, dest + ".part")
        os.replace(dest + ".part", dest)
    return dest


def uncache():
    """Drop cached copies of tracks neither queued nor among the last two to start (one may still be
    fading out)."""
    if not CACHE:
        return
    with db() as c:
        keep = {str(r[0]) for r in c.execute(
            "SELECT t.rowid FROM tracks t WHERE t.path IN (SELECT path FROM queued) "
            "OR t.path IN (SELECT path FROM plays ORDER BY started DESC LIMIT 2)")}
    for name in os.listdir(CACHE):
        if name.split(".")[0] not in keep:
            os.remove(os.path.join(CACHE, name))


def fill():
    """Keep Liquidsoap AHEAD tracks ahead."""
    with FILLING:
        _fill()
        uncache()


def _fill():
    try:
        waiting = len([x for x in telnet("q.queue").split() if x])
    except OSError:
        return  # Liquidsoap is restarting
    with LOCK, db() as c:
        # Liquidsoap forgets its queue when it restarts; forget what it no longer holds, oldest first
        # (a track leaves this table when it starts playing).
        stale = c.execute("SELECT COUNT(*) FROM queued").fetchone()[0] - waiting
        if stale > 0:
            c.execute("DELETE FROM queued WHERE path IN (SELECT path FROM queued ORDER BY at LIMIT ?)", (stale,))
    for _ in range(max(0, AHEAD - waiting)):
        path = pick()
        if not path:
            return
        with db() as c:
            rowid = c.execute("SELECT rowid FROM tracks WHERE path = ?", (path,)).fetchone()[0]
        try:
            playable = cached(path, rowid)
        except OSError as e:
            print("not cached:", path, repr(e)[:120], flush=True)
            continue
        # The edge stamps the stream with this id, which the page looks the track up by.
        telnet(f'q.push annotate:jpop_id="{rowid}":{playable}')
        with LOCK, db() as c:
            c.execute("INSERT OR REPLACE INTO queued VALUES (?, ?)", (path, time.time()))


LRC_TIME = re.compile(r"\[(\d+):(\d+(?:[.:]\d+)?)\]")
LRC_TAG = re.compile(r"^\[[a-z]+:.*\]$", re.I)


def parse_lyrics(text):
    """(plain text, [[seconds, line], ...]) from a lyrics tag; the list is empty unless it is LRC."""
    offset = re.search(r"^\[offset:\s*([+-]?\d+)\]", text or "", re.I | re.M)
    shift = -int(offset.group(1)) / 1000 if offset else 0  # a positive LRC offset means earlier
    plain, synced = [], []
    for raw in (text or "").splitlines():
        stamps = LRC_TIME.findall(raw)
        line = LRC_TIME.sub("", raw).strip()
        if not stamps and LRC_TAG.match(raw.strip()):
            continue
        plain.append(line)
        for m, sec in stamps:
            synced.append([round(max(0, int(m) * 60 + float(sec.replace(":", ".")) + shift), 2), line])
    synced.sort(key=lambda x: x[0])
    return "\n".join(plain).strip(), synced


def track_info(path, started=None, words=False):
    with db() as c:
        r = c.execute("SELECT rowid, * FROM tracks WHERE path = ?", (path,)).fetchone()
    if not r:
        return {"title": os.path.basename(path)}
    out = {"id": str(r["rowid"]), **{k: r[k] for k in ("title", "artist", "album", "year", "seconds")}}
    if started:
        out["started"] = started
    if words:
        out["lyrics"], out["synced"] = parse_lyrics(r["lyrics"])
    return out


def state():
    """What's on and around it. The page plays some seconds behind, so the previous track carries its
    lyrics too."""
    with db() as c:
        plays = c.execute("SELECT path, started FROM plays ORDER BY started DESC LIMIT 11").fetchall()
        queued = c.execute("SELECT path FROM queued ORDER BY at").fetchall()
        total = c.execute("SELECT COUNT(*) FROM tracks").fetchone()[0]
    recent = [track_info(r["path"], r["started"], words=i < 2) for i, r in enumerate(plays)]
    return {"now": recent[0] if recent else None, "next": [track_info(r["path"]) for r in queued],
            "history": recent[1:], "library": total, "server_time": time.time()}


def publish():
    if not RELAY:
        return
    req = urllib.request.Request(RELAY + "/state", data=json.dumps(state()).encode(), method="POST",
                                 headers={"Content-Type": "application/json", "Authorization": "Bearer " + RELAY_SECRET})
    try:
        urllib.request.urlopen(req, timeout=10).close()
    except OSError as e:
        print("relay unreachable:", e, flush=True)


def started(filename):
    if CACHE and filename.startswith(CACHE + "/"):
        with db() as c:
            row = c.execute("SELECT path FROM tracks WHERE rowid = ?", (os.path.basename(filename).split(".")[0],)).fetchone()
        path = row[0] if row else filename
    else:
        path = filename if filename.startswith(MUSIC) else os.path.join(MUSIC, filename)
    with LOCK, db() as c:
        c.execute("INSERT INTO plays (path, started) VALUES (?, ?)", (path, time.time()))
        c.execute("DELETE FROM queued WHERE path = ?", (path,))
    print("playing", path[len(MUSIC):], flush=True)
    # Liquidsoap waits for this answer, so the slow part (copying the next track) runs apart from it.
    threading.Thread(target=lambda: (fill(), publish()), daemon=True).start()


class Handler(BaseHTTPRequestHandler):
    def reply(self, status, obj):
        data = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/state":
            return self.reply(200, state())
        self.reply(404, {"error": "not found"})

    def do_POST(self):
        body = json.loads(self.rfile.read(min(int(self.headers.get("Content-Length") or 0), 65536)) or b"{}")
        if self.path == "/track" and body.get("filename"):
            started(body["filename"])
            return self.reply(200, {"ok": True})
        if self.path == "/skip":
            telnet("skip")
            return self.reply(200, {"skipped": True})
        self.reply(404, {"error": "not found"})

    def log_message(self, *args):
        pass


def loop():
    next_scan = 0
    while True:
        if time.time() >= next_scan:
            scan()
            next_scan = time.time() + RESCAN
        fill()
        time.sleep(5)


if __name__ == "__main__":
    setup()
    with LOCK, db() as c:  # a restart starts with an empty Liquidsoap queue
        c.execute("DELETE FROM queued")
    threading.Thread(target=loop, daemon=True).start()
    threading.Thread(target=lambda: [publish() or time.sleep(30) for _ in iter(int, 1)], daemon=True).start()
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
