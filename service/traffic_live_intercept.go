package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const (
	TrafficLiveInterceptSkipLogKey = "traffic_live_intercept_skip_log"
	trafficLiveSettingsOptionKey   = "TrafficLiveInterceptSettings"
	trafficLiveDecisionAccept      = "accept"
	trafficLiveDecisionBlock       = "block"
)

type TrafficLiveInterceptSettings struct {
	Enabled           bool     `json:"enabled"`
	UserIds           []int    `json:"user_ids"`
	Usernames         []string `json:"usernames"`
	InterceptRequest  bool     `json:"intercept_request"`
	InterceptResponse bool     `json:"intercept_response"`
	TimeoutSeconds    int      `json:"timeout_seconds"`
}

type TrafficLiveInterceptEvent struct {
	Id              string            `json:"id"`
	Phase           string            `json:"phase"`
	CreatedAt       int64             `json:"created_at"`
	UserId          int               `json:"user_id"`
	Username        string            `json:"username"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	RequestURL      string            `json:"request_url"`
	ModelName       string            `json:"model_name"`
	ContentType     string            `json:"content_type"`
	Headers         map[string]string `json:"headers"`
	Body            string            `json:"body"`
	StatusCode      int               `json:"status_code"`
	ResponseHeaders map[string]string `json:"response_headers"`
	ResponseBody    string            `json:"response_body"`
	ResponseType    string            `json:"response_content_type"`
}

type TrafficLiveInterceptDecision struct {
	Decision        string            `json:"decision"`
	Headers         map[string]string `json:"headers"`
	Body            string            `json:"body"`
	HeadersModified bool              `json:"headers_modified"`
	BodyModified    bool              `json:"body_modified"`
}

type trafficLivePendingEvent struct {
	event    *TrafficLiveInterceptEvent
	decision chan TrafficLiveInterceptDecision
}

var (
	trafficLiveMu       sync.Mutex
	trafficLiveLoaded   bool
	trafficLiveSettings TrafficLiveInterceptSettings
	trafficLiveEvents   = map[string]*trafficLivePendingEvent{}
	trafficLiveSeq      uint64
)

func defaultTrafficLiveInterceptSettings() TrafficLiveInterceptSettings {
	return TrafficLiveInterceptSettings{
		TimeoutSeconds: 60,
	}
}

func normalizeTrafficLiveInterceptSettings(settings TrafficLiveInterceptSettings) TrafficLiveInterceptSettings {
	if settings.TimeoutSeconds <= 0 {
		settings.TimeoutSeconds = 60
	}
	if settings.TimeoutSeconds > 600 {
		settings.TimeoutSeconds = 600
	}
	settings.UserIds = uniquePositiveInts(settings.UserIds)
	settings.Usernames = uniqueNonEmptyStrings(settings.Usernames)
	return settings
}

func GetTrafficLiveInterceptSettings() TrafficLiveInterceptSettings {
	trafficLiveMu.Lock()
	defer trafficLiveMu.Unlock()
	if trafficLiveLoaded {
		return normalizeTrafficLiveInterceptSettings(trafficLiveSettings)
	}
	settings := defaultTrafficLiveInterceptSettings()
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[trafficLiveSettingsOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if strings.TrimSpace(raw) != "" {
		if err := common.Unmarshal([]byte(raw), &settings); err != nil {
			common.SysLog("failed to unmarshal traffic live intercept settings: " + err.Error())
		}
	}
	trafficLiveSettings = normalizeTrafficLiveInterceptSettings(settings)
	trafficLiveLoaded = true
	return trafficLiveSettings
}

func UpdateTrafficLiveInterceptSettings(settings TrafficLiveInterceptSettings) (TrafficLiveInterceptSettings, error) {
	settings = normalizeTrafficLiveInterceptSettings(settings)
	data, err := common.Marshal(settings)
	if err != nil {
		return settings, err
	}
	if err := model.UpdateOption(trafficLiveSettingsOptionKey, string(data)); err != nil {
		return settings, err
	}
	trafficLiveMu.Lock()
	trafficLiveSettings = settings
	trafficLiveLoaded = true
	trafficLiveMu.Unlock()
	return settings, nil
}

func ListTrafficLiveInterceptEvents() []*TrafficLiveInterceptEvent {
	trafficLiveMu.Lock()
	defer trafficLiveMu.Unlock()
	events := make([]*TrafficLiveInterceptEvent, 0, len(trafficLiveEvents))
	for _, pending := range trafficLiveEvents {
		events = append(events, pending.event)
	}
	return events
}

func DecideTrafficLiveInterceptEvent(id string, decision TrafficLiveInterceptDecision) error {
	decision.Decision = strings.ToLower(strings.TrimSpace(decision.Decision))
	if decision.Decision != trafficLiveDecisionAccept && decision.Decision != trafficLiveDecisionBlock {
		return errors.New("invalid live intercept decision")
	}
	trafficLiveMu.Lock()
	pending := trafficLiveEvents[id]
	if pending == nil {
		trafficLiveMu.Unlock()
		return errors.New("live intercept event not found")
	}
	delete(trafficLiveEvents, id)
	trafficLiveMu.Unlock()
	select {
	case pending.decision <- decision:
	default:
	}
	return nil
}

func ApplyTrafficLiveInboundInterceptor(c *gin.Context) bool {
	settings := GetTrafficLiveInterceptSettings()
	if !settings.Enabled || !settings.InterceptRequest || !trafficLiveMatches(c, settings) {
		return false
	}
	event := trafficLiveRequestEvent(c)
	decision := waitTrafficLiveDecision(event, settings.TimeoutSeconds)
	if decision.Decision == trafficLiveDecisionAccept {
		if err := applyTrafficLiveRequestDecision(c, decision); err != nil {
			c.Set(TrafficLiveInterceptSkipLogKey, true)
			writeTrafficInterceptBlock(c, http.StatusInternalServerError, "application/json", `{"error":"failed to apply live intercept request edits"}`, nil)
			return true
		}
		return false
	}
	c.Set(TrafficLiveInterceptSkipLogKey, true)
	writeTrafficInterceptBlock(c, http.StatusForbidden, "application/json", `{"error":"request blocked by live intercept"}`, nil)
	return true
}

func ApplyTrafficLiveResponseInterceptor(c *gin.Context, req *http.Request, resp *http.Response, info *relaycommon.RelayInfo) error {
	settings := GetTrafficLiveInterceptSettings()
	if resp == nil || !settings.Enabled || !settings.InterceptResponse || !trafficLiveMatches(c, settings) {
		return nil
	}
	var data []byte
	if resp.Body != nil {
		var err error
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(data))
	}
	event := trafficLiveResponseEvent(c, req, resp, info, data)
	decision := waitTrafficLiveDecision(event, settings.TimeoutSeconds)
	if decision.Decision == trafficLiveDecisionAccept {
		applyTrafficLiveResponseDecision(resp, data, decision)
		return nil
	}
	c.Set(TrafficLiveInterceptSkipLogKey, true)
	body := `{"error":"response blocked by live intercept"}`
	resp.StatusCode = http.StatusForbidden
	resp.Status = fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	resp.Header = http.Header{}
	resp.Header.Set("Content-Type", "application/json")
	setHTTPResponseBody(resp, body)
	return nil
}

func ApplyTrafficLiveGeneratedResponseInterceptor(c *gin.Context, statusCode int, headers http.Header, body string) (int, http.Header, string, bool) {
	settings := GetTrafficLiveInterceptSettings()
	if !settings.Enabled || !settings.InterceptResponse || !trafficLiveMatches(c, settings) {
		return statusCode, headers, body, false
	}
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	if headers == nil {
		headers = http.Header{}
	}
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	event := trafficLiveBaseEvent(c, "response")
	event.StatusCode = statusCode
	event.ResponseType = headers.Get("Content-Type")
	event.ResponseHeaders = redactedHeaderMap(headers)
	event.ResponseBody = trafficLiveBodyPreview([]byte(body))
	decision := waitTrafficLiveDecision(event, settings.TimeoutSeconds)
	if decision.Decision == trafficLiveDecisionAccept {
		nextHeaders := cloneHTTPHeader(headers)
		if decision.HeadersModified {
			replaceTrafficLiveHeaderValues(nextHeaders, decision.Headers)
		}
		if decision.BodyModified {
			body = decision.Body
			nextHeaders.Set("Content-Length", strconv.Itoa(len(body)))
			nextHeaders.Del("Transfer-Encoding")
		}
		return statusCode, nextHeaders, body, true
	}
	c.Set(TrafficLiveInterceptSkipLogKey, true)
	blockBody := `{"error":"response blocked by live intercept"}`
	blockHeaders := http.Header{}
	blockHeaders.Set("Content-Type", "application/json")
	blockHeaders.Set("Content-Length", strconv.Itoa(len(blockBody)))
	return http.StatusForbidden, blockHeaders, blockBody, true
}

func waitTrafficLiveDecision(event *TrafficLiveInterceptEvent, timeoutSeconds int) TrafficLiveInterceptDecision {
	if event == nil {
		return TrafficLiveInterceptDecision{Decision: trafficLiveDecisionBlock}
	}
	pending := &trafficLivePendingEvent{
		event:    event,
		decision: make(chan TrafficLiveInterceptDecision, 1),
	}
	trafficLiveMu.Lock()
	trafficLiveEvents[event.Id] = pending
	trafficLiveMu.Unlock()
	timer := time.NewTimer(time.Duration(timeoutSeconds) * time.Second)
	defer timer.Stop()
	select {
	case decision := <-pending.decision:
		return decision
	case <-timer.C:
		trafficLiveMu.Lock()
		delete(trafficLiveEvents, event.Id)
		trafficLiveMu.Unlock()
		return TrafficLiveInterceptDecision{Decision: trafficLiveDecisionBlock}
	}
}

func trafficLiveMatches(c *gin.Context, settings TrafficLiveInterceptSettings) bool {
	if len(settings.UserIds) == 0 && len(settings.Usernames) == 0 {
		return false
	}
	userId := 0
	username := ""
	if c != nil {
		userId = c.GetInt("id")
		username = c.GetString("username")
	}
	for _, id := range settings.UserIds {
		if id != 0 && id == userId {
			return true
		}
	}
	for _, name := range settings.Usernames {
		if name != "" && name == username {
			return true
		}
	}
	return false
}

func trafficLiveRequestEvent(c *gin.Context) *TrafficLiveInterceptEvent {
	event := trafficLiveBaseEvent(c, "request")
	if c != nil && c.Request != nil {
		event.ContentType = c.GetHeader("Content-Type")
		event.Headers = redactedHeaderMap(c.Request.Header)
		event.Body = trafficLiveBodyPreview([]byte(readGinRequestBody(c)))
	}
	return event
}

func trafficLiveResponseEvent(c *gin.Context, req *http.Request, resp *http.Response, info *relaycommon.RelayInfo, body []byte) *TrafficLiveInterceptEvent {
	event := trafficLiveBaseEvent(c, "response")
	if req != nil {
		event.Method = req.Method
		if req.URL != nil {
			event.RequestURL = req.URL.String()
			event.Path = req.URL.Path
		}
		event.Headers = redactedHeaderMap(req.Header)
	}
	if info != nil && info.OriginModelName != "" {
		event.ModelName = info.OriginModelName
	}
	if resp != nil {
		event.StatusCode = resp.StatusCode
		event.ResponseType = resp.Header.Get("Content-Type")
		event.ResponseHeaders = redactedHeaderMap(resp.Header)
		event.ResponseBody = trafficLiveBodyPreview(body)
	}
	return event
}

func applyTrafficLiveRequestDecision(c *gin.Context, decision TrafficLiveInterceptDecision) error {
	if c == nil || c.Request == nil {
		return nil
	}
	if decision.HeadersModified {
		if c.Request.Header == nil {
			c.Request.Header = http.Header{}
		}
		replaceTrafficLiveHeaderValues(c.Request.Header, decision.Headers)
	}
	if decision.BodyModified {
		storage, err := common.CreateBodyStorage([]byte(decision.Body))
		if err != nil {
			return err
		}
		if old, exists := c.Get(common.KeyBodyStorage); exists && old != nil {
			if oldStorage, ok := old.(common.BodyStorage); ok {
				_ = oldStorage.Close()
			}
		}
		if _, err := storage.Seek(0, io.SeekStart); err != nil {
			_ = storage.Close()
			return err
		}
		c.Set(common.KeyBodyStorage, storage)
		c.Request.Body = io.NopCloser(storage)
		c.Request.ContentLength = storage.Size()
		c.Request.Header.Set("Content-Length", strconv.FormatInt(storage.Size(), 10))
	}
	return nil
}

func applyTrafficLiveResponseDecision(resp *http.Response, originalBody []byte, decision TrafficLiveInterceptDecision) {
	if resp == nil {
		return
	}
	if resp.Header == nil {
		resp.Header = http.Header{}
	}
	if decision.HeadersModified {
		replaceTrafficLiveHeaderValues(resp.Header, decision.Headers)
	}
	if decision.BodyModified {
		setHTTPResponseBody(resp, decision.Body)
		return
	}
	resp.Body = io.NopCloser(bytes.NewReader(originalBody))
	if decision.HeadersModified {
		resp.ContentLength = int64(len(originalBody))
		resp.Header.Set("Content-Length", strconv.Itoa(len(originalBody)))
		resp.Header.Del("Transfer-Encoding")
	}
}

func replaceTrafficLiveHeaderValues(header http.Header, values map[string]string) {
	for key := range header {
		header.Del(key)
	}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" || shouldSkipTrafficInterceptHeader(key) {
			continue
		}
		header.Set(key, value)
	}
}

func cloneHTTPHeader(header http.Header) http.Header {
	out := make(http.Header, len(header))
	for key, values := range header {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func trafficLiveBaseEvent(c *gin.Context, phase string) *TrafficLiveInterceptEvent {
	id := atomic.AddUint64(&trafficLiveSeq, 1)
	event := &TrafficLiveInterceptEvent{
		Id:              strconv.FormatUint(id, 10),
		Phase:           phase,
		CreatedAt:       time.Now().Unix(),
		Headers:         map[string]string{},
		ResponseHeaders: map[string]string{},
	}
	if c != nil {
		event.UserId = c.GetInt("id")
		event.Username = c.GetString("username")
		event.ModelName = c.GetString("original_model")
		if c.Request != nil {
			event.Method = c.Request.Method
			if c.Request.URL != nil {
				event.Path = c.Request.URL.Path
				event.RequestURL = c.Request.URL.RequestURI()
			}
		}
	}
	return event
}

func redactedHeaderMap(header http.Header) map[string]string {
	out := make(map[string]string, len(header))
	for key := range header {
		if shouldMaskTrafficLiveHeader(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = header.Get(key)
	}
	return out
}

func shouldMaskTrafficLiveHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "x-api-key", "x-goog-api-key", "mj-api-secret", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func trafficLiveBodyPreview(data []byte) string {
	limit := common.TrafficLogMaxBodyBytes
	if limit > 0 && len(data) > limit {
		data = data[:limit]
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return "[base64]" + base64.StdEncoding.EncodeToString(data)
}

func uniquePositiveInts(values []int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
