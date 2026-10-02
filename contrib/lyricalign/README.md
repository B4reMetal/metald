# Lyric aligner

Times each line of a song's lyrics for the radio's read-along display, and writes lyrics for songs that
came without any. It runs as ComfyUI custom nodes (`comfyui/`), so its models (Demucs, MMS forced
alignment, Whisper `turbo`) are under ComfyUI's model management: they load onto the GPU for a lyrics job
and give way to the other models (songs, images, speech) when those need the room.

## Nodes

- **Align lyrics** (`MetaldLyricAlign`): `audio_file` (a file in ComfyUI's `input/`) and `lyrics`.
  Demucs pulls the vocals out first (an aligner does much worse with the band underneath), then MMS forced
  alignment places the known words, with a wildcard token between lines to absorb intros, solos and
  ad-libs. Answers `{"lines": [[seconds, line], ...]}` in its UI output `lyrics`; section labels
  (`[Verse]`) take the time of the line they introduce. The model reads only a–z, so other languages are
  converted for it first: accents folded (canción → cancion), Japanese read with pykakasi, other scripts
  romanized with uroman. The page still shows the original lyrics.
- **Transcribe lyrics** (`MetaldLyricTranscribe`): `audio_file` only. Whisper writes the words from the
  separated vocals, low-confidence and stock filler segments ("thank you for watching") are dropped, the
  words are grouped into lines at pauses, and the lines are timed as above. Answers `{"lines": [...],
  "language": "en"}`, or `{"lines": [], "instrumental": true}` when fewer than 8 words are confidently sung.

Both delete their input file once read, so songs don't pile up in `input/`.

## Install

1. Copy `comfyui/` to `ComfyUI/custom_nodes/ComfyUI-metald-lyricalign`.
2. In ComfyUI's Python environment, without letting pip touch torch:
   `pip install -c <(pip freeze | grep -E '^(torch|torchaudio|numpy)==') demucs==4.0.1 openai-whisper==20250625 uroman==1.3.1.1 pykakasi==2.3.0`
3. Restart ComfyUI. Weights download into `ComfyUI/models/lyricalign` on first use (~2.8 GB).

The bot's song tool uses it with `LYRICS_ALIGN: comfyui` (`plugins/lib/metald_tools/lyricsync.py`).

## Lyrics worker for the radio

`worker.py` (the `lyricworker` service here) gives the radio's songs read-along lyrics: songs queued with
an API key and without lyrics, or any song whose lyrics were never timed. It holds a request to the radio's
`/lyrics/pending?wait=55` open, so the radio answers as soon as a song needs lyrics without reaching into
this network. For each song it downloads the audio, runs Align lyrics if the song came with lyrics and
Transcribe lyrics if not, and posts the result back; the listening page shows it live. Set
`RADIO_API_URL` (e.g. `https://radio.example.com/api`) and `RADIO_TOKEN` in `.env` (mode 600), and
`COMFYUI_URL` if ComfyUI isn't on this host's port 8188. A song ComfyUI can't do is marked failed and not
retried; an unreachable radio or ComfyUI is retried.
