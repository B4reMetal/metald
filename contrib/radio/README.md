# Web radio

Server side for `plugins/radio.py`. Liquidsoap plays the queue into Icecast; when the queue is empty the stream is silent. `api.py` lets the bot queue a track and reports what is playing, for `www/index.html`: a visualizer, and read-along lyrics when the file carries timed lyrics (a `SYNCEDLYRICS` Vorbis comment in LRC form, which `plugins/musicgen.py` writes via `metald_tools/lyricsync.py`; plain `LYRICS` are shown untimed).

The API never downloads anything. A queued link must start with `RADIO_SOURCE_PREFIX`, and its file is copied from `RADIO_UPLOADS`, the file host's upload directory (for Zipline, file names equal URL names). Queued files are kept in `library/` so they can be queued again after the file host expires them: each for `RADIO_RETENTION_DAYS` (default 7) after it was last queued, within `RADIO_LIBRARY_MAX` files and `RADIO_LIBRARY_MAX_MB` (default 5000) in total, oldest first. Anything queued or playing is never removed. The clean-up runs hourly and on every queue.

The pages in `www/` (listening page, song list) are built from `web/radio` (Svelte + TypeScript): `cd web/radio && npm ci && npm run build`. Don't edit `www/` by hand. Listeners can pick a theme and a visualizer style (bars, mirror, wave, radial, LED meter); both are remembered in their browser.

## Setup

Create `.env` next to `compose.yml` (mode 600):

```
ICECAST_SOURCE_PASSWORD=<random>
ICECAST_ADMIN_PASSWORD=<random>
RADIO_TOKEN=<random, also RADIO_TOKEN in the bot's config.yml>
RADIO_SOURCE_PREFIX=https://files.example.com/u/
RADIO_UPLOADS=/opt/zipline/uploads
RADIO_NAME="my radio"
```

Then `docker compose up -d`. Only `127.0.0.1` ports are published: Icecast on 8100, the API on 8101. Put a reverse proxy in front, and keep writes to your own address as well as behind the token:

```
radio.example.com {
	# downloads for the song list (www/songs.html); track files only
	handle_path /dl/* {
		@song path_regexp ^/[A-Za-z0-9_-]{1,64}\.(mp3|ogg|oga|opus|flac|wav|m4a)$
		handle @song {
			# the API counts the download (and always answers 200)
			forward_auth 127.0.0.1:8101 {
				uri /downloaded
			}
			root * /opt/radio/library
			header Content-Disposition attachment
			file_server
		}
		respond 404
	}
	handle /live {
		reverse_proxy 127.0.0.1:8100 {
			flush_interval -1
		}
	}
	handle_path /api/* {
		handle /downloaded {
			respond 404
		}
		@write method POST
		handle @write {
			@denied not remote_ip 203.0.113.7
			respond @denied 403
			reverse_proxy 127.0.0.1:8101
		}
		handle {
			reverse_proxy 127.0.0.1:8101
		}
	}
	handle {
		root * /opt/radio/www
		file_server
	}
}
```

## API keys

Others can queue songs with an API key, created and revoked in the admin page (`/admin/`). The key is shown once; only its hash is stored, in `./keys`.

```
curl -XPOST https://radio.example.com/api/request \
  -H "Authorization: Bearer rk_..." -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/song.mp3", "title": "Song", "lyrics": "optional, one line per line"}'
```

Every stored file is fingerprinted (SHA-256): a link whose file the radio already has, whether fetched for a key or from the file host, queues the stored song again instead of keeping a second copy (the answer to a key holder says `duplicate_of`). A key holder's own DJ interlude is `{"url": "...", "dj": true}` (up to `RADIO_DJ_MAX_MB`, 10): it plays whole, without the crossfade. Every interlude is levelled on arrival with ffmpeg (in the API's image, `api.Dockerfile`) to `RADIO_DJ_LUFS` (-14, about where the songs sit), so quiet speech doesn't drop out between songs. Every interlude, the bot's included, is deleted within a minute or two of having played; interludes are never requeued, matched as duplicates or listed. To play a song the radio already has, send `{"song": "<id>"}` instead of a url (the `file` from `GET /api/library`): it plays the stored copy and keeps its title, lyrics and original requester.

Lyrics that are already timed go in `synced`, as LRC text (`"[00:12.50]first line\n[00:20.00]second"`, several stamps per line and `<mm:ss>` word tags allowed) or as `[[12.5, "first line"], [20, "second"]]`; LRC pasted into `lyrics` is recognised too. They are shown exactly as sent, and the lyrics worker skips the song. A FLAC file's own `SYNCEDLYRICS` tag is used the same way.

Any public http(s) link to an mp3, flac, ogg, wav or m4a file works (ports 80 and 443 only; the file type is read from its bytes). The API fetches it itself: every address the name resolves to must be public, the connection is pinned to the address it checked, redirects are re-checked, and a download is capped at `RADIO_FETCH_MAX_MB` (100) and 90 seconds. A key may have `RADIO_KEY_WAITING_MAX` (3) songs waiting and queue `RADIO_KEY_DAILY_MAX` (20) a day (0 turns either off); the page credits the key's name, or `requested_by` if the request names who asked (the key still shows as who queued it). Songs without lyrics get them from the lyrics worker in contrib/lyricalign. A requested song that would play first but has no timed lyrics yet is held until the worker is done with it, or `RADIO_LYRICS_WAIT` seconds (120); songs requested meanwhile wait behind it, and holds survive an API restart. `RADIO_REQUEST_ALLOW` (comma-separated addresses or ranges) limits who may use `/request`; empty, anyone with a key can. The proxy must let `POST /request` through from anywhere, ahead of the rule that keeps other writes to your address:

```
		@request {
			method POST
			path /request
		}
		handle @request {
			reverse_proxy 127.0.0.1:8101
		}
```

Answers: 200 `{"queued", "position"}`; 400 with an `error` for a bad link or a file that isn't audio; 401 for a missing, wrong or revoked key; 403 from an address outside `RADIO_REQUEST_ALLOW`; 429 when the key's limits or the queue are full.

To skip the current track: `curl -XPOST -H "Authorization: Bearer $RADIO_TOKEN" https://radio.example.com/api/skip`.
