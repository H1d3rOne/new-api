package openai

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func TestRewriteOpenAIStreamContentData(t *testing.T) {
	state := &openAIStreamContentRewriteState{}
	first := `{"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":"old "}}]}`
	out := rewriteOpenAIStreamContentData(first, "replacement", state)
	if len(out) != 2 {
		t.Fatalf("expected start and replacement chunks, got %#v", out)
	}
	if !state.Sent {
		t.Fatal("expected replacement content to be marked as sent")
	}

	var start dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal([]byte(out[0]), &start); err != nil {
		t.Fatal(err)
	}
	if len(start.Choices) != 1 || start.Choices[0].Delta.Role != "assistant" || start.Choices[0].Delta.GetContentString() != "" {
		t.Fatalf("expected assistant start chunk, got %s", out[0])
	}

	var content dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal([]byte(out[1]), &content); err != nil {
		t.Fatal(err)
	}
	if len(content.Choices) != 1 || content.Choices[0].Delta.GetContentString() != "replacement" {
		t.Fatalf("expected replacement content chunk, got %s", out[1])
	}
	if strings.Contains(strings.Join(out, "\n"), "old") {
		t.Fatalf("expected original content to be suppressed, got %#v", out)
	}

	second := `{"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"test-model","choices":[{"index":0,"delta":{"content":"content"}}]}`
	if out := rewriteOpenAIStreamContentData(second, "replacement", state); len(out) != 0 {
		t.Fatalf("expected later original content-only chunk to be suppressed, got %#v", out)
	}

	stop := `{"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	out = rewriteOpenAIStreamContentData(stop, "replacement", state)
	if len(out) != 1 || !strings.Contains(out[0], `"finish_reason":"stop"`) {
		t.Fatalf("expected stop chunk to be preserved, got %#v", out)
	}
}
