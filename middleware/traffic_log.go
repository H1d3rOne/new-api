package middleware

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

type trafficCaptureWriter struct {
	gin.ResponseWriter
	body      strings.Builder
	size      int64
	limit     int64
	truncated bool
	mu        sync.Mutex
}

func (w *trafficCaptureWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *trafficCaptureWriter) WriteString(data string) (int, error) {
	w.captureString(data)
	return w.ResponseWriter.WriteString(data)
}

func (w *trafficCaptureWriter) capture(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.size += int64(len(data))
	if w.limit <= 0 {
		if len(data) > 0 {
			w.truncated = true
		}
		return
	}
	remaining := int(w.limit) - w.body.Len()
	if remaining <= 0 {
		if len(data) > 0 {
			w.truncated = true
		}
		return
	}
	if len(data) > remaining {
		_, _ = w.body.Write(data[:remaining])
		w.truncated = true
		return
	}
	_, _ = w.body.Write(data)
}

func (w *trafficCaptureWriter) captureString(data string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.size += int64(len(data))
	if w.limit <= 0 {
		if data != "" {
			w.truncated = true
		}
		return
	}
	remaining := int(w.limit) - w.body.Len()
	if remaining <= 0 {
		if data != "" {
			w.truncated = true
		}
		return
	}
	if len(data) > remaining {
		_, _ = w.body.WriteString(data[:remaining])
		w.truncated = true
		return
	}
	_, _ = w.body.WriteString(data)
}

func (w *trafficCaptureWriter) captured() (body string, size int64, truncated bool, truncatedBytes int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	raw := w.body.String()
	size = w.size
	truncated = w.truncated || int64(len(raw)) < size
	if truncated && size > int64(len(raw)) {
		truncatedBytes = size - int64(len(raw))
	}
	return bodyBytesToText([]byte(raw)), size, truncated, truncatedBytes
}

func TrafficLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.TrafficLogEnabled || isWebsocketRequest(c) {
			c.Next()
			return
		}

		startTime := time.Now()
		requestHeaders := trafficLogRequestHeaders(c)
		captureWriter := &trafficCaptureWriter{
			ResponseWriter: c.Writer,
			limit:          int64(common.TrafficLogMaxBodyBytes),
		}
		c.Writer = captureWriter

		c.Next()

		if !shouldRecordTrafficLog(c) {
			return
		}

		if rewrittenRequestHeaders := trafficLogInterceptRequestHeaders(c); rewrittenRequestHeaders != "" {
			requestHeaders = rewrittenRequestHeaders
		}
		requestBody, requestSize, requestTruncated, requestTruncatedBytes := readStoredRequestBody(c)
		responseBody, responseSize, responseTruncated, responseTruncatedBytes := captureWriter.captured()

		userId := c.GetInt("id")
		trafficLog := &model.TrafficLog{
			CreatedAt:                  startTime.Unix(),
			UserId:                     userId,
			Username:                   c.GetString("username"),
			TokenId:                    c.GetInt("token_id"),
			TokenName:                  c.GetString("token_name"),
			ModelName:                  c.GetString("original_model"),
			ChannelId:                  c.GetInt("channel_id"),
			Group:                      c.GetString("group"),
			Ip:                         trafficLogClientIP(c, userId),
			RequestId:                  c.GetString(common.RequestIdKey),
			UpstreamRequestId:          c.GetString(common.UpstreamRequestIdKey),
			Method:                     c.Request.Method,
			Path:                       trafficLogPath(c),
			RequestURL:                 trafficLogURL(c),
			StatusCode:                 c.Writer.Status(),
			IsStream:                   c.GetBool("is_stream"),
			RequestContentType:         c.GetHeader("Content-Type"),
			ResponseContentType:        c.Writer.Header().Get("Content-Type"),
			RequestHeaders:             requestHeaders,
			ResponseHeaders:            trafficLogHeaders(c.Writer.Header()),
			RequestBody:                requestBody,
			ResponseBody:               responseBody,
			RequestBodySize:            requestSize,
			ResponseBodySize:           responseSize,
			RequestBodyTruncated:       requestTruncated,
			ResponseBodyTruncated:      responseTruncated,
			RequestBodyTruncatedBytes:  requestTruncatedBytes,
			ResponseBodyTruncatedBytes: responseTruncatedBytes,
			DurationMs:                 time.Since(startTime).Milliseconds(),
			UserAgent:                  common.LocalLogPreview(c.Request.UserAgent()),
		}
		if trafficLog.Username == "" {
			trafficLog.Username, _ = model.GetUsernameById(userId, false)
		}
		if trafficLog.ModelName == "" {
			trafficLog.ModelName = c.Query("model")
		}

		gopool.Go(func() {
			if err := model.RecordTrafficLog(trafficLog); err != nil {
				common.SysLog("failed to record traffic log: " + err.Error())
			}
		})
	}
}

func shouldRecordTrafficLog(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if c.Request.Method == http.MethodOptions {
		return false
	}
	if c.GetHeader("X-New-Api-Traffic-Replay") != "" {
		return false
	}
	if c.GetBool(service.TrafficLiveInterceptSkipLogKey) {
		return false
	}
	if c.GetString(RouteTagKey) != "relay" {
		return false
	}
	return c.GetInt("id") > 0
}

func isWebsocketRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.EqualFold(c.GetHeader("Upgrade"), "websocket")
}

func trafficLogClientIP(c *gin.Context, userId int) string {
	if userId == 0 {
		return ""
	}
	if setting, err := model.GetUserSetting(userId, false); err == nil && setting.RecordIpLog {
		return c.ClientIP()
	}
	return ""
}

func trafficLogPath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}

func trafficLogURL(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.RequestURI()
}

func trafficLogRequestHeaders(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	values := trafficLogHeaderValues(c.Request.Header)
	if c.Request.Host != "" {
		values["Host"] = c.Request.Host
	}
	if c.Request.ContentLength > 0 {
		values["Content-Length"] = strconv.FormatInt(c.Request.ContentLength, 10)
	}
	return trafficLogMarshalHeaders(values)
}

func trafficLogHeaders(header http.Header) string {
	return trafficLogMarshalHeaders(trafficLogHeaderValues(header))
}

func trafficLogHeaderValues(header http.Header) map[string]string {
	values := make(map[string]string, len(header))
	if len(header) == 0 {
		return values
	}
	for key := range header {
		if shouldMaskTrafficLogHeader(key) {
			values[key] = "[redacted]"
			continue
		}
		values[key] = header.Get(key)
	}
	return values
}

func trafficLogMarshalHeaders(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	data, err := common.Marshal(values)
	if err != nil {
		common.SysLog("failed to marshal traffic log headers: " + err.Error())
		return ""
	}
	return string(data)
}

func trafficLogInterceptRequestHeaders(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, ok := c.Get(service.TrafficInterceptLoggedRequestHeadersKey)
	if !ok || value == nil {
		return ""
	}
	headers, ok := value.(map[string]string)
	if !ok {
		return ""
	}
	return trafficLogMarshalHeaders(headers)
}

func shouldMaskTrafficLogHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "x-api-key", "x-goog-api-key", "mj-api-secret", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func readStoredRequestBody(c *gin.Context) (body string, size int64, truncated bool, truncatedBytes int64) {
	if body, size, truncated, truncatedBytes, ok := trafficLogInterceptRequestBody(c); ok {
		return body, size, truncated, truncatedBytes
	}
	storageValue, exists := c.Get(common.KeyBodyStorage)
	if !exists || storageValue == nil {
		return "", 0, false, 0
	}
	storage, ok := storageValue.(common.BodyStorage)
	if !ok || storage == nil {
		return "", 0, false, 0
	}

	size = storage.Size()
	limit := int64(common.TrafficLogMaxBodyBytes)
	if limit <= 0 {
		return "", size, size > 0, size
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return fmt.Sprintf("[failed to read request body: %s]", err.Error()), size, false, 0
	}
	data, err := io.ReadAll(io.LimitReader(storage, limit+1))
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil && err == nil {
		err = seekErr
	}
	if err != nil {
		return fmt.Sprintf("[failed to read request body: %s]", err.Error()), size, false, 0
	}
	if int64(len(data)) > limit {
		data = data[:limit]
		truncated = true
	}
	if size > int64(len(data)) {
		truncated = true
		truncatedBytes = size - int64(len(data))
	}
	return bodyBytesToText(data), size, truncated, truncatedBytes
}

func trafficLogInterceptRequestBody(c *gin.Context) (body string, size int64, truncated bool, truncatedBytes int64, ok bool) {
	if c == nil {
		return "", 0, false, 0, false
	}
	value, exists := c.Get(service.TrafficInterceptLoggedRequestBodyKey)
	if !exists {
		return "", 0, false, 0, false
	}
	text, ok := value.(string)
	if !ok {
		return "", 0, false, 0, false
	}
	data := []byte(text)
	size = int64(len(data))
	limit := int64(common.TrafficLogMaxBodyBytes)
	if limit <= 0 {
		return "", size, size > 0, size, true
	}
	if size > limit {
		data = data[:limit]
		truncated = true
		truncatedBytes = size - limit
	}
	return bodyBytesToText(data), size, truncated, truncatedBytes, true
}

func bodyBytesToText(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return "[base64]" + base64.StdEncoding.EncodeToString(data)
}
