# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

import io
import json
import os
import re
import subprocess
import unicodedata

import comfy.model_management as mm
import comfy.model_patcher
import folder_paths
import soundfile as sf
import torch
import torchaudio

# Weights live under ComfyUI's models folder, not in a user cache.
MODELS = os.path.join(folder_paths.models_dir, "lyricalign")
os.makedirs(MODELS, exist_ok=True)
torch.hub.set_dir(MODELS)

BUNDLE = torchaudio.pipelines.MMS_FA
# Emission is computed in overlapping windows (attention over a whole song won't fit), keeping only each
# window's middle so no word is judged at a window's edge.
WINDOW, HOP = 30 * BUNDLE.sample_rate, 20 * BUNDLE.sample_rate
# Whisper's habit on music: confident-sounding text in the gaps ("thank you for watching").
NOT_SUNG = re.compile(r"(thank(s| you) for (watching|listening)|subscribe|subtitles? by|^\W*$|^\s*\[?music\]?\s*$)", re.I)


class Managed:
    """A model loaded once into system RAM and handed to ComfyUI, which puts it on the GPU when a node
    asks (unloading others if needed) and takes it off again when another model needs the room."""

    def __init__(self, load):
        self._load, self.module, self.patcher = load, None, None

    def ready(self):
        if self.module is None:
            self.module = self._load().eval()
            # ComfyUI records the device by setting .device, which some models only have as a read-only
            # property; a plain holder takes it, and moving the holder moves the model inside.
            holder = torch.nn.Module()
            holder.inner = self.module
            self.patcher = comfy.model_patcher.ModelPatcher(holder, load_device=mm.get_torch_device(),
                                                            offload_device=mm.unet_offload_device())
        return self


def on_gpu(*models):
    mm.load_models_gpu([m.ready().patcher for m in models])
    return [m.module for m in models]


def _separator():
    from demucs.pretrained import get_model
    return get_model("htdemucs")


def _whisper():
    import whisper
    return whisper.load_model("turbo", device="cpu", download_root=os.path.join(MODELS, "whisper"))


SEPARATOR = Managed(_separator)
ALIGNER_MODEL = Managed(lambda: BUNDLE.get_model(with_star=True))
TRANSCRIBER = Managed(_whisper)
TOKENIZER, ALIGNER = BUNDLE.get_tokenizer(), BUNDLE.get_aligner()
LABELS = set(BUNDLE.get_dict()) - {"*", "-"}
_ROMANIZER = _KAKASI = None
KANA = re.compile(r"[぀-ヿ]")


def romanize(line):
    """The aligner knows only a-z and apostrophes: accents are folded away, and other scripts are romanized
    (東京 -> dongjing). What is shown is still the original line; only the aligner sees this."""
    global _ROMANIZER, _KAKASI
    if KANA.search(line):  # Japanese: uroman would read the kanji as Chinese
        if _KAKASI is None:
            import pykakasi
            _KAKASI = pykakasi.kakasi()
        line = " ".join(item["hepburn"] for item in _KAKASI.convert(line))
    if not line.isascii():
        if _ROMANIZER is None:
            import uroman
            _ROMANIZER = uroman.Uroman()
        line = _ROMANIZER.romanize_string(line)
    return "".join(c for c in unicodedata.normalize("NFKD", line) if not unicodedata.combining(c))


def words_of(line):
    return [w for w in (re.sub(r"[^a-z']", "", t.lower()) for t in romanize(line).split()) if w and set(w) <= LABELS]


def take_input(name):
    """The uploaded song's bytes; the file is removed once read, so songs don't pile up in input/."""
    path = folder_paths.get_annotated_filepath(os.path.basename(name))
    try:
        with open(path, "rb") as f:
            return f.read()
    finally:
        os.remove(path)


def decode(audio_bytes):
    """Samples and rate; files soundfile can't read (m4a, opus) go through ffmpeg."""
    try:
        return sf.read(io.BytesIO(audio_bytes), dtype="float32", always_2d=True)
    except sf.LibsndfileError:
        wav = subprocess.run(["ffmpeg", "-v", "error", "-i", "pipe:0", "-f", "wav", "pipe:1"],
                             input=audio_bytes, capture_output=True, timeout=300, check=True).stdout
        return sf.read(io.BytesIO(wav), dtype="float32", always_2d=True)


@torch.inference_mode()
def vocals(audio_bytes):
    """The vocals alone, mono at the aligner's rate: an aligner does much worse with the band underneath."""
    from demucs.apply import apply_model
    data, sr = decode(audio_bytes)
    wav = torch.from_numpy(data.T)
    if wav.shape[0] == 1:
        wav = wav.repeat(2, 1)
    (sep,) = on_gpu(SEPARATOR)
    wav = torchaudio.functional.resample(wav[:2], sr, sep.samplerate)
    ref = wav.mean(0)
    mean, std = ref.mean(), ref.std() + 1e-8
    stems = apply_model(sep, ((wav - mean) / std)[None].to(mm.get_torch_device()), split=True, overlap=0.25)[0]
    voice = stems[sep.sources.index("vocals")] * std + mean
    return torchaudio.functional.resample(voice.mean(0).cpu(), sep.samplerate, BUNDLE.sample_rate)


@torch.inference_mode()
def emission(voice):
    (model,) = on_gpu(ALIGNER_MODEL)
    parts, margin = [], (WINDOW - HOP) // 2
    for start in range(0, voice.shape[0], HOP):
        chunk = voice[start:start + WINDOW]
        e, _ = model(chunk[None].to(mm.get_torch_device()))
        per_sample = e.shape[1] / chunk.shape[0]
        lo = 0 if start == 0 else round(margin * per_sample)
        last = start + WINDOW >= voice.shape[0]
        hi = e.shape[1] if last else round((margin + HOP) * per_sample)
        parts.append(e[0, lo:hi].cpu())
        if last:
            break
    return torch.cat(parts)


def align(voice, lyrics):
    """[[seconds, line], ...] for every non-empty line; a section label ([Verse]) or a line with nothing
    alignable takes the time of the next line that has one. A star token before each line absorbs intros,
    solos and ad-libs, which would otherwise be pinned to the nearest word."""
    lines = [l.strip() for l in lyrics.splitlines() if l.strip()]
    sung = [(i, words_of(l)) for i, l in enumerate(lines) if not l.startswith("[")]
    tokens = [w for _, ws in sung if ws for w in ["*"] + ws] + ["*"]
    if len(tokens) == 1:
        return []
    em = emission(voice)
    spans = ALIGNER(em, TOKENIZER(tokens))
    frame = voice.shape[0] / em.shape[0] / BUNDLE.sample_rate
    starts, k = {}, 0
    for i, ws in sung:
        if ws:
            starts[i] = round(spans[k + 1][0].start * frame, 2)  # k is the line's star
            k += 1 + len(ws)
    out, t = [], 0.0
    for i, line in enumerate(lines):
        at = starts.get(i, next((starts[j] for j, _ in sung if j > i and j in starts), t))
        t = max(t, at)
        out.append([t, line])
    return out


def lyric_lines(words, gap=0.6, longest=9):
    """Whisper's words grouped the way lyrics are written: a new line at a pause, at the end of a sentence,
    or when a line gets long."""
    lines, line, last_end = [], [], None
    for w in words:
        if line and (w["start"] - last_end >= gap or len(line) >= longest or line[-1].rstrip()[-1:] in ".!?"):
            lines.append("".join(line).strip())
            line = []
        line.append(w["word"])
        last_end = w["end"]
    if line:
        lines.append("".join(line).strip())
    return [l for l in lines if l]


def transcribe(voice):
    (model,) = on_gpu(TRANSCRIBER)
    result = model.transcribe(voice.numpy(), word_timestamps=True, condition_on_previous_text=False,
                              fp16=mm.get_torch_device().type == "cuda")
    words = [w for seg in result["segments"]
             if seg["no_speech_prob"] < 0.6 and seg["avg_logprob"] > -1.0 and seg["compression_ratio"] < 2.4
             and not NOT_SUNG.search(seg["text"])
             for w in seg.get("words", [])]
    lines = lyric_lines(words)
    if sum(len(l.split()) for l in lines) < 8:  # a few stray words is not a vocal track
        return {"lines": [], "instrumental": True}
    return {"lines": align(voice, "\n".join(lines)), "language": result.get("language", "")}


class LyricAlign:
    """Time known lyrics against the song they were sung in."""

    @classmethod
    def INPUT_TYPES(cls):
        return {"required": {"audio_file": ("STRING", {"default": "", "tooltip": "a file in input/, removed once read"}),
                             "lyrics": ("STRING", {"multiline": True, "default": ""})}}

    RETURN_TYPES = ()
    OUTPUT_NODE = True
    FUNCTION = "run"
    CATEGORY = "audio/lyrics"

    def run(self, audio_file, lyrics):
        lines = align(vocals(take_input(audio_file)), lyrics)
        return {"ui": {"lyrics": [json.dumps({"lines": lines})]}}


class LyricTranscribe:
    """Write down and time the lyrics of a song that came without them."""

    @classmethod
    def INPUT_TYPES(cls):
        return {"required": {"audio_file": ("STRING", {"default": "", "tooltip": "a file in input/, removed once read"})}}

    RETURN_TYPES = ()
    OUTPUT_NODE = True
    FUNCTION = "run"
    CATEGORY = "audio/lyrics"

    def run(self, audio_file):
        return {"ui": {"lyrics": [json.dumps(transcribe(vocals(take_input(audio_file))))]}}


NODE_CLASS_MAPPINGS = {"MetaldLyricAlign": LyricAlign, "MetaldLyricTranscribe": LyricTranscribe}
NODE_DISPLAY_NAME_MAPPINGS = {"MetaldLyricAlign": "Align lyrics", "MetaldLyricTranscribe": "Transcribe lyrics"}
