#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
radio tool for metald: queue songs on a web radio and say what is playing.

The server side (Icecast + Liquidsoap + a small API) is in contrib/radio.

Configure under env: in config.yml:
  RADIO_API_URL    base url of the radio API, e.g. https://files.example.com/radio/api (required)
  RADIO_TOKEN      the API's bearer token (required)
  RADIO_PAGE_URL   the listening page posted to the channel (required)
  RADIO_QUEUED_BY  the name the page shows as having queued a track (default: none shown)
  RADIO_DJ_VOICE   voice for spoken intros, one of TTS_VOICES (default: the stock voice)
  RADIO_DJ_EXAGGERATION  delivery intensity for intros, 0.25-2.0 (default: TTS_EXAGGERATION)
  RADIO_DJ_CFG_WEIGHT    pacing for intros, lower = faster and punchier (default: TTS_CFG_WEIGHT)
  RADIO_DJ_STYLE       how intros are spoken when the model gives no intro_style (MOSS only; default:
                       an upbeat radio DJ)
  RADIO_DJ_DELETES_AT  file host retention for intros (default: 12h; the radio keeps its own copy)
  intros use the voice settings in metald_tools/speech.py and the file host in metald_tools/hosting.py
"""

import sys
import os
import json
import re
import tempfile
import time
import urllib.parse
import urllib.request
import urllib.error

_here = os.path.dirname(os.path.abspath(__file__))
sys.path[:0] = [_here, os.path.join(_here, "lib")]
from metald_tools import hosting, lyricsync, speech, toollog
from metald_tools.hosting import upload_file, HostingError
from metald_tools.media import tag_flac

API_URL = os.environ.get("RADIO_API_URL", "").rstrip("/")
TOKEN = os.environ.get("RADIO_TOKEN", "")
PAGE_URL = os.environ.get("RADIO_PAGE_URL", "")
QUEUED_BY = os.environ.get("RADIO_QUEUED_BY", "")
DJ_VOICE = os.environ.get("RADIO_DJ_VOICE", "")
DJ_DELETES_AT = os.environ.get("RADIO_DJ_DELETES_AT", "12h")
DJ_EXAGGERATION = float(os.environ.get("RADIO_DJ_EXAGGERATION") or 0)
DJ_CFG_WEIGHT = float(os.environ.get("RADIO_DJ_CFG_WEIGHT") or 0)
DJ_STYLE = os.environ.get("RADIO_DJ_STYLE") or "An upbeat radio DJ: warm, bright and quick, smiling through the words."
MAX_INTRO = 300
MAX_RESULTS = 15
TIMEOUT = 20
GENERIC_ERROR = "Error: the radio is unavailable right now"


def print_schema():
    schema = {
        "title": "station",
        "description": (
            "the channel's web radio. 'queue' adds a song to the stream: pass the link the "
            "song tool gave you, a short title, and the lyrics exactly as the song tool "
            "returned them (the listening page shows them). to build a playlist, make the "
            "songs with the song tool first, then queue each one; for more than three new songs, "
            "do that inside a background task (task__start), queueing each as it's made. you are also the "
            "station's dj: give about one song in three a spoken 'intro', read aloud just "
            "before the song. write it like a real radio dj talks between records: upbeat, "
            "chatty patter, one or two short sentences - back-announce what just played or "
            "name the song coming up, set it up, give the requester a shout-out, or throw in a "
            "quip; a [pause 1s] works for timing. plain words only, no emoji or links. 'now' says what is "
            "playing, 'list' shows what is queued. 'search' finds songs the radio already "
            "has, by words from the title, the lyrics, or the nick that asked for them "
            "(empty query: the newest), each with an id and a download link; queue one "
            "again with its id as 'song', which works after its original link has "
            "expired. only links from the bot's own file host can be queued. give people "
            "the listening page link it returns."
        ),
        "type": "object",
        "properties": {
            "action": {"type": "string", "enum": ["queue", "now", "list", "search"],
                       "description": "what to do (default: now)"},
            "url": {"type": "string", "description": "for queue: the song's link"},
            "song": {"type": "string", "description": "for queue: a song id from search, instead of url"},
            "query": {"type": "string",
                      "description": "for search: words to find in titles, lyrics or requester nicks"},
            "title": {"type": "string", "description": "for queue: a short title for the song"},
            "lyrics": {"type": "string", "description": "for queue: the lyrics as sung, if any"},
            "intro_style": {"type": "string",
                            "description": ("how to deliver this intro, if not the station's usual upbeat DJ "
                                            "voice: e.g. 'late-night radio, smooth and low' after a slow song, "
                                            "'hyped festival MC' before a banger. leave it out otherwise.")},
            "intro": {"type": "string",
                      "description": (f"a short spoken dj intro for this song, plain words, at most {MAX_INTRO} "
                                      "characters, or \"\" for none. when queueing three or more songs, give at "
                                      "least one of them an intro. \"\" for now, list and search.")},
        },
        # intro is required (empty allowed) because an optional one is skipped when songs are queued in a batch.
        "required": ["action", "intro"],
        "additionalProperties": False,
        "sandbox": {"allowNetwork": True},
        "requires": ["RADIO_API_URL", "RADIO_TOKEN", "RADIO_PAGE_URL"] + hosting.requires(),
    }
    print(json.dumps(schema, indent=2))


def call(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(API_URL + path, data=data, method=method,
                                 headers={"Content-Type": "application/json",
                                          "Authorization": "Bearer " + TOKEN})
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            return resp.status, json.load(resp)
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.load(e)
        except ValueError:
            return e.code, {}


def reachable(url):
    """Whether the song is still hosted, checked before an intro is spoken for it."""
    try:
        with urllib.request.urlopen(urllib.request.Request(url, method="HEAD"), timeout=TIMEOUT) as resp:
            return resp.status == 200
    except (urllib.error.URLError, OSError, ValueError):
        return False


def intro_clip(text, style=""):
    """Speak text and host it; the link, or "" if that failed. The words ride along as the clip's lyrics,
    one sentence per line, so the page highlights them as they are spoken."""
    try:
        audio = speech.synthesize(text, DJ_VOICE, DJ_EXAGGERATION, DJ_CFG_WEIGHT,
                                  style=" ".join(style.split())[:200] or DJ_STYLE)
    except speech.SpeechError as e:
        toollog.log_detail("radio", f"intro not spoken: {e}")
        return ""
    lines = "\n".join(s for s in re.split(r"(?<=[.!?])\s+", " ".join(text.split())) if s)
    with tempfile.NamedTemporaryFile(suffix=".flac") as f:
        f.write(audio)
        f.flush()
        synced = lyricsync.synced_lyrics(f.name, lines)
    audio = tag_flac(audio, {"LYRICS": lines, "SYNCEDLYRICS": synced})
    try:
        return upload_file(audio, f"metald_dj_{int(time.time() * 1000)}.flac", "audio/flac",
                           deletes_at=DJ_DELETES_AT)
    except HostingError as e:
        toollog.log_detail("radio", f"intro upload failed: {e}")
        return ""


def search(args):
    query = " ".join((args.get("query") or "").split())[:200]
    status, out = call("GET", "/library?q=" + urllib.parse.quote(query))
    if status != 200:
        toollog.log_detail("radio", f"library {status} {out}")
        return GENERIC_ERROR
    songs = [t for t in out.get("tracks") or [] if not t.get("dj") and not t.get("title", "").endswith("(dj)")]
    if not songs:
        return f"no songs on the radio match \"{query}\"" if query else "the radio has no songs yet"
    page = PAGE_URL.rstrip("/") + "/"
    lines = []
    for t in songs[:MAX_RESULTS]:
        secs = t.get("seconds")
        bits = [f"id {os.path.splitext(t['file'])[0]}", f"\"{t.get('title')}\""]
        bits += [f"requested by {t['requested_by']}"] if t.get("requested_by") else []
        bits += [f"{int(secs) // 60}:{int(secs) % 60:02d}"] if secs else []
        bits += [time.strftime("%b %d", time.gmtime(t["queued_at"]))] if t.get("queued_at") else []
        bits += [f"{t['downloads']} downloads"] if t.get("downloads") else []
        bits += [page + "dl/" + t["file"]]
        lines.append(" · ".join(bits))
    more = f"\n({len(songs) - MAX_RESULTS} more; narrow the search)" if len(songs) > MAX_RESULTS else ""
    head = f"{len(songs)} song{'s' if len(songs) != 1 else ''}" + (f" matching \"{query}\"" if query else ", newest first")
    return head + ":\n" + "\n".join(lines) + more


def queue_song(args):
    url = (args.get("url") or "").strip()
    song = (args.get("song") or "").strip().removesuffix(".flac")
    if not url and not song:
        return "Error: queue needs the song's link, or its id from search"
    if song and not re.fullmatch(r"[A-Za-z0-9_-]{1,64}", song):
        return "Error: that isn't a song id from search"
    intro = " ".join((args.get("intro") or "").split())
    if len(intro) > MAX_INTRO:
        return f"Error: the intro is too long ({len(intro)} characters, at most {MAX_INTRO})"
    note = ""
    if intro and url and not song and not reachable(url):
        return "Refused: that song's link is gone (expired or never uploaded)"
    if intro:
        clip = intro_clip(intro, str(args.get("intro_style") or ""))
        status, out = call("POST", "/push", {"url": clip, "title": f"{QUEUED_BY} (dj)" if QUEUED_BY else "dj",
                                             "by": QUEUED_BY, "dj": True}) if clip else (0, {})
        if status != 200:
            toollog.log_detail("radio", f"intro push {status} {out}")
            note = " (the spoken intro didn't make it; the song is queued without it)"
    target = {"file": song + ".flac"} if song else {"url": url}
    status, out = call("POST", "/push", {
        **target, "title": args.get("title") or "", "lyrics": args.get("lyrics") or "", "by": QUEUED_BY})
    if status == 200:
        with_intro = " with a spoken intro" if intro and not note else ""
        return f"queued \"{out.get('queued')}\"{with_intro} at position {out.get('position')}{note}. listen: {PAGE_URL}"
    if status in (400, 404, 429) and out.get("error"):
        return "Refused: " + out["error"]
    toollog.log_detail("radio", f"push {status} {out}")
    return GENERIC_ERROR


def run(args):
    if not (API_URL and TOKEN and PAGE_URL):
        return "Error: the radio is not configured"
    action = (args.get("action") or "now").strip().lower()
    try:
        if action == "queue":
            return queue_song(args)
        if action == "search":
            return search(args)
        if action == "list":
            status, out = call("GET", "/queue")
            if status != 200:
                toollog.log_detail("radio", f"queue {status} {out}")
                return GENERIC_ERROR
            queue = out.get("queue") or []
            if not queue:
                return f"nothing queued, so the radio is silent. listen: {PAGE_URL}"
            lines = [f"{i}. {t}" for i, t in enumerate(queue, 1)]
            return "up next:\n" + "\n".join(lines) + f"\nlisten: {PAGE_URL}"
        status, out = call("GET", "/now")
        if status != 200:
            toollog.log_detail("radio", f"now {status} {out}")
            return GENERIC_ERROR
        by = f", queued by {out['by']}" if out.get("by") else ""
        who = f", {out['listeners']} listening" if out.get("listeners") is not None else ""
        if not out.get("title"):
            return f"nothing is playing; queue a song to start the radio. listen: {PAGE_URL}"
        return f"now playing: {out['title']}{by}{who}. listen: {PAGE_URL}"
    except (urllib.error.URLError, OSError, ValueError) as e:
        toollog.log_detail("radio", f"{action} failed: {e}")
        return GENERIC_ERROR


def main():
    if len(sys.argv) < 2:
        print("usage: radio.py --schema | --execute <json>", file=sys.stderr)
        sys.exit(1)
    if sys.argv[1] == "--schema":
        print_schema()
    elif sys.argv[1] == "--execute" and len(sys.argv) > 2:
        try:
            args = json.loads(sys.argv[2])
        except ValueError:
            print("Error: could not read the request")
            return
        print(run(args if isinstance(args, dict) else {}))
    else:
        print(f"unknown argument: {sys.argv[1]}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
