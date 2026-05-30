package common

import "github.com/QuantumNous/new-api/constant"

// GetEndpointTypesByChannelType 获取渠道最优先端点类型。
// 文本模型通过端点转换同时支持 OpenAI Chat Completions、OpenAI Responses 和 Anthropic Messages。
func GetEndpointTypesByChannelType(channelType int, modelName string) []constant.EndpointType {
	var endpointTypes []constant.EndpointType
	switch channelType {
	case constant.ChannelTypeJina:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeJinaRerank}
	//case constant.ChannelTypeMidjourney, constant.ChannelTypeMidjourneyPlus:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeMidjourney}
	//case constant.ChannelTypeSunoAPI:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeSuno}
	//case constant.ChannelTypeKling:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeKling}
	//case constant.ChannelTypeJimeng:
	//	endpointTypes = []constant.EndpointType{constant.EndpointTypeJimeng}
	case constant.ChannelTypeAws:
		fallthrough
	case constant.ChannelTypeAnthropic:
		endpointTypes = withTextCompatibleEndpointTypes(constant.EndpointTypeAnthropic)
	case constant.ChannelTypeVertexAi:
		fallthrough
	case constant.ChannelTypeGemini:
		endpointTypes = withTextCompatibleEndpointTypes(constant.EndpointTypeGemini)
	case constant.ChannelTypeOpenRouter:
		endpointTypes = withTextCompatibleEndpointTypes(constant.EndpointTypeOpenAI)
	case constant.ChannelTypeXai:
		endpointTypes = withTextCompatibleEndpointTypes(constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse)
	case constant.ChannelTypeSora:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
	default:
		if IsOpenAIResponseOnlyModel(modelName) {
			endpointTypes = withTextCompatibleEndpointTypes(constant.EndpointTypeOpenAIResponse)
		} else {
			endpointTypes = withTextCompatibleEndpointTypes(constant.EndpointTypeOpenAI)
		}
	}
	if IsImageGenerationModel(modelName) {
		// add to first
		endpointTypes = append([]constant.EndpointType{constant.EndpointTypeImageGeneration}, endpointTypes...)
	}
	return endpointTypes
}

func withTextCompatibleEndpointTypes(preferred ...constant.EndpointType) []constant.EndpointType {
	endpointTypes := make([]constant.EndpointType, 0, len(preferred)+3)
	add := func(endpointType constant.EndpointType) {
		for _, existing := range endpointTypes {
			if existing == endpointType {
				return
			}
		}
		endpointTypes = append(endpointTypes, endpointType)
	}
	for _, endpointType := range preferred {
		add(endpointType)
	}
	add(constant.EndpointTypeOpenAI)
	add(constant.EndpointTypeOpenAIResponse)
	add(constant.EndpointTypeAnthropic)
	return endpointTypes
}
