#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Radio API: queue a hosted audio file, skip, and report what is playing.

POST /push {"url", "title", "lyrics", "by", "dj"}  (Bearer RADIO_TOKEN; "dj": true for a spoken clip)
POST /skip                                   (Bearer RADIO_TOKEN)
POST /remove {"position"}, /swap {"a", "b"}   (Bearer RADIO_TOKEN; queue positions from 0)
GET  /now, /queue, /library                  (public; /library?q=words matches title, requester and lyrics)
GET  /meta/<file>                            (public; one track's details: the page shows the track it hears,
                                              which can lag what /now says is on air)
GET  /listening?id=<random>                  (public; the page's heartbeat while it plays the HLS stream, so
                                              those listeners are counted with Icecast's)
GET  /events                                 (public; Server-Sent Events: "now" and "queue" when they change;
                                              the listening page's one connection)
GET  /downloaded                             (the proxy's check before serving /dl/<file>; counts it)

POST /request {"url", "title", "lyrics", "synced", "requested_by"}  (Bearer <API key>; any public audio link, fetched
     here; "synced" is timed lyrics as LRC text or [[seconds, line], ...], and LRC in "lyrics" counts too)
     or {"song"}: a song already on the radio, by its id from /library, queued again as it was
     or {"url", "dj": true}: a spoken DJ interlude, deleted once it has played
GET  /keys, POST /keys {"name"}, POST /keys/revoke {"id"}          (Bearer RADIO_TOKEN; API keys)
GET  /lyrics/pending[?wait=55], GET /track/<file>, POST /lyrics {"file", ...}  (Bearer RADIO_TOKEN; lyrics
     worker; with wait, an empty answer is held until a song needs lyrics or that many seconds pass)

A pushed URL must start with RADIO_SOURCE_PREFIX; its file is read from /uploads (the host's upload
directory, where file names equal URL names). Only /request fetches from the network, and only from
public addresses (checked on every redirect, and the connection pinned to the checked address).
"""

import hashlib
import hmac
import http.client
import ipaddress
import json
import os
import re
import secrets
import shutil
import socket
import ssl
import subprocess
import threading
import time
import urllib.parse
import urllib.request
from queue import Empty, Full, Queue
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

TOKEN = os.environ["RADIO_TOKEN"]
PREFIX = os.environ["RADIO_SOURCE_PREFIX"]
QUEUE_MAX = int(os.environ.get("RADIO_QUEUE_MAX", "15"))
LIBRARY_MAX = int(os.environ.get("RADIO_LIBRARY_MAX", "300"))
# A queued file is kept this long after it was last queued, so it can be queued again after the file
# host has expired it, and the library as a whole stays under LIBRARY_MAX_MB.
RETENTION = float(os.environ.get("RADIO_RETENTION_DAYS", "7")) * 86400
LIBRARY_MAX_MB = float(os.environ.get("RADIO_LIBRARY_MAX_MB", "5000"))
UPLOADS, LIBRARY, META = "/uploads", "/library", "/meta"
AUDIO = re.compile(r"^[A-Za-z0-9_-]{1,64}\.(mp3|ogg|oga|opus|flac|wav|m4a)$")
MAX_TEXT = {"title": 120, "lyrics": 6000, "by": 40}
META_LOCK = threading.Lock()  # meta files are read, changed and rewritten by push and download counts
NEW_TRACK = threading.Condition()  # wakes a lyrics worker waiting on /lyrics/pending
# A song requested through the API that would play first, but has no timed lyrics yet, is held back until
# the lyrics worker is done with it, or this many seconds; whatever is requested meanwhile waits behind it.
LYRICS_WAIT = float(os.environ.get("RADIO_LYRICS_WAIT", "120"))
HELD = []  # [library name, play uri], first held first
HOLD = threading.Condition()
LISTENING = {}  # heartbeat id -> when last heard, for HLS listeners
LISTENING_LOCK = threading.Lock()
LISTENING_WINDOW = 30  # the page beats every 10 s
KEYS = "/keys/keys.json"
KEYS_LOCK = threading.Lock()
# Per key: tracks it may have queued at once, and songs it may queue in a day; 0 is no limit.
KEY_WAITING_MAX = int(os.environ.get("RADIO_KEY_WAITING_MAX", "3"))
KEY_DAILY_MAX = int(os.environ.get("RADIO_KEY_DAILY_MAX", "20"))
FETCH_MAX_MB = float(os.environ.get("RADIO_FETCH_MAX_MB", "100"))
FETCH_SECONDS = 90
DJ_FETCH_MAX_MB = float(os.environ.get("RADIO_DJ_MAX_MB", "10"))  # interludes are a few seconds of speech
DJ_LUFS = float(os.environ.get("RADIO_DJ_LUFS", "-14"))  # songs master near -14 LUFS; speech comes in far quieter
# Addresses or ranges (comma-separated) that may use /request; empty lets anyone with a key in.
REQUEST_ALLOW = [ipaddress.ip_network(n.strip(), strict=False)
                 for n in os.environ.get("RADIO_REQUEST_ALLOW", "").split(",") if n.strip()]
EVENTS_MAX = int(os.environ.get("RADIO_EVENTS_MAX", "200"))  # each listening page holds a thread


def telnet(command):
    with socket.create_connection(("liquidsoap", 1234), timeout=5) as s:
        s.sendall(command.encode() + b"\nquit\n")
        out = b""
        while chunk := s.recv(4096):
            out += chunk
    lines = out.decode(errors="replace").splitlines()
    return "\n".join(l for l in lines if l not in ("END", "Bye!")).strip()


def flac_tags(path):
    """A FLAC file's Vorbis comments, keys upper-cased, or {}."""
    tags = {}
    try:
        with open(path, "rb") as f:
            if f.read(4) != b"fLaC":
                return tags
            while True:
                header = f.read(4)
                if len(header) < 4:
                    return tags
                length = int.from_bytes(header[1:], "big")
                if header[0] & 0x7F != 4:
                    f.seek(length, 1)
                elif body := f.read(length):
                    i = 4 + int.from_bytes(body[:4], "little")
                    count, i = int.from_bytes(body[i:i + 4], "little"), i + 4
                    for _ in range(count):
                        n = int.from_bytes(body[i:i + 4], "little")
                        key, _, value = body[i + 4:i + 4 + n].decode(errors="replace").partition("=")
                        tags[key.upper()] = value
                        i += 4 + n
                if header[0] & 0x80:
                    return tags
    except OSError:
        return tags


LRC_STAMP = re.compile(r"\[(\d+):(\d+(?:\.\d+)?)\]")
LRC_WORD = re.compile(r"<\d+:\d+(?:\.\d+)?>")


def parse_lrc(text):
    """[[seconds, line], ...] from LRC text, in time order. A line may carry several times (a repeated
    chorus); word-level <mm:ss> tags are dropped, and so are metadata tags such as [ar:...]."""
    out = []
    for line in text.splitlines():
        line, stamps = line.strip(), []
        while m := LRC_STAMP.match(line):
            stamps.append(round(int(m[1]) * 60 + float(m[2]), 2))
            line = line[m.end():].strip()
        words = " ".join(LRC_WORD.sub("", line).split())[:200]
        out += [[t, words] for t in stamps]
    return sorted(out, key=lambda x: x[0])[:400]


def timed_lyrics(body):
    """Timed lyrics a caller sent: "synced" as LRC or [[seconds, line], ...], or LRC pasted into "lyrics"."""
    given = body.get("synced")
    if isinstance(given, list):
        try:
            return sorted([[round(float(t), 2), " ".join(str(l).split())[:200]] for t, l in given[:400]
                           if float(t) >= 0], key=lambda x: x[0])
        except (TypeError, ValueError):
            return []
    if isinstance(given, str):
        return parse_lrc(given)
    lyrics = str(body.get("lyrics") or "")
    stamped = sum(1 for l in lyrics.splitlines() if LRC_STAMP.match(l.strip()))
    return parse_lrc(lyrics) if stamped >= 2 else []


def pending():
    """Library paths of the queued tracks, soonest first (annotations stripped from their URIs)."""
    return [uri.rsplit(":", 1)[-1] for uri in telnet("pending").splitlines() if uri]


def meta_for(path):
    name = os.path.basename(path)
    if not name:
        return {}
    try:
        with open(os.path.join(META, name + ".json")) as f:
            return json.load(f)
    except (OSError, ValueError):
        return {"title": os.path.splitext(name)[0]} if name else {}


def listeners():
    """Icecast's listeners plus the page's HLS listeners; None if Icecast can't be asked."""
    try:
        with urllib.request.urlopen("http://icecast:8000/status-json.xsl", timeout=3) as r:
            src = json.load(r)["icestats"].get("source")
        src = src[0] if isinstance(src, list) else src
        icecast = int(src.get("listeners", 0)) if src else 0
    except Exception:
        return None
    return icecast + hls_listeners()


def heard_from(beat_id, now=None):
    now = now or time.time()
    with LISTENING_LOCK:
        LISTENING[beat_id] = now
        # Ids are the page's own random choice; old ones are dropped so they can't pile up.
        for k in [k for k, t in LISTENING.items() if t < now - LISTENING_WINDOW]:
            del LISTENING[k]
        if len(LISTENING) > 1000:
            LISTENING.clear()


def hls_listeners(now=None):
    now = now or time.time()
    with LISTENING_LOCK:
        return sum(1 for t in LISTENING.values() if t >= now - LISTENING_WINDOW)


def remove_track(path):
    for p in (path, os.path.join(META, os.path.basename(path) + ".json")):
        try:
            os.remove(p)
        except OSError:
            pass


def prune(now=None):
    """Delete DJ interludes that have played, library files past retention, then the oldest until the count
    and size caps hold. Files queued, held or playing are kept whatever their age."""
    now = now or time.time()
    try:
        keep = set(pending()) | {telnet("now").partition("\n")[0]} | {os.path.join(LIBRARY, h[0]) for h in HELD}
    except OSError:
        return  # without the player's view, nothing is known to be safe to delete
    for f in os.listdir(LIBRARY):
        # An interlude is never played twice. The grace period covers a clip between being stored and queued.
        meta = meta_for(os.path.join(LIBRARY, f))
        if meta.get("dj") and os.path.join(LIBRARY, f) not in keep and now - meta.get("added", 0) > 120:
            remove_track(os.path.join(LIBRARY, f))
    files = sorted((os.path.join(LIBRARY, f) for f in os.listdir(LIBRARY)), key=os.path.getmtime)
    old = [f for f in files if f not in keep and now - os.path.getmtime(f) > RETENTION]
    for f in old:
        remove_track(f)
    files = [f for f in files if f not in old]
    count, total = len(files), sum(os.path.getsize(f) for f in files)
    for f in [f for f in files if f not in keep]:
        if count <= LIBRARY_MAX and total <= LIBRARY_MAX_MB * 1024 * 1024:
            break
        count, total = count - 1, total - os.path.getsize(f)
        remove_track(f)
    for m in os.listdir(META):  # metadata whose track is gone
        if not os.path.exists(os.path.join(LIBRARY, m[:-len(".json")])):
            remove_track(os.path.join(LIBRARY, m[:-len(".json")]))


def prune_often():
    while True:
        time.sleep(60)
        try:
            prune()
        except OSError as e:
            print("prune failed:", e, flush=True)


def flac_seconds(path):
    try:
        with open(path, "rb") as f:
            head = f.read(42)
    except OSError:
        return None
    if head[:4] != b"fLaC":
        return None
    info = head[8:42]
    rate = (info[10] << 12) | (info[11] << 4) | (info[12] >> 4)
    samples = ((info[13] & 0x0F) << 32) | int.from_bytes(info[14:18], "big")
    return round(samples / rate, 1) if rate else None


SECONDS = {}  # (path, mtime) -> track length; ffprobe is slow enough to be worth remembering


def seconds(path):
    """A track's length: from the FLAC header when it is one, else from ffprobe."""
    try:
        key = (path, os.path.getmtime(path))
    except OSError:
        return None
    if key not in SECONDS:
        length = flac_seconds(path)
        if length is None:
            try:
                out = subprocess.run(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path],
                                     capture_output=True, text=True, timeout=20).stdout.strip()
                length = round(float(out), 1)
            except (OSError, ValueError, subprocess.TimeoutExpired):
                length = None
        SECONDS[key] = length
    return SECONDS[key]


def count_download(uri, range_header):
    """Count a download of /dl/<file>. A resumed or partial fetch (a Range not starting at 0) is part of
    a download already counted."""
    name = os.path.basename(uri.split("?", 1)[0])
    path = os.path.join(LIBRARY, name)
    if not AUDIO.match(name) or not os.path.isfile(path):
        return
    if range_header and not re.match(r"bytes=0-", range_header.strip()):
        return
    with META_LOCK:
        meta = meta_for(path)
        meta["downloads"] = int(meta.get("downloads") or 0) + 1
        with open(os.path.join(META, name + ".json"), "w") as f:
            json.dump(meta, f)


def matches(meta, words):
    text = " ".join([meta.get("title") or "", meta.get("requested_by") or "", meta.get("lyrics") or ""]).lower()
    return all(w in text for w in words)


def library(query=""):
    """Every stored track, newest first, for the admin and song list pages and the bot's search; with a
    query, only those whose title, requester or lyrics contain every word of it."""
    words = query.lower().split()
    out = []
    for name in os.listdir(LIBRARY):
        path = os.path.join(LIBRARY, name)
        meta = meta_for(path)
        if words and not matches(meta, words):
            continue
        out.append({"file": name, "title": meta.get("title") or name, "seconds": seconds(path),
                    "queued_at": int(os.path.getmtime(path)), "timed": bool(meta.get("synced")),
                    "lyrics": bool(meta.get("lyrics")), "dj": meta.get("dj") is True,
                    "requested_by": meta.get("requested_by", ""), "downloads": meta.get("downloads", 0)})
    return sorted(out, key=lambda t: -t["queued_at"])


def file_sha(path):
    with open(path, "rb") as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def same_audio(digest_hex):
    """The library song whose file is byte for byte this one, or None."""
    for m in os.listdir(META):
        name = m[:-len(".json")]
        meta = meta_for(os.path.join(LIBRARY, name))
        if meta.get("sha256") == digest_hex and not meta.get("dj") and os.path.isfile(os.path.join(LIBRARY, name)):
            return name
    return None


def fingerprint_library():
    """Give every stored song a fingerprint, so new copies of it are recognised (songs stored before
    fingerprints existed get theirs here, once)."""
    for m in os.listdir(META):
        path = os.path.join(LIBRARY, m[:-len(".json")])
        if not m.endswith(".json") or not os.path.isfile(path) or meta_for(path).get("sha256"):
            continue
        digest_hex = file_sha(path)
        with META_LOCK:
            meta = meta_for(path)
            meta["sha256"] = digest_hex
            with open(os.path.join(META, m), "w") as f:
                json.dump(meta, f)


def needs_lyrics(meta):
    return not (meta.get("dj") or meta.get("synced") or meta.get("lyrics_status") in ("instrumental", "failed"))


def save_meta(name, change):
    with META_LOCK:
        meta = meta_for(os.path.join(LIBRARY, name))
        change(meta)
        with open(os.path.join(META, name + ".json"), "w") as f:
            json.dump(meta, f)


def release_held():
    while True:
        with HOLD:
            HOLD.wait(2)
        release_ready()


def release_ready():
    """Queue held songs in order: the first once its lyrics are done or its wait is up, the rest after it."""
    with HOLD:
        if not HELD:
            return
        meta = meta_for(os.path.join(LIBRARY, HELD[0][0]))
        if needs_lyrics(meta) and time.time() < meta.get("held_until", 0):
            return
        ready, HELD[:] = list(HELD), []
    for i, (name, uri) in enumerate(ready):
        try:
            telnet("q.push " + uri)
        except OSError:
            with HOLD:
                HELD[0:0] = ready[i:]  # the player is restarting; these go first next time
            return
        save_meta(name, lambda m: m.pop("held_until", None))


def resume_holds():
    """Holds survive a restart of the API: they are written down with the song."""
    held = []
    for m in os.listdir(META):
        meta = meta_for(os.path.join(LIBRARY, m[:-len(".json")]))
        if meta.get("held_until") and os.path.isfile(os.path.join(LIBRARY, m[:-len(".json")])):
            held.append((meta.get("added", 0), m[:-len(".json")]))
    with HOLD:
        HELD[:] = [[name, os.path.join(LIBRARY, name)] for _, name in sorted(held)]


def level_interlude(path):
    """Bring a DJ interlude to the songs' loudness (EBU R128), peaks under -1 dBFS, rewriting the file in
    its own format. Interludes are never reused, so changing the file is safe; a failure leaves it as it
    was, only quieter."""
    tmp = path + ".level" + os.path.splitext(path)[1]
    try:
        subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", path, "-map_metadata", "0",
                        "-af", f"loudnorm=I={DJ_LUFS}:TP=-1:LRA=11", "-ar", "48000", tmp],
                       capture_output=True, timeout=60, check=True)
        os.replace(tmp, path)
        return True
    except (OSError, subprocess.SubprocessError) as e:
        print("interlude not levelled:", path, repr(e)[:200], flush=True)
        if os.path.exists(tmp):
            os.remove(tmp)
        return False


def push(body, extra=None, hold=False):
    """Queue a hosted file ({"url"}) or a track already in the library ({"file"}). extra is metadata set by
    the server itself (who sent it), never taken from the body. hold: an API request, which waits for its
    lyrics if it would play first (see LYRICS_WAIT)."""
    src = None
    if body.get("file"):
        name = str(body["file"])
        if not AUDIO.match(name) or not os.path.isfile(os.path.join(LIBRARY, name)):
            return 404, {"error": "no such track in the library"}
    else:
        url = str(body.get("url", "")).strip()
        if not url.startswith(PREFIX):
            return 400, {"error": "only files hosted at " + PREFIX + " can be queued"}
        name = url[len(PREFIX):]
        if not AUDIO.match(name):
            return 400, {"error": "not an audio file link"}
        src = os.path.join(UPLOADS, name)
        if not os.path.isfile(src):
            return 404, {"error": "that file is gone (expired or never uploaded)"}
    queued = pending()
    if len(queued) + len(HELD) >= QUEUE_MAX:
        return 429, {"error": f"the queue is full ({QUEUE_MAX} tracks)"}
    dest = os.path.join(LIBRARY, name)
    if src and not os.path.exists(dest):
        # The same audio uploaded again under another link plays the copy the radio already has.
        if not body.get("dj") and (dup := same_audio(file_sha(src))):
            name, dest = dup, os.path.join(LIBRARY, dup)
        else:
            shutil.copyfile(src, dest)
    os.utime(dest)  # retention counts from the last time a track was queued
    prev = meta_for(dest)  # a track queued before keeps what it was given then
    levelled = prev.get("levelled", False)
    if (body.get("dj") is True or prev.get("dj")) and not levelled:
        levelled = level_interlude(dest)
    meta = {k: str(body.get(k) or prev.get(k) or "").strip()[:n] for k, n in MAX_TEXT.items()}
    tags = flac_tags(dest)
    meta["title"] = meta["title"] or tags.get("TITLE", "")[:MAX_TEXT["title"]] or os.path.splitext(name)[0]
    meta["dj"] = body.get("dj") is True or prev.get("dj") is True
    # Lyrics embedded by the song tool are what was actually sung; they beat whatever the caller sent.
    meta["lyrics"] = tags.get("LYRICS", "")[:MAX_TEXT["lyrics"]] or meta["lyrics"]
    # Timings saved from an earlier queue are at least as new as the file's (a re-timing updates them).
    meta["synced"] = prev.get("synced") or parse_lrc(tags.get("SYNCEDLYRICS", ""))
    meta["requested_by"] = tags.get("REQUESTED_BY", "")[:40] or prev.get("requested_by", "")
    meta["added"] = int(time.time())
    meta["sha256"] = prev.get("sha256") or file_sha(dest)
    if levelled:
        meta["levelled"] = True
    for k in ("key_id", "lyrics_status"):
        if k in prev:
            meta[k] = prev[k]
    meta.update(extra or {})
    with META_LOCK:
        meta["downloads"] = meta_for(dest).get("downloads", 0)
        with open(os.path.join(META, name + ".json"), "w") as f:
            json.dump(meta, f)
    with NEW_TRACK:
        NEW_TRACK.notify_all()
    # A spoken clip is short: a crossfade would eat its first and last seconds, so it plays whole.
    uri = 'annotate:liq_cross_duration="0.":' + dest if meta["dj"] else dest
    with HOLD:
        if hold and (HELD or (not queued and needs_lyrics(meta))):
            save_meta(name, lambda m: m.update(held_until=time.time() + LYRICS_WAIT))
            HELD.append([name, uri])
            prune()
            return 200, {"queued": meta["title"], "position": len(queued) + len(HELD),
                         "waiting_for_lyrics": LYRICS_WAIT}
    rid = telnet("q.push " + uri)
    prune()
    return 200, {"queued": meta["title"], "position": len(queued) + len(HELD) + 1, "request": rid}


def load_keys():
    try:
        with open(KEYS) as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def save_keys(keys):
    tmp = KEYS + ".tmp"
    with open(os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as f:
        json.dump(keys, f)
    os.replace(tmp, KEYS)


def digest(key):
    return hashlib.sha256(key.encode()).hexdigest()


def create_key(name):
    """A new key; only its hash is kept, so this is the one time it can be shown."""
    name = " ".join(str(name or "").split())[:40]
    if not name:
        return 400, {"error": "give the key a name (who it is for)"}
    key = "rk_" + secrets.token_urlsafe(24)
    with KEYS_LOCK:
        keys = load_keys()
        kid = secrets.token_hex(4)
        keys[kid] = {"name": name, "hash": digest(key), "created": int(time.time()), "revoked": None,
                     "last_used": None, "uses": 0, "recent": []}
        save_keys(keys)
    return 200, {"id": kid, "name": name, "key": key}


def list_keys():
    return [{"id": kid, **{k: v for k, v in rec.items() if k not in ("hash", "recent")},
             "today": len([t for t in rec.get("recent", []) if t > time.time() - 86400])}
            for kid, rec in sorted(load_keys().items(), key=lambda kv: -kv[1]["created"])]


def revoke_key(kid):
    with KEYS_LOCK:
        keys = load_keys()
        if kid not in keys:
            return 404, {"error": "no such key"}
        keys[kid]["revoked"] = keys[kid]["revoked"] or int(time.time())
        save_keys(keys)
    return 200, {"revoked": kid}


def request_allowed(forwarded_for):
    """Whether /request is open to this client. The proxy sets X-Forwarded-For (replacing what the client
    sent), so its last entry is the connecting address."""
    if not REQUEST_ALLOW:
        return True
    try:
        ip = ipaddress.ip_address(forwarded_for.split(",")[-1].strip())
    except ValueError:
        return False
    return any(ip in net for net in REQUEST_ALLOW)


def key_from(header):
    """The id of the live key presented as "Bearer <key>", or None."""
    got = digest(header[7:].strip()) if header.startswith("Bearer ") else ""
    for kid, rec in load_keys().items():
        if hmac.compare_digest(got, rec["hash"]) and not rec["revoked"]:
            return kid
    return None


class FetchError(Exception):
    pass


def public_address(host):
    """The address to connect to, if every address the name has is public; a name that also resolves to a
    private one is refused, and the connection is pinned so the name can't be re-pointed in between."""
    try:
        infos = socket.getaddrinfo(host, None, type=socket.SOCK_STREAM)
    except OSError:
        raise FetchError("that link's server doesn't exist")
    ips = [ipaddress.ip_address(info[4][0].split("%")[0]) for info in infos]
    if not ips or not all(ip.is_global and not ip.is_multicast for ip in ips):
        raise FetchError("links must point at a public server")
    return str(ips[0])


class PinnedHTTP(http.client.HTTPConnection):
    def __init__(self, host, port, ip, **kw):
        super().__init__(host, port, **kw)
        self.ip = ip

    def connect(self):
        self.sock = socket.create_connection((self.ip, self.port), self.timeout)


class PinnedHTTPS(http.client.HTTPSConnection):
    def __init__(self, host, port, ip, **kw):
        super().__init__(host, port, context=ssl.create_default_context(), **kw)
        self.ip = ip

    def connect(self):
        sock = socket.create_connection((self.ip, self.port), self.timeout)
        self.sock = self._context.wrap_socket(sock, server_hostname=self.host)


def fetch(url, max_mb=None):
    max_mb = max_mb or FETCH_MAX_MB
    deadline, cap = time.monotonic() + FETCH_SECONDS, int(max_mb * 1024 * 1024)
    for _ in range(5):
        u = urllib.parse.urlsplit(url)
        if u.scheme not in ("http", "https") or not u.hostname:
            raise FetchError("only http and https links can be queued")
        port = u.port or (443 if u.scheme == "https" else 80)
        if port not in (80, 443):
            raise FetchError("links must use the standard web ports")
        conn = (PinnedHTTPS if u.scheme == "https" else PinnedHTTP)(u.hostname, port, public_address(u.hostname),
                                                                     timeout=15)
        try:
            conn.request("GET", (u.path or "/") + ("?" + u.query if u.query else ""),
                         headers={"User-Agent": "radio-fetch/1 (fetches songs listeners queue)", "Accept": "audio/*"})
            r = conn.getresponse()
            if r.status in (301, 302, 303, 307, 308) and r.getheader("Location"):
                url = urllib.parse.urljoin(url, r.getheader("Location"))
                continue
            if r.status != 200:
                raise FetchError(f"the link answered {r.status}")
            if int(r.getheader("Content-Length") or 0) > cap:
                raise FetchError(f"the file is over {max_mb:g} MB")
            data = bytearray()
            while chunk := r.read(65536):
                data += chunk
                if len(data) > cap:
                    raise FetchError(f"the file is over {max_mb:g} MB")
                if time.monotonic() > deadline:
                    raise FetchError("the download took too long")
            return bytes(data)
        except (OSError, http.client.HTTPException) as e:
            raise FetchError(f"couldn't download that link ({e.__class__.__name__})")
        finally:
            conn.close()
    raise FetchError("too many redirects")


def audio_kind(data):
    """The file type from its first bytes; the name and the server's word for it don't count."""
    if data[:4] == b"fLaC":
        return "flac"
    if data[:4] == b"OggS":
        return "ogg"
    if data[:3] == b"ID3" or (len(data) > 1 and data[0] == 0xFF and data[1] & 0xE0 == 0xE0):
        return "mp3"
    if data[:4] == b"RIFF" and data[8:12] == b"WAVE":
        return "wav"
    if data[4:8] == b"ftyp":
        return "m4a"
    return None


def request(kid, body):
    """Queue a song from any public link for an API key holder, within the key's limits."""
    now = time.time()
    with KEYS_LOCK:
        keys = load_keys()
        rec = keys[kid]
        recent = [t for t in rec.get("recent", []) if t > now - 86400]
        if KEY_DAILY_MAX and len(recent) >= KEY_DAILY_MAX:
            return 429, {"error": f"this key has queued {KEY_DAILY_MAX} songs today; try again tomorrow"}
    waiting = sum(1 for p in pending() + [h[0] for h in HELD] if meta_for(p).get("key_id") == kid)
    if KEY_WAITING_MAX and waiting >= KEY_WAITING_MAX:
        return 429, {"error": f"this key already has {waiting} songs waiting; let them play first"}
    if len(pending()) + len(HELD) >= QUEUE_MAX:
        return 429, {"error": f"the queue is full ({QUEUE_MAX} tracks)"}
    dj = body.get("dj") is True
    if body.get("song"):
        if dj:
            return 400, {"error": "an interlude is a new clip (url), never a song already played"}
        return requeue(kid, rec, str(body["song"]).strip(), now)
    synced = timed_lyrics(body)
    if body.get("synced") and not synced:
        return 400, {"error": "couldn't read the timed lyrics: send LRC text or [[seconds, line], ...]"}
    url = str(body.get("url", "")).strip()
    if url.startswith(PREFIX):
        data = None  # the radio's own file host: read from disk as usual
    else:
        try:
            data = fetch(url, DJ_FETCH_MAX_MB if dj else None)
        except FetchError as e:
            return 400, {"error": str(e)}
        kind = audio_kind(data)
        if not kind:
            return 400, {"error": "that link isn't an audio file (mp3, flac, ogg, wav or m4a)"}
        if not dj and (dup := same_audio(hashlib.sha256(data).hexdigest())):
            status, out = requeue(kid, rec, dup, now)
            return status, ({**out, "duplicate_of": os.path.splitext(dup)[0]} if status == 200 else out)
    body = {**body, "dj": dj}
    if dj and not str(body.get("title") or "").strip():
        body["title"] = f"{rec['name']} (dj)"
    if not str(body.get("title") or "").strip():
        guess = urllib.parse.unquote(os.path.splitext(os.path.basename(urllib.parse.urlsplit(url).path))[0])
        body = {**body, "title": " ".join(guess.replace("_", " ").split())}
    # Who asked for the song, if the key holder says (a bot queueing for its users); otherwise the key.
    asked = " ".join(str(body.get("requested_by") or "").split())[:40]
    extra = {"key_id": kid, "requested_by": asked or rec["name"]}
    if synced:  # already timed: shown as sent, and the lyrics worker leaves it alone
        extra.update(synced=synced, lyrics_status="supplied",
                     lyrics="\n".join(l for _, l in synced if l)[:MAX_TEXT["lyrics"]])
    if data is None:
        status, out = push({**body, "by": rec["name"]}, extra, hold=True)
    else:
        name = f"k{secrets.token_urlsafe(6)}.{kind}"
        with open(os.path.join(LIBRARY, name), "wb") as f:
            f.write(data)
        status, out = push({**body, "file": name, "by": rec["name"]}, extra, hold=True)
    if status == 200:
        count_use(kid, now)
    return status, out


def count_use(kid, now):
    with KEYS_LOCK:
        keys = load_keys()
        if kid in keys:
            keys[kid]["recent"] = [t for t in keys[kid].get("recent", []) if t > now - 86400] + [now]
            keys[kid]["uses"] = keys[kid].get("uses", 0) + 1
            keys[kid]["last_used"] = int(now)
            save_keys(keys)


def requeue(kid, rec, song, now):
    """Queue a song the radio already has. It keeps its title, lyrics and who asked for it; the key holder
    is only who queued it this time."""
    name = song if os.path.splitext(song)[1] else next(
        (n for n in os.listdir(LIBRARY) if os.path.splitext(n)[0] == song), "")
    meta = meta_for(os.path.join(LIBRARY, name)) if AUDIO.match(name) else {}
    if not meta or meta.get("dj") or not os.path.isfile(os.path.join(LIBRARY, name)):
        return 404, {"error": "no song with that id; find ids with GET /library"}
    status, out = push({"file": name, "by": rec["name"]}, {"key_id": kid}, hold=True)
    if status == 200:
        count_use(kid, now)
    return status, out


def lyrics_pending_wait(seconds):
    """lyrics_pending(), but when there is nothing to do, wait up to seconds for a new track first."""
    todo = lyrics_pending()
    deadline = time.monotonic() + seconds
    while not todo and time.monotonic() < deadline:
        with NEW_TRACK:
            NEW_TRACK.wait(deadline - time.monotonic())
        todo = lyrics_pending()
    return todo


def lyrics_pending():
    """Songs still without timed lyrics, the queued ones first: what the lyrics worker should do next."""
    queued = [os.path.basename(p) for p in pending()] + [h[0] for h in HELD]
    todo = []
    for name in os.listdir(LIBRARY):
        meta = meta_for(os.path.join(LIBRARY, name))
        if meta.get("dj") or meta.get("synced") or meta.get("lyrics_status") in ("instrumental", "failed"):
            continue
        todo.append({"file": name, "lyrics": meta.get("lyrics", ""), "queued": name in queued,
                     "added": meta.get("added", 0)})
    return sorted(todo, key=lambda t: (not t["queued"], -t["added"]))[:20]


def save_lyrics(body):
    name = os.path.basename(str(body.get("file", "")))
    path = os.path.join(LIBRARY, name)
    if not AUDIO.match(name) or not os.path.isfile(path):
        return 404, {"error": "no such track"}
    with META_LOCK:
        meta = meta_for(path)
        if body.get("instrumental"):
            meta["lyrics_status"] = "instrumental"
        elif body.get("failed"):
            meta["lyrics_status"] = "failed"
        else:
            lines = [[round(float(t), 2), str(l)[:200]] for t, l in body.get("lines", [])[:400]]
            meta["synced"] = lines
            meta["lyrics"] = meta.get("lyrics") or "\n".join(l for _, l in lines)[:MAX_TEXT["lyrics"]]
            meta["lyrics_status"] = "transcribed" if body.get("transcribed") else "aligned"
        with open(os.path.join(META, name + ".json"), "w") as f:
            json.dump(meta, f)
    with HOLD:
        HOLD.notify_all()
    return 200, {"saved": name}


def now_info(listening=None):
    path, _, started = telnet("now").partition("\n")
    return {**meta_for(path), "file": os.path.basename(path), "seconds": seconds(path) if path else None,
            "started": float(started or 0) or None, "server_time": time.time(),
            "listeners": listeners() if listening is None else listening}


def queue_info():
    queued = pending()
    held = [os.path.join(LIBRARY, h[0]) for h in list(HELD)]
    return {"queue": [meta_for(p).get("title") for p in queued] +
                     [f"{meta_for(p).get('title')} (getting lyrics)" for p in held],
            "files": [os.path.basename(p) for p in queued + held]}


def sse(event, data):
    return f"event: {event}\ndata: {json.dumps(data, separators=(',', ':'))}\n\n".encode()


# One queue per connected page; the broadcaster fills them all, so Liquidsoap and Icecast are asked once
# for everyone rather than once per listener.
subscribers = set()
subscribers_lock = threading.Lock()


def broadcast(message):
    with subscribers_lock:
        for q in subscribers:
            try:
                q.put_nowait(message)
            except Full:
                pass  # a page that stopped reading loses frames, not the server's memory


def broadcaster():
    last_now, last_queue, listening, tick = None, None, None, 0
    while True:
        time.sleep(1)
        tick += 1
        with subscribers_lock:
            if not subscribers:
                # whoever connects next gets all of these in their snapshot
                last_now, last_queue = None, None
                continue
        try:
            if tick % 5 == 0 or listening is None:
                listening = listeners()
            now = now_info(listening)
            key = (now.get("title"), now.get("started"), now.get("lyrics"), len(now.get("synced") or []),
                   now.get("listeners"))
            if key != last_now:
                last_now = key
                broadcast(sse("now", now))
            queue = queue_info()
            if queue != last_queue:
                last_queue = queue
                broadcast(sse("queue", queue))
        except OSError:
            time.sleep(2)  # the player is restarting


class Handler(BaseHTTPRequestHandler):
    def reply(self, status, obj):
        data = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def authorized(self):
        got = self.headers.get("Authorization", "")
        return hmac.compare_digest(got.encode(), ("Bearer " + TOKEN).encode())

    def events(self):
        q = Queue(maxsize=40)
        with subscribers_lock:
            if len(subscribers) >= EVENTS_MAX:
                return self.reply(503, {"error": "too many listeners; poll instead"})
            subscribers.add(q)
        try:
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(b"retry: 3000\n\n")
            try:
                self.wfile.write(sse("now", now_info()) + sse("queue", queue_info()))
            except OSError:
                pass  # the player is down; the broadcaster sends it all once it is back
            self.wfile.flush()
            while True:
                try:
                    message = q.get(timeout=15)
                except Empty:
                    message = b": keepalive\n\n"
                self.wfile.write(message)
                self.wfile.flush()
        except OSError:
            pass  # the page went away
        finally:
            with subscribers_lock:
                subscribers.discard(q)

    def do_GET(self):
        if self.path == "/events":
            return self.events()
        try:
            if self.path == "/now":
                self.reply(200, now_info())
            elif self.path.startswith("/meta/"):
                name = urllib.parse.unquote(self.path[len("/meta/"):])
                if not AUDIO.match(name) or not os.path.isfile(os.path.join(LIBRARY, name)):
                    return self.reply(404, {"error": "no such track"})
                self.reply(200, {**meta_for(name), "seconds": seconds(os.path.join(LIBRARY, name))})
            elif self.path.split("?", 1)[0] == "/listening":
                beat = urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query).get("id", [""])[0]
                if not re.fullmatch(r"[A-Za-z0-9]{16,64}", beat):
                    return self.reply(400, {"error": "bad id"})
                heard_from(beat)
                self.reply(200, {})
            elif self.path == "/queue":
                self.reply(200, queue_info())
            elif self.path.split("?", 1)[0] == "/library":
                query = urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query).get("q", [""])[0]
                self.reply(200, {"tracks": library(query[:200])})
            elif self.path in ("/keys",) or self.path.split("?", 1)[0] == "/lyrics/pending" \
                    or self.path.startswith("/track/"):
                if not self.authorized():
                    return self.reply(401, {"error": "unauthorized"})
                if self.path == "/keys":
                    self.reply(200, {"keys": list_keys()})
                elif self.path.startswith("/lyrics/pending"):
                    wait = urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query).get("wait", ["0"])[0]
                    try:
                        wait = min(max(float(wait), 0), 55)
                    except ValueError:
                        wait = 0
                    self.reply(200, {"tracks": lyrics_pending_wait(wait)})
                else:
                    self.send_track(urllib.parse.unquote(self.path[len("/track/"):]))
            elif self.path == "/downloaded":
                # Never refuses: a failed count must not block the download.
                if self.headers.get("X-Forwarded-Method", "GET") == "GET":
                    count_download(self.headers.get("X-Forwarded-Uri", ""), self.headers.get("Range"))
                self.reply(200, {})
            else:
                self.reply(404, {"error": "not found"})
        except OSError:
            self.reply(503, {"error": "the player is not running"})

    def send_track(self, name):
        path = os.path.join(LIBRARY, os.path.basename(name))
        if not AUDIO.match(os.path.basename(name)) or not os.path.isfile(path):
            return self.reply(404, {"error": "no such track"})
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(os.path.getsize(path)))
        self.end_headers()
        with open(path, "rb") as f:
            shutil.copyfileobj(f, self.wfile)

    def do_POST(self):
        # /request is the one write open to API key holders; every other one takes the radio's token.
        if self.path == "/request" and not request_allowed(self.headers.get("X-Forwarded-For", "")):
            return self.reply(403, {"error": "requests aren't open from this address"})
        kid = key_from(self.headers.get("Authorization", "")) if self.path == "/request" else None
        if not (kid or self.authorized()) or (self.path == "/request" and not kid):
            return self.reply(401, {"error": "unauthorized (a missing, wrong or revoked key)"})
        n = min(int(self.headers.get("Content-Length") or 0), 65536)
        try:
            body = json.loads(self.rfile.read(n) or b"{}")
        except ValueError:
            return self.reply(400, {"error": "bad json"})
        body = body if isinstance(body, dict) else {}
        try:
            if self.path == "/push":
                self.reply(*push(body))
            elif self.path == "/request":
                self.reply(*request(kid, body))
            elif self.path == "/keys":
                self.reply(*create_key(body.get("name")))
            elif self.path == "/keys/revoke":
                self.reply(*revoke_key(str(body.get("id", ""))))
            elif self.path == "/lyrics":
                self.reply(*save_lyrics(body))
            elif self.path in ("/remove", "/swap"):
                try:
                    args = [int(body["position"])] if self.path == "/remove" else [int(body["a"]), int(body["b"])]
                except (KeyError, TypeError, ValueError):
                    return self.reply(400, {"error": "positions must be numbers"})
                answer = telnet(self.path[1:] + " " + " ".join(map(str, args)))
                self.reply(200 if answer == "ok" else 404, {"result": answer})
            elif self.path == "/skip":
                telnet("skip")
                self.reply(200, {"skipped": True})
            else:
                self.reply(404, {"error": "not found"})
        except OSError:
            self.reply(503, {"error": "the player is not running"})

    def log_message(self, fmt, *args):
        print("%s %s" % (self.command, self.path), flush=True)


if __name__ == "__main__":
    os.makedirs(META, exist_ok=True)
    os.makedirs(os.path.dirname(KEYS), exist_ok=True)
    threading.Thread(target=prune_often, daemon=True).start()
    threading.Thread(target=broadcaster, daemon=True).start()
    threading.Thread(target=fingerprint_library, daemon=True).start()
    resume_holds()
    threading.Thread(target=release_held, daemon=True).start()
    server = ThreadingHTTPServer(("0.0.0.0", 8080), Handler)
    server.daemon_threads = True  # open event streams must not hold up a restart
    server.serve_forever()
