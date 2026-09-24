# Metald

**Metald** is an IRC chatbot driven by an LLM. It is a fork of [pkdindustries/soulshack](https://github.com/pkdindustries/soulshack), licensed under GPL-3 like the original.

## Features

-   **Any model**: OpenAI, Anthropic, Google Gemini, Ollama, or any OpenAI-compatible endpoint.
-   **Tools**: shell and Python plugins, MCP servers, and native IRC tools. Shipped plugins cover web search and page fetch (Exa), code execution in a throwaway Fly.io microVM, image, music, video and speech generation (ComfyUI), speech-to-text, image understanding, Wikipedia, MusicBrainz, YouTube transcripts and pastes; Context7 library docs come over MCP.
-   **Several networks at once**, each with its own nick, channels and conversations. Up to `maxconcurrent` requests (default 3) run at a time across all of them; each request's question and answer are written to history together when it finishes.
-   **Per-network isolation**: memories, ignores, flood counters, reminders and suspicion scores never cross between networks.
-   **Screening**: named nicks can be put behind a classifier on the way in (`screennicks`) and have replies checked on the way out (`filternicks`), with a deterministic block on any reply that reproduces the system prompt.
-   **Injection resistance**: fake `<think>`/tool tags are stripped from input, reasoning the model writes into its reply is filtered, each turn has an output budget, refused tool arguments are removed from history, and a decaying per-speaker suspicion score drops only that speaker's turns.
-   **Persistent memory** (SQLite) with a classifier on every write, so nobody can store an instruction disguised as a fact.
-   **Runtime control** through `+` commands, persisted across restarts.

## Quickstart

### From source

Requires Go 1.26+ and Python 3 for the plugins.

```bash
git clone https://github.com/B4reMetal/metald.git
cd metald
go build -o metald ./cmd/metald
cp examples/chatbot.yml config.yml     # then edit server, channel, model, tools
./metald --config config.yml
```

### Docker

Images for `linux/amd64` and `linux/arm64` are built from `main` and rebuilt weekly for base-image security fixes:

```bash
docker pull ghcr.io/b4remetal/metald:latest
```

They contain the binary, Python 3, curl, jq, ffmpeg, a current yt-dlp and every shipped plugin. The image itself is never written to: everything the container reads or writes lives in one folder mounted at `/config`, and the container can run with `--read-only`.

| path | contents |
|---|---|
| `config.yml` | every setting, including tool secrets under `env:` |
| `plugins/` | your own plugins, listed in `config.yml` as `custom-plugins/<name>`, the same name they have outside Docker |
| `data/` | runtime state: the SQLite memory database, reminders, ignores, settings changed at runtime, the tools' error log (`tool-errors.log`), and temp and cache files |

```bash
mkdir config
docker run --rm --read-only -v $(pwd)/config:/config ghcr.io/b4remetal/metald:latest
# first start: writes config/config.yml from the example and stops; edit it, then
docker run -d --name metald --read-only -v $(pwd)/config:/config ghcr.io/b4remetal/metald:latest
```

`examples/docker-compose.yml` does the same. The container runs as an unprivileged user, so the mounted folder must be writable by it; on Linux, `chown` the folder to the uid the error message names, or add `--user $(id -u)`. To build the image yourself: `docker build . -t metald`.

## Configuration

Everything is set in one file, `config.yml`. Start from `examples/chatbot.yml`, which documents every key and carries the default text of every prompt.

-   **Bot settings** are flat top-level keys named after the command-line flags: `nick`, `server`, `channel`, `model`, `tool`, `admins`, and so on. Each one can also be given as `--<name>` or as the environment variable `METALD_<NAME>`; a flag beats the environment, which beats `config.yml`. `./metald --help` lists them all.
-   **Tool settings** go under `env:`: API keys, service URLs and tool prompts, named as each plugin documents them. The bot passes every entry to every tool it runs as an environment variable. A variable already set in the real environment (for example `docker run -e EXA_API_KEY=...`) takes precedence.
-   **Every prompt is configurable, and none is built in.** Seven are required by the bot itself: `floorprompt`, `gatekeeperpreamble`, `gatekeeperpolicy`, `classifypreamble`, `replyscreenpolicy`, `memorypolicy` and `memoryframe`. The plugins' prompts and safety policies are required settings under `env:`. The bot refuses to start and names anything missing; `examples/chatbot.yml` carries the default text of all of them.
-   **Changes made at runtime** with `+set`, `+admins`, `+tools restrict` or `+screen` are saved to `config-overrides.json` in the data directory and survive restarts. `config.yml` itself is never rewritten.

A minimal file, with the required prompts omitted for brevity:

```yaml
nick: metald
server: irc.example.com
port: 6697
tls: true
channel: "#chat"
admins:
  - "yournick!*@your.host"

model: openai/gpt-5.1
openaikey: "sk-..."

tool:
  - plugins/datetime.sh
  - plugins/websearch.py
  - irc__op

env:
  EXA_API_KEY: "..."
```

### Models

Set `model` to `provider/name`:

| provider | example | also set |
|---|---|---|
| OpenAI | `openai/gpt-5.1` | `openaikey` |
| OpenAI-compatible (LiteLLM, vLLM, llama.cpp…) | `openai/<model>` | `openaiurl`, and `openaikey` if the endpoint needs one |
| Anthropic | `anthropic/claude-opus-4.5` | `anthropickey` |
| Google | `gemini/<model>` | `geminikey` |
| Ollama | `ollama/qwen3:30b` | `ollamaurl` (default `http://localhost:11434`) |

`thinkingeffort` is `off` by default. `low`, `medium` and `high` are standard; any other value is passed to the backend, which decides whether it exists.

### Common settings

| key | default | |
|---|---|---|
| `nick` | `metald` | bot nickname |
| `server`, `port` | `localhost`, `6667` | IRC server |
| `tls`, `tlsinsecure` | off | TLS, and skipping certificate checks |
| `channel`, `channelkey` | | channel to join, and its key |
| `saslnick`, `saslpass`, `serverpass` | | authentication |
| `networks` | | several networks; each entry takes `name`, `nick`, `server`, `port`, `channel`, `channelkey`, `tls`, `tlsinsecure`, `saslnick`, `saslpass`, `serverpass` and `responseprefix`, and inherits anything it leaves out |
| `admins` | none | hostmasks allowed to run admin commands; empty means nobody |
| `addressed`, `trigger` | on, the nick | answer only when addressed, and the word that addresses the bot |
| `commandprefix` | `+` | what starts a command |
| `responseprefix` | | text put in front of every reply line |
| `model`, `maxtokens`, `temperature` | `ollama/llama3.2`, `16384`, `0.7` | model settings |
| `apitimeout` | `5m` | limit for one request, tool calls included |
| `maxconcurrent` | `3` | requests handled at once across all networks |
| `sessionduration`, `maxcontext` | `10m`, `0` (unlimited) | how long an idle conversation is kept, and its token cap |
| `screennicks`, `filternicks` | | nicks screened on the way in and on the way out |
| `floodmessages`, `floodwindow`, `floodtimeout` | `5`, `30s`, `5m` | automatic timeout for floods |
| `tool`, `admintools` | | tools to load, and tools only admins may trigger |
| `datadir` | `.` | where runtime state is kept |
| `urlwatcher` | off | comment on links posted in the channel |
| `sandbox` | off | run shell, bash and MCP tools in a platform sandbox |
| `verbose`, `loglevel`, `logformat` | off, `info`, `text` | logging |

## Plugins

-   **`plugins/`** ships with the code. None of the plugins is tied to one deployment; everything site-specific is a setting under `env:`. `plugins/lib/metald_tools` is a shared library they all use, and custom plugins can too: safety review, URL guard, ComfyUI client, chat and vision calls, hosting uploads, media metadata stripping, the lyricist and the image prompt refiner.
-   **`custom-plugins/`** is for your own. Everything in it except its README is git-ignored. In Docker, put them in the `plugins/` folder of the mounted config folder. See `custom-plugins/README.md` for the plugin contract.

Plugins are optional; the bot starts with none. An enabled plugin must have every setting it declares as required, or the bot refuses to start and names the plugin and the setting:

| plugin | requires under `env:` | also reads |
|---|---|---|
| `websearch`, `webfetch` | `EXA_API_KEY` | |
| `sandbox` | `FLY_API_TOKEN`, `SANDBOX_SAFETY_POLICY` | `FLY_SANDBOX_APP`, `FLY_SANDBOX_REGION` |
| `imagegen` | `IMAGE_SAFETY_PROMPT`, `IMAGE_PROMPT_REFINER`, file hosting (below) | `COMFYUI_URL`, `VISION_API_URL` for the safety check |
| `musicgen` | `MUSIC_SAFETY_POLICY`, `LYRICIST_PROMPT`, `LYRICIST_FORMAT`, file hosting | `COMFYUI_URL`, `SAFETY_REVIEW_URL` |
| `videogen` | `VIDEO_SAFETY_POLICY`, `VIDEO_NEGATIVE_PROMPT`, file hosting; `ffmpeg` on PATH | `COMFYUI_URL`, `SAFETY_REVIEW_URL` |
| `tts` | file hosting | `COMFYUI_URL`, `TTS_VOICES` |
| `cat_pic` | `CAT_PIC_ROAST_PROMPT` | `VISION_API_URL` |
| `paste` | `GIST_URL`, `GIST_TOKEN`, `PASTE_SAFETY_POLICY` | `SAFETY_REVIEW_URL` |
| `stt`, `vision` | | `WHISPER_URL`; `VISION_API_URL`, `VISION_API_KEY` |
| `wikipedia`, `musicinfo`, `youtube`, `datetime`, `weather` | | |

Tools that run a safety review (`musicgen`, `videogen`, `paste`, `sandbox`) also require `SAFETY_REVIEW_PREAMBLE` unless `SAFETY_REVIEW_MODE` is `score`. `examples/chatbot.yml` lists every setting in its `env:` section, with the default text of every prompt and policy.

### File hosting

Images, songs, speech and video go through one uploader, chosen with `UPLOAD_BACKEND`:

-   `zipline` (default): needs `ZIPLINE_URL` and `ZIPLINE_TOKEN`
-   `imgbb`: needs `IMGBB_API_KEY`; images only
-   `http`: any other host, described by settings: the endpoint (`UPLOAD_URL`, which may contain `{filename}`), method, multipart or raw body, extra form fields, where the link is in the response (`UPLOAD_RESPONSE`: a JSON path or `text`), and how expiry is sent

`UPLOAD_HEADERS` adds headers to every upload on every backend, for example `"Authorization: Bearer $FILES_TOKEN"`. For a host the `http` backend can't describe, a custom plugin can call `hosting.register_backend()`.

## Commands

Commands start with the command prefix, `+` by default (`commandprefix: "!"` to change it; punctuation only), and must be the first word of the message: `metald +stats` is ordinary chat, not a command. They don't need the bot to be addressed and run immediately, even while a long request is in progress. The tables below use the default prefix.

| command | admin | |
|---|---|---|
| `+help` | | list commands |
| `+version` | | show the version |
| `+stats` | | show this conversation's size and token use |
| `+reset` | | clear this channel's conversation, custom persona and model switch |
| `+prompt <text>` | | set a custom persona for this channel; it disables every tool until `+reset`. No argument shows the current state |
| `+memories` / `+memories about <nick>` | | list stored memories |
| `+memories clear <nick>` / `+memories forget <id>` | own memories only | clear all memories about a nick, or delete one; admins may do either for anyone |
| `+tools` | | list loaded tools |
| `+tools load <spec>` / `+tools rm <pattern>` | yes | load or unload a tool |
| `+tools restrict <pattern>` / `+tools unrestrict <pattern>` | yes | make tools admin-only, or lift that |
| `+get <key>` / `+set <key> <value>` | yes | read or change a setting |
| `+models` / `+models <name>` | yes | list the models an OpenAI-compatible backend (`openaiurl`) serves, or switch to one |
| `+backend` | yes | check that the OpenAI-compatible backend answers |
| `+admins` / `+admins add <hostmask>` / `+admins remove <hostmask>` | yes | manage admins |
| `+ignore <nick> [duration]` / `+unignore <nick>` | yes | stop answering someone for a while (default 1h) |
| `+screen <nick>` / `+unscreen <nick>` / `+screen list` | yes | put a nick behind inbound and outbound screening, dropping their earlier turns |

## Native tools

These run inside the bot and are listed in `tool:` by name:

-   `irc__op`, `irc__kick`, `irc__ban` (ban and unban), `irc__topic`, `irc__invite`, `irc__mode_set`, `irc__mode_query`: channel management, subject to the bot's own channel permissions.
-   `irc__names`, `irc__whois`: who is in the channel.
-   `irc__action`: send a `/me` action.
-   `irc__ignore`: let the bot mute someone itself, for at most an hour. Admins can't be muted.
-   `irc__remind`, `irc__reminders`: schedule, list and cancel reminders.
-   `memory__remember`, `memory__recall`, `memory__forget`: the bot's long-term memory, checked by `memorypolicy` on every write.

## Sandboxing

With `sandbox: true` (or `--sandbox`, or `METALD_SANDBOX`), shell scripts, the built-in `bash` tool, and MCP servers launched from `tool:` run inside a platform sandbox. Off by default.

**Requirements**: `sandbox-exec` on macOS, `bwrap` (bubblewrap) on Linux. If neither is available the setting is ignored with a `sandbox_unavailable` warning and tools run as before.

**Default policy** for every sandboxed tool:

-   Writes allowed only under the OS temp directory.
-   Outbound network blocked.
-   Sensitive paths blocked from reads: `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.azure`, `~/.config/gcloud`, `~/.kube`, `~/.docker/config.json`, `~/.npmrc`, `~/.config/gh`, `~/.netrc`, `~/.git-credentials`, macOS keychains, and other credential stores.
-   Each sandboxed tool's description gets a `[sandboxed]` suffix so the model knows it's restricted.

**Per-tool overrides**: a plugin declares a `sandbox` field in its `--schema` output; an MCP server's JSON file adds it next to `command`/`args`:

```json
"sandbox": true
"sandbox": { "allowNetwork": true, "writablePaths": ["/tmp/data"] }
"sandbox": { "denyWrite": true }
"sandbox": { "allowEnv": ["HOME", "PATH"] }
```

`false` opts the tool out entirely. Leaving the field out applies the default policy. `POLLYTOOL_*` variables are always stripped from sandboxed processes unless listed in `allowEnv`. Native tools run in-process and are unaffected.

The sandbox lives in pollytool; see [pollytool's Sandboxing section](https://github.com/alexschlessinger/pollytool#sandboxing) and [API.md](https://github.com/alexschlessinger/pollytool/blob/main/API.md) for the full schema and per-platform details.

## Documentation

-   [Contributing](docs/contributing.md): adding commands and tools.
-   [Architecture](docs/architecture.md): how the pieces fit.
-   [AGENTS.md](AGENTS.md): repo rules for contributors and coding agents.

## License

GPL-3.0-only. Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors; Copyright (C) 2026 BareMetal for the metald changes. See `COPYRIGHT` and `license.md`.

---
*From the original soulshack README: Named as tribute to my old friend dayv, sp0t, who i think of often.*
