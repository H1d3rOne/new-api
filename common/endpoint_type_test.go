package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestExpandTextCompatibleEndpointTypes(t *testing.T) {
	endpoints := ExpandTextCompatibleEndpointTypes([]constant.EndpointType{constant.EndpointTypeOpenAI})

	require.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeAnthropic,
	}, endpoints)
}

func TestExpandTextCompatibleEndpointTypesSkipsNonTextEndpoint(t *testing.T) {
	endpoints := ExpandTextCompatibleEndpointTypes([]constant.EndpointType{constant.EndpointTypeEmbeddings})

	require.Equal(t, []constant.EndpointType{constant.EndpointTypeEmbeddings}, endpoints)
}
