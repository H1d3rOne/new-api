package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestFillTrafficLogHeaderFallbacks(t *testing.T) {
	log := &TrafficLog{
		RequestContentType:  "application/json",
		ResponseContentType: "text/event-stream",
	}

	fillTrafficLogHeaderFallbacks(log)

	var requestHeaders map[string]string
	require.NoError(t, common.Unmarshal([]byte(log.RequestHeaders), &requestHeaders))
	require.Equal(t, "application/json", requestHeaders["Content-Type"])

	var responseHeaders map[string]string
	require.NoError(t, common.Unmarshal([]byte(log.ResponseHeaders), &responseHeaders))
	require.Equal(t, "text/event-stream", responseHeaders["Content-Type"])
}
