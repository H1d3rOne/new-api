package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTrafficLogRequestHeadersIncludesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	req.ContentLength = 123
	c.Request = req

	raw := trafficLogRequestHeaders(c)
	var headers map[string]string
	require.NoError(t, common.Unmarshal([]byte(raw), &headers))
	require.Equal(t, "application/json", headers["Content-Type"])
	require.Equal(t, "Bearer secret", headers["Authorization"])
	require.Equal(t, "example.com", headers["Host"])
	require.Equal(t, "123", headers["Content-Length"])
}

func TestTrafficLogUsesInterceptRewrittenRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldLimit := common.TrafficLogMaxBodyBytes
	common.TrafficLogMaxBodyBytes = 1024
	t.Cleanup(func() {
		common.TrafficLogMaxBodyBytes = oldLimit
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(service.TrafficInterceptLoggedRequestBodyKey, `{"messages":[{"role":"user","content":"rewritten"}]}`)

	body, size, truncated, truncatedBytes := readStoredRequestBody(c)

	require.Equal(t, `{"messages":[{"role":"user","content":"rewritten"}]}`, body)
	require.Equal(t, int64(len(body)), size)
	require.False(t, truncated)
	require.Equal(t, int64(0), truncatedBytes)
}

func TestTrafficLogUsesInterceptRewrittenRequestHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(service.TrafficInterceptLoggedRequestHeadersKey, map[string]string{
		"Authorization":  "Bearer rewritten",
		"Content-Length": "52",
		"Content-Type":   "application/json",
	})

	raw := trafficLogInterceptRequestHeaders(c)
	var headers map[string]string
	require.NoError(t, common.Unmarshal([]byte(raw), &headers))
	require.Equal(t, "Bearer rewritten", headers["Authorization"])
	require.Equal(t, "52", headers["Content-Length"])
	require.Equal(t, "application/json", headers["Content-Type"])
}
