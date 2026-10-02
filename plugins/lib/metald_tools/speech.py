# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only
"""Speech synthesis via ComfyUI, shared by the speak tool and the radio's DJ intros. Two engines:
MOSS-TTS (ComfyUI-MOSS-TTS-1.5 nodes: clones a voice from its clip, takes a free-text style and
[pause 1.5s] marks) and Chatterbox (ComfyUI_Fill-ChatterBox: exaggeration and pacing dials). With MOSS
chosen, Chatterbox is the reserve: a MOSS failure (not a cancellation) is retried on Chatterbox.

Settings (env: in config.yml):
  COMFYUI_URL          ComfyUI server (default: http://127.0.0.1:8188)
  TTS_ENGINE           moss or chatterbox (default: chatterbox)
  TTS_MOSS_MODEL       the MOSS node's model choice (default: the 4B Local-Transformer v1.5)
  TTS_VOICES           extra voices, name=clip.wav,... (clips in ComfyUI's input/; MOSS likes 10-20 s)
  TTS_MAX_CHARS        longest text accepted (default: 800)
  TTS_EXAGGERATION     Chatterbox delivery intensity, 0.25-2.0 (default: 0.5)
  TTS_CFG_WEIGHT       Chatterbox pacing/guidance, lower = slower and more expressive (default: 0.5)
"""

import os
import re
import time

from metald_tools import comfyui, toollog
from metald_tools.media import strip_audio_metadata


class SpeechError(Exception):
    """Synthesis failed; the message is safe to show, the detail is in the tool log."""


MAX_CHARS = int(os.environ.get("TTS_MAX_CHARS", "800"))
EXAGGERATION = float(os.environ.get("TTS_EXAGGERATION", "0.5"))

CFG_WEIGHT = float(os.environ.get("TTS_CFG_WEIGHT", "0.5"))
ENGINE = os.environ.get("TTS_ENGINE", "chatterbox").strip().lower()
# The node pack's label for the 4B model ("1.7B" is a leftover in its name, kept for saved workflows).
MOSS_MODEL = os.environ.get("TTS_MOSS_MODEL", "OpenMOSS-Team/MOSS-TTS-Local-Transformer-v1.5 (1.7B)")
MOSS_LANGUAGES = ("Arabic", "Cantonese", "Chinese", "Czech", "Danish", "Dutch", "English", "Finnish", "French",
                  "German", "Greek", "Hebrew", "Hindi", "Hungarian", "Italian", "Japanese", "Korean", "Macedonian",
                  "Malay", "Persian (Farsi)", "Polish", "Portuguese", "Romanian", "Russian", "Spanish", "Swahili",
                  "Swedish", "Tagalog", "Thai", "Turkish", "Vietnamese")
PAUSE = re.compile(r"\[pause \d+(?:\.\d+)?s\]")

def parse_voices(raw: str) -> dict:
    """TTS_VOICES: comma-separated name=file pairs; each file is a short
    reference clip in ComfyUI's input folder (voice_scottish_f=voice_scottish_f.wav)."""
    out = {}
    for part in raw.split(","):
        name, _, fname = part.partition("=")
        if name.strip() and fname.strip():
            out[name.strip()] = fname.strip()
    return out

VOICES = parse_voices(os.environ.get("TTS_VOICES", ""))

POLL_TIMEOUT = 540  # can queue behind other renders on the shared GPU; under apitimeout (10m)

def build_workflow(text: str, seed: int, voice: str = "", exaggeration: float = 0.0, cfg_weight: float = 0.0) -> dict:
    wf = {
        "prompt": {
            "1": {"class_type": "FL_ChatterboxTTS", "inputs": {
                "text": text,
                "exaggeration": exaggeration or EXAGGERATION,
                "cfg_weight": cfg_weight or CFG_WEIGHT,
                "temperature": 0.8,
                "seed": seed,
                # Keep the model resident: a cold load takes ~45s, a warm run ~5s.
                "keep_model_loaded": True,
            }},
            "2": {"class_type": "SaveAudio", "inputs": {
                "audio": ["1", 0], "filename_prefix": "metald_speech",
            }},
        }
    }
    # audio_prompt is optional on the node: wired only when a voice is asked
    # for, so the stock voice stays the zero-config path.
    if voice and voice in VOICES:
        wf["prompt"]["3"] = {"class_type": "LoadAudio",
                             "inputs": {"audio": VOICES[voice]}}
        wf["prompt"]["1"]["inputs"]["audio_prompt"] = ["3", 0]
    return wf

def moss_frames(text: str) -> int:
    """MOSS sometimes keeps going past the words; cap it at about twice a slow reading of the text (12
    characters a second), plus any pauses asked for. The node refuses less than 256 frames."""
    pauses = sum(float(m) for m in re.findall(r"\[pause (\d+(?:\.\d+)?)s\]", text))
    seconds = len(PAUSE.sub("", text)) / 12 * 2 + 4 + pauses
    return max(256, int(seconds * 12.5))


def build_moss_workflow(text: str, seed: int, voice: str = "", style: str = "", language: str = "") -> dict:
    """A known voice is cloned from its clip; without one, MOSS speaks in a voice of its own."""
    clone = bool(voice and voice in VOICES)
    inputs = {"moss_model": ["1", 0], "text": text, "language": language if language in MOSS_LANGUAGES else "English",
              "instruction": style, "audio_temperature": 1.7, "audio_top_p": 0.8, "audio_top_k": 25,
              "target_tokens": 0, "max_new_tokens": moss_frames(text), "seed": seed, "target_overshoot_frames": 50}
    wf = {"prompt": {
        "1": {"class_type": "MOSSLoadModel", "inputs": {"model_id": MOSS_MODEL, "device": "cuda", "attention": "auto"}},
        "3": {"class_type": "MOSSVoiceClone" if clone else "MOSSSpeak", "inputs": inputs},
        "4": {"class_type": "SaveAudio", "inputs": {"audio": ["3", 0], "filename_prefix": "metald_speech"}},
    }}
    if clone:
        wf["prompt"]["2"] = {"class_type": "LoadAudio", "inputs": {"audio": VOICES[voice]}}
        inputs["reference_audio"] = ["2", 0]
    return wf


def synthesize(text: str, voice: str = "", exaggeration: float = 0.0, cfg_weight: float = 0.0,
               style: str = "", language: str = "") -> bytes:
    """FLAC audio of text spoken aloud, without generator metadata. style (a free-text delivery hint) and
    language reach MOSS only; exaggeration and cfg_weight reach Chatterbox only. Unset ones use TTS_*."""
    text = " ".join(text.split())
    if not text:
        raise SpeechError("nothing to say")
    if len(text) > MAX_CHARS:
        raise SpeechError(f"too long to read out ({len(text)} chars, limit {MAX_CHARS})")
    seed = int(time.time() * 1000) % (2**31)
    log = lambda m: toollog.log_detail("tts", m)
    audio = None
    if ENGINE == "moss":
        try:
            audio = comfyui.run(build_moss_workflow(text, seed, voice, " ".join(style.split())[:300], language),
                                "audio", POLL_TIMEOUT, log=log)
        except comfyui.ComfyCancelled:
            raise SpeechError("an operator cancelled it on the GPU")
        except RuntimeError as e:
            log(f"moss failed ({e}); speaking with chatterbox instead")
    try:
        if audio is None:
            # Chatterbox would read pause marks aloud.
            plain = " ".join(PAUSE.sub("...", text).split())
            audio = comfyui.run(build_workflow(plain, seed, voice, exaggeration, cfg_weight), "audio", POLL_TIMEOUT,
                                log=log)
    except comfyui.ComfyCancelled:
        raise SpeechError("an operator cancelled it on the GPU")
    except RuntimeError:
        raise SpeechError("the speech backend is unavailable right now")
    before = len(audio)
    audio = strip_audio_metadata(audio)
    if len(audio) != before:
        toollog.log_detail("tts", f"stripped {before - len(audio)} bytes of workflow metadata")
    return audio
