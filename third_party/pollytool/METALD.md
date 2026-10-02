# pollytool, patched for metald

A copy of github.com/alexschlessinger/pollytool at v0.0.0-20260421064145-217a8a1e496b (MIT, see
LICENSE), used through the `replace` in metald's go.mod. `cmd/` (the polly CLI) is left out.

Changes:

- `llm/openai.go`: Chat Completions responses, streamed or not, now pass on reasoning that
  OpenAI-compatible servers (vLLM, llama.cpp, LiteLLM, DeepSeek) send as `reasoning_content` or
  `reasoning` (`extraText`). The SDK keeps those among a delta's extra fields and the client only read
  `content`, so the model's thinking never reached `OnReasoning`.

Drop this copy and the `replace` once upstream reads those fields.
