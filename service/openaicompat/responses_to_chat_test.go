package openaicompat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestResponsesRequestToChatCompletionsRequest(t *testing.T) {
	input := json.RawMessage(`[
		{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://example.com/a.png"}]},
		{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"},
		{"type":"function_call_output","call_id":"call_1","output":"ok"}
	]`)
	instructions, err := common.Marshal("be concise")
	require.NoError(t, err)
	tools := json.RawMessage(`[{"type":"function","name":"lookup","description":"Lookup data","parameters":{"type":"object"}}]`)
	toolChoice := json.RawMessage(`{"type":"function","name":"lookup"}`)
	parallel := json.RawMessage(`true`)
	stream := true
	maxOutputTokens := uint(128)

	req := &dto.OpenAIResponsesRequest{
		Model:             "gpt-test",
		Input:             input,
		Instructions:      instructions,
		Tools:             tools,
		ToolChoice:        toolChoice,
		ParallelToolCalls: parallel,
		Stream:            &stream,
		MaxOutputTokens:   &maxOutputTokens,
		Reasoning:         &dto.Reasoning{Effort: "low"},
	}

	out, err := ResponsesRequestToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Equal(t, "gpt-test", out.Model)
	require.Len(t, out.Messages, 4)
	require.Equal(t, "system", out.Messages[0].Role)
	require.Equal(t, "be concise", out.Messages[0].StringContent())
	require.Equal(t, "user", out.Messages[1].Role)
	require.Len(t, out.Messages[1].ParseContent(), 2)
	require.Equal(t, "assistant", out.Messages[2].Role)
	require.Len(t, out.Messages[2].ParseToolCalls(), 1)
	require.Equal(t, "tool", out.Messages[3].Role)
	require.Equal(t, "call_1", out.Messages[3].ToolCallId)
	require.Equal(t, maxOutputTokens, *out.MaxCompletionTokens)
	require.Equal(t, "low", out.ReasoningEffort)
	require.NotNil(t, out.ParallelTooCalls)
	require.True(t, *out.ParallelTooCalls)
	require.Len(t, out.Tools, 1)
	require.Equal(t, "lookup", out.Tools[0].Function.Name)
	require.Equal(t, "function", out.ToolChoice.(map[string]any)["type"])
}

func TestChatCompletionsResponseToResponsesResponse(t *testing.T) {
	usage := dto.Usage{
		PromptTokens:     3,
		CompletionTokens: 4,
		TotalTokens:      7,
	}
	resp := &dto.OpenAITextResponse{
		Id:      "chatcmpl-abc",
		Model:   "gpt-test",
		Object:  "chat.completion",
		Created: int64(123),
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index: 0,
				Message: dto.Message{
					Role:    "assistant",
					Content: "hello",
				},
				FinishReason: "stop",
			},
		},
		Usage: usage,
	}

	out, outUsage, err := ChatCompletionsResponseToResponsesResponse(resp, "")
	require.NoError(t, err)
	require.Equal(t, "resp_abc", out.ID)
	require.Equal(t, "response", out.Object)
	require.Equal(t, "gpt-test", out.Model)
	require.Equal(t, 123, out.CreatedAt)
	require.Len(t, out.Output, 1)
	require.Equal(t, "message", out.Output[0].Type)
	require.Equal(t, "assistant", out.Output[0].Role)
	require.Equal(t, "hello", out.Output[0].Content[0].Text)
	require.NotNil(t, out.Usage)
	require.Equal(t, 3, out.Usage.InputTokens)
	require.Equal(t, 4, out.Usage.OutputTokens)
	require.Equal(t, 7, outUsage.TotalTokens)
}
