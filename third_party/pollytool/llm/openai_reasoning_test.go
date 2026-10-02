package llm

import (
	"encoding/json"
	"testing"

	openai "github.com/openai/openai-go/v3"
)

// metald patch: reasoning beside the content, as OpenAI-compatible servers send it.
func TestExtraTextReadsReasoningContent(t *testing.T) {
	for raw, want := range map[string]string{
		`{"id":"x","choices":[{"index":0,"delta":{"content":"","reasoning_content":"let me think"}}]}`: "let me think",
		`{"id":"x","choices":[{"index":0,"delta":{"content":"","reasoning":"hmm"}}]}`:                  "hmm",
		`{"id":"x","choices":[{"index":0,"delta":{"content":"hi"}}]}`:                                  "",
		`{"id":"x","choices":[{"index":0,"delta":{"reasoning_content":null}}]}`:                        "",
	} {
		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
			t.Fatal(err)
		}
		if got := extraText(chunk.Choices[0].Delta.JSON.ExtraFields); got != want {
			t.Errorf("%s: got %q, want %q", raw, got, want)
		}
	}
}
