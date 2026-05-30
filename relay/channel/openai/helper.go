package openai

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

// 辅助函数
func HandleStreamFormat(c *gin.Context, info *relaycommon.RelayInfo, data string, forceFormat bool, thinkToContent bool) error {
	info.SendResponseCount++

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		return sendStreamData(c, info, data, forceFormat, thinkToContent)
	case types.RelayFormatOpenAIResponses:
		return handleResponsesFormat(c, data, info)
	case types.RelayFormatClaude:
		return handleClaudeFormat(c, data, info)
	case types.RelayFormatGemini:
		return handleGeminiFormat(c, data, info)
	}
	return nil
}

func handleClaudeFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		return err
	}

	if streamResponse.Usage != nil {
		info.ClaudeConvertInfo.Usage = streamResponse.Usage
	}
	claudeResponses := service.StreamResponseOpenAI2Claude(&streamResponse, info)
	for _, resp := range claudeResponses {
		helper.ClaudeData(c, *resp)
	}
	return nil
}

func handleGeminiFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
		return err
	}

	geminiResponse := service.StreamResponseOpenAI2Gemini(&streamResponse, info)

	// 如果返回 nil，表示没有实际内容，跳过发送
	if geminiResponse == nil {
		return nil
	}

	geminiResponseStr, err := common.Marshal(geminiResponse)
	if err != nil {
		logger.LogError(c, "failed to marshal gemini response: "+err.Error())
		return err
	}

	// send gemini format response
	c.Render(-1, common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
	_ = helper.FlushWriter(c)
	return nil
}

func ProcessStreamResponse(streamResponse dto.ChatCompletionsStreamResponse, responseTextBuilder *strings.Builder, toolCount *int) error {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Delta.GetContentString())
		responseTextBuilder.WriteString(choice.Delta.GetReasoningContent())
		if choice.Delta.ToolCalls != nil {
			if len(choice.Delta.ToolCalls) > *toolCount {
				*toolCount = len(choice.Delta.ToolCalls)
			}
			for _, tool := range choice.Delta.ToolCalls {
				responseTextBuilder.WriteString(tool.Function.Name)
				responseTextBuilder.WriteString(tool.Function.Arguments)
			}
		}
	}
	return nil
}

func processTokenData(relayMode int, data string, responseTextBuilder *strings.Builder, toolCount *int) error {
	switch relayMode {
	case relayconstant.RelayModeChatCompletions:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return err
		}
		return ProcessStreamResponse(streamResponse, responseTextBuilder, toolCount)
	case relayconstant.RelayModeCompletions:
		var streamResponse dto.CompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return err
		}
		processCompletionsStreamResponse(streamResponse, responseTextBuilder)
	}
	return nil
}

func processCompletionsStreamResponse(streamResponse dto.CompletionsStreamResponse, responseTextBuilder *strings.Builder) {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Text)
	}
}

func handleLastResponse(lastStreamData string, responseId *string, createAt *int64,
	systemFingerprint *string, model *string, usage **dto.Usage,
	containStreamUsage *bool, info *relaycommon.RelayInfo,
	shouldSendLastResp *bool) error {

	var lastStreamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &lastStreamResponse); err != nil {
		return err
	}

	*responseId = lastStreamResponse.Id
	*createAt = lastStreamResponse.Created
	*systemFingerprint = lastStreamResponse.GetSystemFingerprint()
	*model = lastStreamResponse.Model

	if service.ValidUsage(lastStreamResponse.Usage) {
		*containStreamUsage = true
		*usage = lastStreamResponse.Usage
		if !info.ShouldIncludeUsage {
			*shouldSendLastResp = lo.SomeBy(lastStreamResponse.Choices, func(choice dto.ChatCompletionsStreamResponseChoice) bool {
				return choice.Delta.GetContentString() != "" || choice.Delta.GetReasoningContent() != ""
			})
		}
	}

	return nil
}

func HandleFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, lastStreamData string,
	responseId string, createAt int64, model string, systemFingerprint string,
	usage *dto.Usage, containStreamUsage bool) {

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if info.ShouldIncludeUsage && !containStreamUsage {
			response := helper.GenerateFinalUsageResponse(responseId, createAt, model, *usage)
			response.SetSystemFingerprint(systemFingerprint)
			helper.ObjectData(c, response)
		}
		helper.Done(c)

	case types.RelayFormatOpenAIResponses:
		if strings.TrimSpace(lastStreamData) != "" {
			if err := handleResponsesFormat(c, lastStreamData, info); err != nil {
				common.SysLog("send responses last stream response failed: " + err.Error())
			}
		}
		if err := handleResponsesFinalFormat(c, info, usage); err != nil {
			common.SysLog("send responses final response failed: " + err.Error())
		}

	case types.RelayFormatClaude:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &streamResponse); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			return
		}

		info.ClaudeConvertInfo.Usage = usage

		claudeResponses := service.StreamResponseOpenAI2Claude(&streamResponse, info)
		for _, resp := range claudeResponses {
			_ = helper.ClaudeData(c, *resp)
		}
		info.ClaudeConvertInfo.Done = true

	case types.RelayFormatGemini:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &streamResponse); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			return
		}

		// 这里处理的是 openai 最后一个流响应，其 delta 为空，有 finish_reason 字段
		// 因此相比较于 google 官方的流响应，由 openai 转换而来会多一个 parts 为空，finishReason 为 STOP 的响应
		// 而包含最后一段文本输出的响应（倒数第二个）的 finishReason 为 null
		// 暂不知是否有程序会不兼容。

		geminiResponse := service.StreamResponseOpenAI2Gemini(&streamResponse, info)

		// openai 流响应开头的空数据
		if geminiResponse == nil {
			return
		}

		geminiResponseStr, err := common.Marshal(geminiResponse)
		if err != nil {
			common.SysLog("error marshalling gemini response: " + err.Error())
			return
		}

		// 发送最终的 Gemini 响应
		c.Render(-1, common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
		_ = helper.FlushWriter(c)
	}
}

func sendResponsesStreamData(c *gin.Context, streamResponse dto.ResponsesStreamResponse, data string) {
	if data == "" {
		return
	}
	helper.ResponseChunkData(c, streamResponse, data)
}

const responsesStreamConvertStateKey = "responses_stream_convert_state"

type responsesStreamConvertState struct {
	ResponseID  string
	CreatedAt   int64
	Model       string
	SentCreated bool
	Text        strings.Builder
	ToolCalls   map[int]*responsesStreamToolCall
}

type responsesStreamToolCall struct {
	Index     int
	ID        string
	CallID    string
	Generated string
	Name      string
	Arguments strings.Builder
	SentAdded bool
}

func getResponsesStreamConvertState(c *gin.Context, info *relaycommon.RelayInfo) *responsesStreamConvertState {
	if c != nil {
		if value, exists := c.Get(responsesStreamConvertStateKey); exists {
			if state, ok := value.(*responsesStreamConvertState); ok {
				return state
			}
		}
	}
	model := ""
	if info != nil {
		model = info.UpstreamModelName
	}
	state := &responsesStreamConvertState{
		ResponseID: helper.GetResponseID(c),
		CreatedAt:  common.GetTimestamp(),
		Model:      model,
		ToolCalls:  make(map[int]*responsesStreamToolCall),
	}
	if c != nil {
		c.Set(responsesStreamConvertStateKey, state)
	}
	return state
}

func handleResponsesFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
		return err
	}

	state := getResponsesStreamConvertState(c, info)
	state.updateMetadata(&streamResponse)
	for _, event := range state.chatChunkToResponsesEvents(&streamResponse) {
		if err := sendResponsesStreamEvent(c, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *responsesStreamConvertState) updateMetadata(chunk *dto.ChatCompletionsStreamResponse) {
	if s == nil || chunk == nil {
		return
	}
	if chunk.Id != "" {
		s.ResponseID = chunk.Id
	}
	if chunk.Created != 0 {
		s.CreatedAt = chunk.Created
	}
	if chunk.Model != "" {
		s.Model = chunk.Model
	}
}

func (s *responsesStreamConvertState) chatChunkToResponsesEvents(chunk *dto.ChatCompletionsStreamResponse) []*dto.ResponsesStreamResponse {
	if s == nil || chunk == nil {
		return nil
	}
	events := make([]*dto.ResponsesStreamResponse, 0)
	if !s.SentCreated {
		events = append(events, &dto.ResponsesStreamResponse{
			Type:     "response.created",
			Response: s.snapshotResponse("in_progress", nil),
		})
		s.SentCreated = true
	}

	for _, choice := range chunk.Choices {
		if reasoning := choice.Delta.GetReasoningContent(); reasoning != "" {
			events = append(events, &dto.ResponsesStreamResponse{
				Type:  "response.reasoning_summary_text.delta",
				Delta: reasoning,
			})
		}
		if content := choice.Delta.GetContentString(); content != "" {
			s.Text.WriteString(content)
			events = append(events, &dto.ResponsesStreamResponse{
				Type:         "response.output_text.delta",
				Delta:        content,
				OutputIndex:  common.GetPointer(0),
				ContentIndex: common.GetPointer(0),
			})
		}

		for _, toolCall := range choice.Delta.ToolCalls {
			idx := 0
			if toolCall.Index != nil {
				idx = *toolCall.Index
			}
			stateTool := s.toolCall(idx)
			if toolCall.ID != "" {
				stateTool.ID = toolCall.ID
				stateTool.CallID = toolCall.ID
			}
			if toolCall.Function.Name != "" {
				stateTool.Name = toolCall.Function.Name
			}
			if !stateTool.SentAdded && (stateTool.CallID != "" || stateTool.Name != "") {
				events = append(events, &dto.ResponsesStreamResponse{
					Type:        dto.ResponsesOutputTypeItemAdded,
					Item:        stateTool.responsesOutput(),
					OutputIndex: common.GetPointer(idx),
				})
				stateTool.SentAdded = true
			}
			if toolCall.Function.Arguments != "" {
				stateTool.Arguments.WriteString(toolCall.Function.Arguments)
				events = append(events, &dto.ResponsesStreamResponse{
					Type:        "response.function_call_arguments.delta",
					Delta:       toolCall.Function.Arguments,
					ItemID:      stateTool.itemID(),
					OutputIndex: common.GetPointer(idx),
				})
			}
		}
	}
	return events
}

func (s *responsesStreamConvertState) toolCall(index int) *responsesStreamToolCall {
	if s.ToolCalls == nil {
		s.ToolCalls = make(map[int]*responsesStreamToolCall)
	}
	if tool, ok := s.ToolCalls[index]; ok {
		return tool
	}
	tool := &responsesStreamToolCall{Index: index}
	s.ToolCalls[index] = tool
	return tool
}

func (t *responsesStreamToolCall) itemID() string {
	if t == nil {
		return ""
	}
	if t.ID != "" {
		return "fc_" + t.ID
	}
	if t.CallID != "" {
		return "fc_" + t.CallID
	}
	if t.Generated == "" {
		t.Generated = common.GetUUID()
	}
	return "fc_" + t.Generated
}

func (t *responsesStreamToolCall) callID() string {
	if t == nil {
		return ""
	}
	if t.CallID != "" {
		return t.CallID
	}
	if t.ID != "" {
		return t.ID
	}
	return t.itemID()
}

func (t *responsesStreamToolCall) responsesOutput() *dto.ResponsesOutput {
	if t == nil {
		return nil
	}
	argsRaw, _ := common.Marshal(t.Arguments.String())
	return &dto.ResponsesOutput{
		Type:      "function_call",
		ID:        t.itemID(),
		Status:    "completed",
		CallId:    t.callID(),
		Name:      t.Name,
		Arguments: argsRaw,
	}
}

func (s *responsesStreamConvertState) snapshotResponse(status string, usage *dto.Usage) *dto.OpenAIResponsesResponse {
	if s == nil {
		return nil
	}
	statusRaw, _ := common.Marshal(status)
	return &dto.OpenAIResponsesResponse{
		ID:        responsesIDFromChatID(s.ResponseID),
		Object:    "response",
		CreatedAt: int(s.CreatedAt),
		Status:    statusRaw,
		Model:     s.Model,
		Output:    s.outputs(),
		Usage:     responsesUsageFromChatUsage(usage),
	}
}

func (s *responsesStreamConvertState) outputs() []dto.ResponsesOutput {
	if s == nil {
		return nil
	}
	outputs := make([]dto.ResponsesOutput, 0, 1+len(s.ToolCalls))
	if s.Text.Len() > 0 || len(s.ToolCalls) == 0 {
		outputs = append(outputs, dto.ResponsesOutput{
			Type:   "message",
			ID:     "msg_" + strings.TrimPrefix(responsesIDFromChatID(s.ResponseID), "resp_"),
			Status: "completed",
			Role:   "assistant",
			Content: []dto.ResponsesOutputContent{
				{
					Type: "output_text",
					Text: s.Text.String(),
				},
			},
		})
	}
	for _, tool := range s.ToolCalls {
		if output := tool.responsesOutput(); output != nil {
			outputs = append(outputs, *output)
		}
	}
	return outputs
}

func handleResponsesFinalFormat(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) error {
	state := getResponsesStreamConvertState(c, info)
	for _, event := range state.toolCallDoneEvents() {
		if err := sendResponsesStreamEvent(c, event); err != nil {
			return err
		}
	}
	return sendResponsesStreamEvent(c, &dto.ResponsesStreamResponse{
		Type:     "response.completed",
		Response: state.snapshotResponse("completed", usage),
	})
}

func (s *responsesStreamConvertState) toolCallDoneEvents() []*dto.ResponsesStreamResponse {
	if s == nil || len(s.ToolCalls) == 0 {
		return nil
	}
	events := make([]*dto.ResponsesStreamResponse, 0, len(s.ToolCalls))
	for idx, tool := range s.ToolCalls {
		if !tool.SentAdded {
			continue
		}
		events = append(events, &dto.ResponsesStreamResponse{
			Type:        dto.ResponsesOutputTypeItemDone,
			Item:        tool.responsesOutput(),
			OutputIndex: common.GetPointer(idx),
		})
	}
	return events
}

func sendResponsesStreamEvent(c *gin.Context, event *dto.ResponsesStreamResponse) error {
	if event == nil {
		return nil
	}
	data, err := common.Marshal(event)
	if err != nil {
		return err
	}
	helper.ResponseChunkData(c, *event, string(data))
	return nil
}

func responsesIDFromChatID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "resp_" + common.GetUUID()
	}
	if strings.HasPrefix(id, "resp_") {
		return id
	}
	if strings.HasPrefix(id, "chatcmpl-") {
		return "resp_" + strings.TrimPrefix(id, "chatcmpl-")
	}
	return "resp_" + id
}

func responsesUsageFromChatUsage(usage *dto.Usage) *dto.Usage {
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
