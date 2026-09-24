# Metald User Guide

**Metald** is an advanced IRC chatbot powered by LLMs, designed to bridge traditional chat with modern AI capabilities.

## Features

-   **Multi-Provider Support**: Works with OpenAI, Anthropic, Google Gemini, and Ollama.
-   **Unified Tool System**: Supports shell scripts, MCP servers, and native IRC tools.
-   **Secure**: Full SSL/TLS and SASL authentication support.
-   **Session Management**: Configurable history, context window, and session TTL.
-   **Streaming**: Real-time responses with IRC-appropriate chunking.
-   **Passive Mode**: Optional URL watching and analysis.
-   **Runtime Configuration**: Manage settings via IRC commands.

## About this fork

This is a fork of [pkdindustries/soulshack](https://github.com/pkdindustries/soulshack), licensed under GPL-3 like the original. It adds:

-   **Multiple IRC networks** from one process, each with its own nick, channel and conversation context. Requests are serialized across all networks so a single model backend is never asked to do two things at once.
-   **Per-network data isolation**: memories, ignores, flood counters, reminders and suspicion scores never cross between networks.
-   **Inbound screening** (`screennicks`): a classifier gate in front of the model for named nicks. A refused message never enters history.
-   **Outbound screening** (`filternicks`): the finished reply is checked before posting, with a deterministic block on any reply that reproduces the system prompt.
-   **Injection resistance**: stripping of fake `<think>`/tool tags from input, a filter for reasoning the model writes into its reply, a per-turn output budget, refused tool arguments redacted from history, and a decaying per-speaker suspicion score that quarantines only that speaker's own turns.
-   **Persistent memory** (SQLite) with a classifier on every write, so a user cannot store an instruction disguised as a fact.
-   **Tools**: web search and page fetch (Exa), code execution in a throwaway Firecracker microVM (Fly.io), image generation, music generation, video with sound (LTX-2.5), text-to-speech, speech-to-text, Wikipedia, MusicBrainz, YouTube transcripts, and Context7 library docs over MCP.

### Plugins

- **`plugins/`** ships with the code: the generic tools listed below, and `plugins/lib/metald_tools`, a small shared library (logging, safety review, URL guard, lyricist, image prompt refiner, hosting uploads, media metadata stripping). None of the tools are tied to one person's setup; everything site-specific is an environment variable.
- **`custom-plugins/`** is for your own tools. It is git-ignored apart from its README, so site-specific tools stay out of the repo. In Docker, mount them at `/plugins`. The shared library is on `PYTHONPATH` for every tool, so custom plugins can use it too. See `custom-plugins/README.md`.

### Tool credentials

Tools are optional; the bot starts with none. Every tool reads its settings from environment variables. Copy `examples/env.example` to `.env` at the repo root (gitignored) and fill in what you use. An enabled tool must have everything it declares in the `requires` list of its `--schema` output, or the bot refuses to start and names the tool and the missing variables. Tools with no entry here need no credential. The bot's own prompts live in `config.yml`; `examples/chatbot.yml` carries the full default text, and the bot refuses to start if any of the six prompt keys is missing.

| tool | needs |
|---|---|
| `websearch`, `webfetch` | `EXA_API_KEY` |
| `sandbox` (run_code) | `FLY_API_TOKEN`, `FLY_SANDBOX_APP` |
| `imagegen`, `musicgen`, `tts`, `videogen` | `COMFYUI_URL`, plus a file host: `ZIPLINE_URL`/`ZIPLINE_TOKEN` by default, or `UPLOAD_BACKEND=imgbb`/`http` (see *File hosting*) |
| `musicgen` lyricist step | `LYRICIST_PROMPT` (generic text in `env.example`), optional `LYRICIST_URL`/`LYRICIST_MODEL` |
| `videogen` | `COMFYUI_URL` with LTX-2.5 models, `ZIPLINE_URL`/`ZIPLINE_TOKEN`, `VIDEO_SAFETY_POLICY`, `ffmpeg` on PATH |
| `stt` | `WHISPER_URL` |
| `vision`, `cat_pic` | `VISION_API_URL`, `VISION_API_KEY` |
| `paste` | `GIST_URL`, `GIST_TOKEN` |
| `safetyreview` (used by several tools) | `SAFETY_REVIEW_URL`, `SAFETY_REVIEW_MODEL` |

### File hosting

Generated media goes through one uploader (`metald_tools/hosting.py`), picked with `UPLOAD_BACKEND`:

- `zipline` (default): `ZIPLINE_URL`, `ZIPLINE_TOKEN`
- `imgbb`: `IMGBB_API_KEY`, images only
- `http`: any other host, configured from the environment: endpoint (`UPLOAD_URL`, may contain `{filename}`), method, multipart or raw body, extra form fields, where the URL is in the response (`UPLOAD_RESPONSE`, a JSON path or `text`), and how expiry is sent

`UPLOAD_HEADERS` adds headers to every upload on every backend, e.g. `UPLOAD_HEADERS="Authorization: Bearer $FILES_TOKEN"`. For anything the `http` backend can't express, a custom plugin can call `hosting.register_backend()`. `examples/env.example` has the full list.

`examples/chatbot.yml` documents the fork's configuration keys at the bottom.

## Quickstart

### Option 1: Docker

The image bundles the binary, Python 3, curl, ffmpeg, yt-dlp and every shipped tool under `/app/plugins`, with the shared `metald_tools` library on `PYTHONPATH`. Three mount points:

| path | what goes there |
|---|---|
| `/config` | `config.yml` (start from `examples/chatbot.yml`) and `.env` with credentials for the tools you enable |
| `/plugins` | your own tools: any executable that answers `--schema` and `--execute`, listed in `config.yml` as `/plugins/<name>` |
| `/data` | runtime state: `memories.db`, `reminders.json`, `ignores.json`, `config-overrides.json` |

```bash
docker build . -t metald:dev
mkdir -p config plugins data
docker run --rm --entrypoint cat metald:dev /app/examples/chatbot.yml > config/config.yml
docker run --rm --entrypoint cat metald:dev /app/examples/env.example > config/.env
# edit config/config.yml (server, channel, prompts, tool list) and config/.env
docker run -d --name metald \
  -v $(pwd)/config:/config -v $(pwd)/plugins:/plugins -v $(pwd)/data:/data \
  metald:dev
```

`examples/docker-compose.yml` does the same. Bundled tools are referenced as `plugins/<name>` in `config.yml` (the working directory is `/app`); the bot refuses to start if an enabled tool is missing a credential it declares, and the log names the tool and the variable.

### Option 2: Build from Source

**Prerequisites**: Go 1.23+

1.  **Clone and Build**:
    ```bash
    git clone https://github.com/B4reMetal/metald.git
    cd metald
    go build -o metald cmd/metald/main.go
    ```

2.  **Run**:

    ### Configuration File (Recommended)
    
    **Local Binary**:
    ```bash
    ./metald --config examples/chatbot.yml
    ```

    **Docker**:
    ```bash
    # Mount config file to container
    docker run -v $(pwd)/examples/chatbot.yml:/config.yml metald:dev \
      --config /config.yml
    ```

    ### All Flags (Kitchen Sink)

    **Local Binary**:
    ```bash
    ./metald \
      --nick metald \
      --server irc.example.com \
      --port 6697 \
      --tls \
      --channel '#metald' \
      --saslnick mybot \
      --saslpass mypassword \
      --admins "admin!*@*" \
      --model openai/gpt-5.1 \
      --openaikey "sk-..." \
      --maxtokens 4096 \
      --temperature 1 \
      --apitimeout 5m \
      --tool "plugins/datetime.sh" \
      --tool "irc__op" \
      --thinkingeffort off \
      --urlwatcher \
      --verbose
    ```

    **Docker**:
    ```bash
    docker run metald:dev \
      --nick metald \
      --server irc.example.com \
      --port 6697 \
      --tls \
      --channel '#metald' \
      --saslnick mybot \
      --saslpass mypassword \
      --admins "admin!*@*" \
      --model openai/gpt-5.1 \
      --openaikey "sk-..." \
      --maxtokens 4096 \
      --temperature 1 \
      --apitimeout 5m \
      --thinkingeffort off \
      --urlwatcher \
      --verbose
    # Note: Local file tools/scripts require volume mounts to work in Docker
    ```

    ### Ollama (Local)

    **Local Binary**:
    ```bash
    ./metald \
      --server irc.example.com \
      --channel '#metald' \
      --model ollama/qwen3:30b \
      --ollamaurl "http://localhost:11434"
    ```

    **Docker**:
    ```bash
    # Use --network host to access Ollama on localhost
    docker run --network host metald:dev \
      --server irc.example.com \
      --channel '#metald' \
      --model ollama/qwen3:30b \
      --ollamaurl "http://localhost:11434"
    ```

    ### Anthropic

    **Local Binary**:
    ```bash
    ./metald \
      --server irc.example.com \
      --channel '#metald' \
      --model anthropic/claude-opus-4.5 \
      --anthropickey "sk-ant-..."
    ```

    **Docker**:
    ```bash
    docker run metald:dev \
      --server irc.example.com \
      --channel '#metald' \
      --model anthropic/claude-opus-4.5 \
      --anthropickey "sk-ant-..."
    ```


### Configuration Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-n, --nick` | metald | Bot nickname |
| `-s, --server` | localhost | IRC server address |
| `-p, --port` | 6667 | IRC server port |
| `-c, --channel` | | Channel to join |
| `-e, --tls` | false | Enable TLS |
| `--tlsinsecure` | false | Skip TLS cert verification |
| `--saslnick` | | SASL username |
| `--saslpass` | | SASL password |
| `-b, --config` | | Path to YAML config file |
| `-A, --admins` | | Comma-separated admin hostmasks |
| `-a, --addressed` | true | Require the bot be addressed to respond |
| `--trigger` | | Word/phrase that activates the bot instead of its nick (e.g. `hey bot`) |
| `--responseprefix` | | Prefix prepended to every response line (e.g. `[metalai]`) |
| `-V, --verbose` | false | Enable debug logging |
| `--model` | ollama/llama3.2 | LLM model (`provider/name`) |
| `--maxtokens` | 4096 | Max tokens per response |
| `--temperature` | 0.7 | Sampling temperature |
| `-t, --apitimeout` | 5m | API request timeout |
| `--openaikey` | | OpenAI API key |
| `--anthropickey` | | Anthropic API key |
| `--geminikey` | | Google Gemini API key |
| `--ollamaurl` | http://localhost:11434 | Ollama API endpoint |
| `--tool` | | Path to tool definition (repeatable) |
| `--thinkingeffort` | off | Reasoning effort level: off, low, medium, high |
| `--urlwatcher` | false | Enable passive URL watching |
| `--sandbox` | false | Sandbox shell, bash, and MCP tools (see below) |

### YAML Configuration

Create a `config.yml` file:

```yaml
server:
  nick: "metald"
  server: "irc.example.com"
  port: 6697
  channel: "#metald"
  tls: true

bot:
  admins: ["nick!user@host"]
  tools:
    - "plugins/datetime.sh"
    - "plugins/mcp/filesystem.json"
```

Run with: `./metald --config config.yml`

## Commands

| Command | Admin? | Description |
|---------|--------|-------------|
| `+help` | No | Show available commands |
| `+version` | No | Show bot version |
| `+tools` | No | List loaded tools |
| `+tools add <spec>` | Yes | Add a tool at runtime |
| `+tools remove <pattern>` | Yes | Remove a tool |
| `+admins` | Yes | List admins |
| `+admins add <hostmask>` | Yes | Add an admin |
| `+set <key> <value>` | Yes | Set config parameter |
| `+screen <nick>` | Yes | Add the nick to `screennicks` and `filternicks` (gated in, checked out) and drop its earlier turns; persisted with the other runtime overrides |
| `+screen list` / `+screen remove <nick>` / `+unscreen <nick>` | Yes | Show or edit the screening lists, including nicks that came from `config.yml` |
| `+get <key>` | No | Get config parameter |

## Built-in Tools

Metald comes with native IRC management tools (permissions apply):

-   `irc_op`, `irc_deop`: Grant/revoke operator status.
-   `irc_kick`, `irc_ban`, `irc_unban`: User management.
-   `irc_topic`: Set channel topic.
-   `irc_invite`: Invite users to channel.
-   `irc_mode_set`, `irc_mode_query`: Manage channel modes.
-   `irc_names`, `irc_whois`: User information.

## Sandboxing

With `--sandbox` (or `sandbox: true` in YAML, env `METALD_SANDBOX`), all shell scripts, the built-in `bash` tool, and MCP servers launched via `--tool` run inside a platform sandbox. Disabled by default.

**Requirements**: `sandbox-exec` on macOS, `bwrap` (bubblewrap) on Linux. If the backend isn't available the flag is ignored with a `sandbox_unavailable` warning and tools run as before.

**Default policy** (applied to every sandboxed tool):

-   Writes allowed only under the OS temp directory.
-   Outbound network blocked.
-   Sensitive paths blocked from reads: `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.azure`, `~/.config/gcloud`, `~/.kube`, `~/.docker/config.json`, `~/.npmrc`, `~/.config/gh`, `~/.netrc`, `~/.git-credentials`, macOS keychains, and other credential stores.
-   Each sandboxed tool's description gets a `[sandboxed]` suffix so the model knows it's restricted.

**Per-tool overrides** — shell scripts declare a `sandbox` field in their `--schema` output; MCP server JSON files add it alongside `command`/`args`:

```json
"sandbox": true
"sandbox": { "allowNetwork": true, "writablePaths": ["/tmp/data"] }
"sandbox": { "denyWrite": true }
"sandbox": { "allowEnv": ["HOME", "PATH"] }
```

`false` opts the tool out entirely (runs unsandboxed even when `--sandbox` is enabled). Absence of the field uses the default policy above. `POLLYTOOL_*` env vars are always stripped from sandboxed processes unless listed in `allowEnv`.

Native IRC tools (`irc_op`, `irc_kick`, etc.) run in-process and are unaffected.

The sandbox itself lives in pollytool — for the full config schema, merge semantics, and per-platform backend details see [pollytool's Sandboxing section](https://github.com/alexschlessinger/pollytool#sandboxing) and [API.md](https://github.com/alexschlessinger/pollytool/blob/main/API.md).

## Documentation

-   [Contributing](docs/contributing.md): Guide for adding commands and tools.
-   [Architecture](docs/architecture.md): High-level system overview.

## License

GPL-3.0-only. Copyright (C) 2023-2026 Alex Schlessinger and soulshack contributors; Copyright (C) 2026 BareMetal for the metald changes. See `COPYRIGHT` and `license.md`.

---
*From the original soulshack README: Named as tribute to my old friend dayv, sp0t, who i think of often.*
