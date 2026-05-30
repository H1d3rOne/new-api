package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if len(req.Input) == 0 {
		return nil, errors.New("input is required")
	}

	out := &dto.GeneralOpenAIRequest{
		Model:                req.Model,
		Stream:               req.Stream,
		StreamOptions:        req.StreamOptions,
		MaxCompletionTokens:  req.MaxOutputTokens,
		Temperature:          req.Temperature,
		TopP:                 req.TopP,
		TopLogProbs:          req.TopLogProbs,
		User:                 req.User,
		Metadata:             req.Metadata,
		Store:                req.Store,
		PromptCacheKey:       rawJSONToString(req.PromptCacheKey),
		PromptCacheRetention: req.PromptCacheRetention,
		SafetyIdentifier:     req.SafetyIdentifier,
	}

	if req.ServiceTier != "" {
		if raw, err := common.Marshal(req.ServiceTier); err == nil {
			out.ServiceTier = raw
		}
	}

	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}

	if req.ParallelToolCalls != nil {
		if parallel, ok := rawJSONBool(req.ParallelToolCalls); ok {
			out.ParallelTooCalls = common.GetPointer(parallel)
		}
	}

	out.Tools = responsesToolsToChatTools(req.Tools)
	out.ToolChoice = responsesToolChoiceToChatToolChoice(req.ToolChoice)
	out.ResponseFormat = responsesTextToChatResponseFormat(req.Text)

	if webSearchOptions := responsesWebSearchOptions(req.Tools); webSearchOptions != nil {
		out.WebSearchOptions = webSearchOptions
	}

	out.Messages = responsesInputToChatMessages(req.Input)
	if instructions := rawJSONToString(req.Instructions); strings.TrimSpace(instructions) != "" {
		systemMessage := dto.Message{Role: "system"}
		systemMessage.SetStringContent(instructions)
		out.Messages = append([]dto.Message{systemMessage}, out.Messages...)
	}

	if len(out.Messages) == 0 {
		return nil, errors.New("no convertible input messages")
	}
	return out, nil
}

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	text := ExtractOutputTextFromResponses(resp)

	usage := &dto.Usage{}
	if resp.Usage != nil {
		if resp.Usage.InputTokens != 0 {
			usage.PromptTokens = resp.Usage.InputTokens
			usage.InputTokens = resp.Usage.InputTokens
		}
		if resp.Usage.OutputTokens != 0 {
			usage.CompletionTokens = resp.Usage.OutputTokens
			usage.OutputTokens = resp.Usage.OutputTokens
		}
		if resp.Usage.TotalTokens != 0 {
			usage.TotalTokens = resp.Usage.TotalTokens
		} else {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		if resp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = resp.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.ImageTokens = resp.Usage.InputTokensDetails.ImageTokens
			usage.PromptTokensDetails.AudioTokens = resp.Usage.InputTokensDetails.AudioTokens
		}
		if resp.Usage.CompletionTokenDetails.ReasoningTokens != 0 {
			usage.CompletionTokenDetails.ReasoningTokens = resp.Usage.CompletionTokenDetails.ReasoningTokens
		}
	}

	created := resp.CreatedAt

	var toolCalls []dto.ToolCallResponse
	if text == "" && len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if out.Type != "function_call" {
				continue
			}
			name := strings.TrimSpace(out.Name)
			if name == "" {
				continue
			}
			callId := strings.TrimSpace(out.CallId)
			if callId == "" {
				callId = strings.TrimSpace(out.ID)
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID:   callId,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: out.ArgumentsString(),
				},
			})
		}
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
		msg.Content = ""
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse, id string) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	responseID := normalizeResponsesID(id, resp.Id)
	createdAt := responseCreatedAt(resp.Created)
	output := make([]dto.ResponsesOutput, 0, len(resp.Choices))

	for choiceIndex, choice := range resp.Choices {
		toolCalls := choice.Message.ParseToolCalls()
		if len(toolCalls) > 0 {
			for callIndex, toolCall := range toolCalls {
				callID := strings.TrimSpace(toolCall.ID)
				if callID == "" {
					callID = fmt.Sprintf("call_%d_%d", choiceIndex, callIndex)
				}
				argsRaw, _ := common.Marshal(toolCall.Function.Arguments)
				output = append(output, dto.ResponsesOutput{
					Type:      "function_call",
					ID:        "fc_" + callID,
					Status:    "completed",
					CallId:    callID,
					Name:      toolCall.Function.Name,
					Arguments: argsRaw,
				})
			}
			continue
		}

		text := choice.Message.StringContent()
		if text == "" && choice.Message.Content != nil {
			text = interfaceToString(choice.Message.Content)
		}
		output = append(output, dto.ResponsesOutput{
			Type:   "message",
			ID:     fmt.Sprintf("msg_%s_%d", strings.TrimPrefix(responseID, "resp_"), choiceIndex),
			Status: "completed",
			Role:   "assistant",
			Content: []dto.ResponsesOutputContent{
				{
					Type: "output_text",
					Text: text,
				},
			},
		})
	}

	usage := chatUsageToResponsesUsage(&resp.Usage)
	completed, _ := common.Marshal("completed")
	out := &dto.OpenAIResponsesResponse{
		ID:                responseID,
		Object:            "response",
		CreatedAt:         createdAt,
		Status:            json.RawMessage(completed),
		Model:             resp.Model,
		Output:            output,
		ParallelToolCalls: len(output) > 1,
		Usage:             usage,
	}
	return out, usage, nil
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder

	// Prefer assistant message outputs.
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func responsesInputToChatMessages(input json.RawMessage) []dto.Message {
	if len(input) == 0 {
		return nil
	}
	switch common.GetJsonType(input) {
	case "string":
		var text string
		if err := common.Unmarshal(input, &text); err != nil {
			return nil
		}
		message := dto.Message{Role: "user"}
		message.SetStringContent(text)
		return []dto.Message{message}
	case "array":
	default:
		return nil
	}

	var items []any
	if err := common.Unmarshal(input, &items); err != nil {
		return nil
	}
	messages := make([]dto.Message, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			message := dto.Message{Role: "user"}
			message.SetStringContent(v)
			messages = append(messages, message)
		case map[string]any:
			messages = append(messages, responsesInputItemToChatMessages(v)...)
		}
	}
	return messages
}

func responsesInputItemToChatMessages(item map[string]any) []dto.Message {
	itemType := strings.TrimSpace(common.Interface2String(item["type"]))
	role := strings.TrimSpace(common.Interface2String(item["role"]))
	if role == "" {
		role = "user"
	}

	switch itemType {
	case "function_call":
		callID := strings.TrimSpace(common.Interface2String(item["call_id"]))
		if callID == "" {
			callID = strings.TrimSpace(common.Interface2String(item["id"]))
		}
		toolCall := dto.ToolCallRequest{
			ID:   callID,
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      common.Interface2String(item["name"]),
				Arguments: interfaceToString(item["arguments"]),
			},
		}
		message := dto.Message{Role: "assistant"}
		message.SetStringContent("")
		message.SetToolCalls([]dto.ToolCallRequest{toolCall})
		return []dto.Message{message}
	case "function_call_output":
		message := dto.Message{
			Role:       "tool",
			ToolCallId: common.Interface2String(item["call_id"]),
		}
		message.SetStringContent(interfaceToString(item["output"]))
		return []dto.Message{message}
	case "input_text":
		message := dto.Message{Role: "user"}
		message.SetStringContent(common.Interface2String(item["text"]))
		return []dto.Message{message}
	case "message", "":
		message := dto.Message{Role: role}
		setChatMessageContentFromResponses(&message, item["content"])
		return []dto.Message{message}
	default:
		if strings.HasPrefix(itemType, "input_") {
			message := dto.Message{Role: "user"}
			setChatMessageContentFromResponses(&message, []any{item})
			return []dto.Message{message}
		}
	}
	return nil
}

func setChatMessageContentFromResponses(message *dto.Message, content any) {
	if message == nil {
		return
	}
	switch v := content.(type) {
	case nil:
		message.SetStringContent("")
	case string:
		message.SetStringContent(v)
	case []any:
		parts := make([]dto.MediaContent, 0, len(v))
		var textOnly strings.Builder
		allText := true
		for _, partAny := range v {
			part, ok := partAny.(map[string]any)
			if !ok {
				allText = false
				continue
			}
			media, ok := responsesContentPartToChatMedia(part)
			if !ok {
				allText = false
				continue
			}
			parts = append(parts, media)
			if media.Type == dto.ContentTypeText {
				textOnly.WriteString(media.Text)
			} else {
				allText = false
			}
		}
		if len(parts) == 0 {
			message.SetStringContent("")
			return
		}
		if allText {
			message.SetStringContent(textOnly.String())
			return
		}
		message.SetMediaContent(parts)
	default:
		message.SetStringContent(interfaceToString(v))
	}
}

func responsesContentPartToChatMedia(part map[string]any) (dto.MediaContent, bool) {
	partType := strings.TrimSpace(common.Interface2String(part["type"]))
	switch partType {
	case "input_text", "output_text", "text":
		return dto.MediaContent{
			Type: dto.ContentTypeText,
			Text: common.Interface2String(part["text"]),
		}, true
	case "input_image", "image_url":
		return dto.MediaContent{
			Type:     dto.ContentTypeImageURL,
			ImageUrl: normalizeResponsesImageURL(part["image_url"]),
		}, true
	case "input_file", "file":
		file := map[string]any{}
		if fileID := common.Interface2String(part["file_id"]); fileID != "" {
			file["file_id"] = fileID
		}
		if fileURL := common.Interface2String(part["file_url"]); fileURL != "" {
			file["file_data"] = fileURL
		}
		if len(file) == 0 {
			if fileAny, ok := part["file"]; ok {
				return dto.MediaContent{Type: dto.ContentTypeFile, File: fileAny}, true
			}
			return dto.MediaContent{}, false
		}
		return dto.MediaContent{Type: dto.ContentTypeFile, File: file}, true
	case "input_audio":
		return dto.MediaContent{
			Type:       dto.ContentTypeInputAudio,
			InputAudio: part["input_audio"],
		}, true
	default:
		if text := common.Interface2String(part["text"]); text != "" {
			return dto.MediaContent{Type: dto.ContentTypeText, Text: text}, true
		}
	}
	return dto.MediaContent{}, false
}

func normalizeResponsesImageURL(v any) any {
	switch image := v.(type) {
	case string:
		return &dto.MessageImageUrl{Url: image}
	case map[string]any:
		if url := common.Interface2String(image["url"]); url != "" {
			return &dto.MessageImageUrl{
				Url:      url,
				Detail:   common.Interface2String(image["detail"]),
				MimeType: common.Interface2String(image["mime_type"]),
			}
		}
	}
	return v
}

func responsesToolsToChatTools(raw json.RawMessage) []dto.ToolCallRequest {
	if len(raw) == 0 {
		return nil
	}
	var tools []map[string]any
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil
	}
	out := make([]dto.ToolCallRequest, 0, len(tools))
	for _, tool := range tools {
		if common.Interface2String(tool["type"]) != "function" {
			continue
		}
		name := common.Interface2String(tool["name"])
		if name == "" {
			continue
		}
		out = append(out, dto.ToolCallRequest{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        name,
				Description: common.Interface2String(tool["description"]),
				Parameters:  tool["parameters"],
			},
		})
	}
	return out
}

func responsesWebSearchOptions(raw json.RawMessage) *dto.WebSearchOptions {
	if len(raw) == 0 {
		return nil
	}
	var tools []map[string]any
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil
	}
	for _, tool := range tools {
		toolType := common.Interface2String(tool["type"])
		if toolType != dto.BuildInToolWebSearchPreview {
			continue
		}
		searchContextSize := common.Interface2String(tool["search_context_size"])
		if searchContextSize == "" {
			searchContextSize = "medium"
		}
		return &dto.WebSearchOptions{SearchContextSize: searchContextSize}
	}
	return nil
}

func responsesToolChoiceToChatToolChoice(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	switch common.GetJsonType(raw) {
	case "string":
		var value string
		if err := common.Unmarshal(raw, &value); err == nil {
			return value
		}
	case "object":
		var m map[string]any
		if err := common.Unmarshal(raw, &m); err != nil {
			return nil
		}
		if common.Interface2String(m["type"]) == "function" {
			name := common.Interface2String(m["name"])
			if name != "" {
				return map[string]any{
					"type": "function",
					"function": map[string]any{
						"name": name,
					},
				}
			}
		}
		return m
	}
	return nil
}

func responsesTextToChatResponseFormat(raw json.RawMessage) *dto.ResponseFormat {
	if len(raw) == 0 {
		return nil
	}
	var text map[string]any
	if err := common.Unmarshal(raw, &text); err != nil {
		return nil
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		return nil
	}
	formatType := common.Interface2String(format["type"])
	if formatType == "" {
		return nil
	}
	out := &dto.ResponseFormat{Type: formatType}
	if formatType == "json_schema" {
		if rawSchema, err := common.Marshal(format); err == nil {
			out.JsonSchema = rawSchema
		}
	}
	return out
}

func chatUsageToResponsesUsage(usage *dto.Usage) *dto.Usage {
	if usage == nil {
		return nil
	}
	out := *usage
	if out.InputTokens == 0 {
		out.InputTokens = out.PromptTokens
	}
	if out.OutputTokens == 0 {
		out.OutputTokens = out.CompletionTokens
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.InputTokens + out.OutputTokens
	}
	if out.PromptTokens == 0 {
		out.PromptTokens = out.InputTokens
	}
	if out.CompletionTokens == 0 {
		out.CompletionTokens = out.OutputTokens
	}
	if out.InputTokensDetails == nil {
		out.InputTokensDetails = &out.PromptTokensDetails
	}
	return &out
}

func rawJSONBool(raw json.RawMessage) (bool, bool) {
	var value bool
	if err := common.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

func rawJSONToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return common.JsonRawMessageToString(raw)
}

func interfaceToString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		if data, err := common.Marshal(value); err == nil {
			return string(data)
		}
		return fmt.Sprintf("%v", value)
	}
}

func normalizeResponsesID(ids ...string) string {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if strings.HasPrefix(id, "resp_") {
			return id
		}
		if strings.HasPrefix(id, "chatcmpl-") {
			return "resp_" + strings.TrimPrefix(id, "chatcmpl-")
		}
		return "resp_" + id
	}
	return "resp_" + common.GetUUID()
}

func responseCreatedAt(created any) int {
	switch v := created.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return int(i)
		}
	}
	return int(common.GetTimestamp())
}
