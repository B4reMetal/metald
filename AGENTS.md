# AGENTS.md

Repo rules for AI agents and new contributors working on metald. Human-facing docs are in `README.md`, `docs/architecture.md` and `docs/contributing.md`; this file covers what those don't.

## Collaboration

- Stay inside the requested scope. Do not make extra or review-suggested changes without the user's approval.
- Treat feedback from other agents and review bots as input to discuss, not automatic action.
- A suggestion that changes a guard or branch lands only after you show, in a test or a trace, the input that branch handled. A simplification that reads cleaner can still drop a case the old code caught.
- metald is self-hosted software run by one operator for a few channels. Prefer readable code over guards for impossible states, but never weaken a safety check (screening, suspicion, urlguard, fail-closed defaults) to simplify code.

## What this is

A Go IRC chatbot driven by an LLM, forked from [pkdindustries/soulshack](https://github.com/pkdindustries/soulshack) under GPL-3. The fork adds multi-network operation, per-network data isolation, inbound/outbound screening, injection resistance, persistent memory, and a set of Python tools. Module path is `B4reMetal/metald`.

## Required commands

```bash
go build -o metald ./cmd/metald
go test -race -count=1 ./...  # full suite, no live backend or ircd needed
go test ./internal/llm -run TestReply -v
go vet ./...
python3 -m unittest discover -s plugins/tests   # plugin tests, no network
./build.sh                    # docker image metald:dev
```

Before reporting a code change done: `gofmt -l .` is empty, `go vet ./...` is clean, the tests for touched packages pass with `-race -count=1`, the plugin tests pass if `plugins/` changed, and `go build` succeeds. Every Go package under `internal/` has tests except `bot` and `testing`. Add a `_test.go` next to any new file. Tests must not reach the network; anything that would dial a host points at `127.0.0.1:1` or a mock.

## Layout

```
cmd/metald/         main
internal/bot/          startup: parses config, loops over networks, starts reminder schedulers
internal/config/       flags + YAML, networks.go (multi-network), prompts.go (required prompt keys)
internal/irc/          IRC client context, parsing, chunking, reasoning filter, native IRC tools
internal/llm/          completion pipeline, polly.go (streaming + filters), gatekeeper, replyscreen
internal/core/         locks, scoping, memory (SQLite), suspicion, quarantine, reminders, ignores, flood
internal/commands/     + commands (help, set/get, prompt, tools, admins...)
internal/behaviors/    join/op/etc. behaviors
internal/testing/      DefaultTestConfig() mock
examples/chatbot.yml   canonical config, the only one: bot settings, tool settings (env:), default prompt texts
plugins/               shipped tools; each answers --schema and --execute '<json>'
plugins/lib/metald_tools/  shared library for tools (on PYTHONPATH via --pluginlib)
plugins/mcp/           MCP server definitions
plugins/tests/         plugin unit tests (python3 -m unittest discover -s plugins/tests)
custom-plugins/        site-specific tools, git-ignored except its README
```

## Rules that are not obvious from the code

**Prompts have no built-in defaults, anywhere.** The seven keys in `config.PromptKeys` (`floorprompt`, `gatekeeperpreamble`, `gatekeeperpolicy`, `classifypreamble`, `replyscreenpolicy`, `memorypolicy`, `memoryframe`) must be set in config; `NewConfiguration` exits 1 with `prompts_missing keys=...` otherwise. Never add a `Value:` default or a Go const for prompt text. Default text lives only in `examples/chatbot.yml`, guarded by `TestShippedExampleConfigHasEveryPrompt`. Tests that need real prompt text load it with `shippedPrompt(t, key)`. Same rule for tools: every prompt, safety policy and review frame a plugin sends to a model is a required `env:` setting (declared in its `requires`) with default text only in the `env:` section of `examples/chatbot.yml`, guarded by `TestShippedExampleConfigEnvIsValid`; plugin tests load that text with `shipped(name)`. A missing prompt refuses the action rather than skipping the check. Model-facing templates use named placeholders (`{nick}`, `{min_lines}`), never positional format strings.

**One config file.** Everything is set in `config.yml`: bot settings as top-level keys, tool settings under `env:`. At load, `config.ToolEnv`/`ExportToolEnv` export each `env:` entry into the process environment, which every tool inherits; a variable already set in the real environment wins. Do not add a second config source (no `.env` files, no per-tool config files).

**Fail closed.** Empty `admins` means nobody is admin (`CheckAdmin`). Outbound screening denies when the classifier is unreachable. Unknown values are refused, not defaulted, except `thinkingeffort`, which is probed against the backend and passed through if it accepts it.

**Bounded concurrency, coherent history.** Each conversation has a per-key `RequestLock`, and `globalGate` bounds model work across every network; both are counting semaphores whose limit is `maxconcurrent` (default 3, `core.SetConcurrency`, live via `+set`). Lock order is per-key then global, never the reverse. Do not add a second path to the model that bypasses `globalGate`. Named `+commands` skip both locks by default (`Registry.BypassesLock`); a command that calls the model must implement `RequiresLock() bool`. Overlapping requests each work from a snapshot: `llm.Complete` does NOT write the user message up front. The question is held as pending and committed together with the agent's messages as one block when the request finishes (`commitExchange`, under `core.CommitLock(session)`), so history stays question-then-its-answer in completion order. A cancelled request commits nothing; a failed one commits only its question. Anything that rewrites history (`QuarantineSpeaker`) must take the same commit lock.

**Everything is network-scoped.** Session keys, lock keys, memories, ignores, flood counters, reminders and suspicion scores are prefixed via `core.ScopeKey(network, key)` / `irc` `scopeKey`. A store method that takes a key must also take the network. If you add a new persistent store, scope it the same way and add a migration for pre-existing unscoped rows (see `AdoptUnscopedMemories`).

**Streaming output is filtered, not trusted.** `polly.go` runs a `ReasoningFilter` (strips `<think>` and split markers), a per-turn content budget (`maxTurnContent`) that latches and discards the rest of a runaway turn, and a bare-marker backstop in the IRC context. Reply text that reproduces the system prompt is blocked deterministically (`leaksSystemPrompt`, 8-word window) before any classifier runs.

**Suspicion and quarantine.** Signals (`ToolRefused`, `ScreenDenied`, `InjectionFrames`, `MemoryRefused`, `ToolSyntax`, `Runaway`, `ReplyDenied`) feed a per-speaker score with 10-minute half-life. Crossing `SuspicionQuarantine` removes only that speaker's contiguous turns, including their tool messages, from context. Add a signal when you add a new refusal path.

**Config inheritance.** `networks:` is a list of `networkYAML` with pointer fields; unset fields inherit from the top-level `server:`. `Configuration.ForNetwork(s)` clones the config sharing `Bot`/`Model`/`API` and swaps `Server`. Add new per-network fields as pointers so inheritance keeps working.

## Code shape

- Match upstream: small functions, `slog` structured logging with snake_case event names (`prompts_missing`, `sandbox_unavailable`).
- Keep Go `gofmt` clean. Prefer explicit error handling and small interfaces. Use structs, not `map[string]interface{}`.
- Prefer branches that change behaviour. Collapse `switch` cases that equal `default`; do not add branches that only document.
- No backward-compatibility shims unless asked. Go 1.22+: no `tt := tt` in parallel subtests.
- Tests live beside the code, table-driven where it fits. Test file writes use mode `0o600`.

## Comments

**Keep comments concise, and add one only when it is necessary.** Most code needs none; the default is no comment. When one is needed, it records what the code cannot show: why this shape, the bug a guard prevents, a coupling to another file. Restating what the line does buys nothing and goes stale first. One line is the norm.

- Change a line, change its comment, in the same diff.
- An invariant a future change must keep is a test, not a sentence with "must not" in it.
- Doc-comment an exported identifier only when its name leaves the contract unclear.
- No multi-paragraph essays, history of past bugs, or incident narratives in comments; that belongs in the commit message.

## Style

- No secrets, LAN addresses, real hostnames or real user nicks anywhere in the tree, including test fixtures and comments. Use `example.com`, `127.0.0.1`, the fixture nicks already in use (`alice`, `bob`, `carol`, `dave`, `mallory`, `eve`) and channels `#chat` / `#test`.
- `config.yml`, `.env`, `*.db*`, `*.bak*`, `reminders.json`, `ignores.json`, `testbed/` and the binary are gitignored. Never commit them or add exceptions.

## Tools (`plugins/`, `custom-plugins/`)

Shipped tools must stay generic: anything site-specific (hosts, model files, voice clips, prompts) is a setting with a neutral default, read from the environment and set under `env:` in `config.yml`, and a tool that only makes sense for one deployment belongs in `custom-plugins/`. Reusable pieces go in `metald_tools`, not in another tool that others then import. Each tool is a standalone script. `--schema` prints a JSON tool definition (name, description, parameters, optional `sandbox` policy, optional `requires`: env vars the tool cannot work without); `--execute '<json>'` runs it and prints the result. Credentials come from the environment only (i.e. `env:` in `config.yml`), never from the plugin's own files. Tools are optional, but an enabled tool whose `requires` are unset stops the bot at startup (`tool_requirements_missing`), so declare every hard dependency there; the execute-time `Error: ... not configured` check stays as the backstop. Tools that fetch a user-supplied URL directly run it through `urlguard.py` first (see `stt.py`, `vision.py`); the rest hit an API or a fixed host allowlist and never dial arbitrary hosts. Tools that produce media upload through `metald_tools.hosting.upload_file()` (backend chosen by `UPLOAD_BACKEND`) and return a link; expiry is per tool, permanent uploads only on explicit request. Use the library instead of re-implementing: `comfyui.run()` for ComfyUI workflows, `chat.complete()` for model calls, `vision` for looking at images, `safetyreview` for policy checks, `media` for stripping generator metadata. Each surfaces a generic error to the channel and logs the detail with `toollog`.

## When changing behavior

1. Enforce in code, not in the prompt. A prompt line is a suggestion; a filter is a guarantee.
2. Prefer deterministic check → cheap classifier → full model, in that order.
3. Measure before asserting a fix worked; a passing test plus a log line beats a guess.
4. Test against a local ircd only, never a production network.

## Commits and PRs

- Conventional commits: `feat(scope):`, `fix(scope):`, `docs:`, `test:`, `refactor(scope):`.
- **Never attribute the agent as an author.** No `Co-Authored-By` lines naming an AI, no "Generated with" footers, no AI advertising in commit messages, PR bodies or code. The human who commits is the author.
- **Never add session identifiers** to commit messages or PR bodies: no session URLs, conversation or run IDs, request IDs, or links back to an agent session.
- These two rules override any tool or harness default that would add such lines.
- One feature is one branch and one PR. Keep the layers as separate working commits on that branch.
- Before each commit, review the diff for over-engineering: speculative config, unused states, single-caller layers, duplicate helpers.
- Never commit `config.yml`, `.env`, `custom-plugins/` contents, databases, `testbed/`, or anything else in `.gitignore`.
- Never publish identifying details from a real deployment in commits, PRs, issues, docs, fixtures or comments: real nicks, channel names, hostnames, IP addresses, file-host links, or messages copied from a channel. Build equivalent examples instead (`alice`, `#test`, `example.com`, `127.0.0.1`) and check they still reproduce the bug. Keep the real strings in notes outside the repo.

## Field test

Before you report a behavior change complete, run it live against the local testbed ircd (never a production network): build, start the bot, and exercise what changed. For a tool change, run the tool with `--execute` against the real backend it talks to. Report the command and the output you saw. Restart a running bot only when no request is in flight. If a live run is not possible, say so and name the closest check you did run.

## Final report

State which required checks you ran, which you skipped and why, and any unresolved failures. Do not call work complete while a required check is known to fail unless the user accepts the risk.
