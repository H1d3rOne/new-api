package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const trafficReplayTimeout = 120 * time.Second

type TrafficReplayRequest struct {
	Method      string            `json:"method"`
	RequestURL  string            `json:"request_url"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	ContentType string            `json:"content_type"`
}

type TrafficReplayResponse struct {
	StatusCode     int               `json:"status_code"`
	ContentType    string            `json:"content_type"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
	BodySize       int64             `json:"body_size"`
	BodyTruncated  bool              `json:"body_truncated"`
	TruncatedBytes int64             `json:"truncated_bytes"`
	DurationMs     int64             `json:"duration_ms"`
}

func ReplayTrafficLog(log *model.TrafficLog, input TrafficReplayRequest) (*TrafficReplayResponse, error) {
	if log == nil {
		return nil, errors.New("traffic log is required")
	}
	if log.TokenId <= 0 {
		return nil, errors.New("traffic log has no replayable token")
	}
	token, err := model.GetTokenById(log.TokenId)
	if err != nil {
		return nil, err
	}
	if token == nil || token.Key == "" {
		return nil, errors.New("traffic log token is not available")
	}

	method := strings.ToUpper(strings.TrimSpace(input.Method))
	if method == "" {
		method = strings.ToUpper(strings.TrimSpace(log.Method))
	}
	if method == "" {
		method = http.MethodPost
	}

	requestURI, err := trafficReplayRequestURI(log, input.RequestURL)
	if err != nil {
		return nil, err
	}
	targetURL := trafficReplayBaseURL() + requestURI

	requestBody := trafficReplayBodyBytes(input.Body)
	req, err := http.NewRequest(method, targetURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(len(requestBody))
	applyTrafficReplayHeaders(req.Header, input.Headers)
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = strings.TrimSpace(log.RequestContentType)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer sk-"+token.Key)
	req.Header.Set("X-New-Api-Traffic-Replay", strconv.Itoa(log.Id))

	client := &http.Client{
		Timeout:   trafficReplayTimeout,
		Transport: &http.Transport{Proxy: nil},
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, size, truncated, truncatedBytes, err := readTrafficReplayResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return &TrafficReplayResponse{
		StatusCode:     resp.StatusCode,
		ContentType:    resp.Header.Get("Content-Type"),
		Headers:        headerToStringMap(resp.Header),
		Body:           body,
		BodySize:       size,
		BodyTruncated:  truncated,
		TruncatedBytes: truncatedBytes,
		DurationMs:     time.Since(start).Milliseconds(),
	}, nil
}

func trafficReplayRequestURI(log *model.TrafficLog, override string) (string, error) {
	raw := strings.TrimSpace(override)
	if raw == "" && log != nil {
		raw = strings.TrimSpace(log.RequestURL)
	}
	if raw == "" && log != nil {
		raw = strings.TrimSpace(log.Path)
	}
	if raw == "" {
		return "", errors.New("request URL is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() {
		raw = parsed.RequestURI()
		parsed, err = url.Parse(raw)
		if err != nil {
			return "", err
		}
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
		parsed, err = url.Parse(raw)
		if err != nil {
			return "", err
		}
	}
	cleanTrafficReplayQuery(parsed)
	requestURI := parsed.RequestURI()
	if requestURI == "" {
		requestURI = raw
	}
	if !isTrafficReplayRelayPath(parsed.Path) {
		return "", errors.New("only relay traffic can be replayed")
	}
	return requestURI, nil
}

func cleanTrafficReplayQuery(u *url.URL) {
	if u == nil || u.RawQuery == "" {
		return
	}
	query := u.Query()
	for _, key := range []string{"key", "api_key", "access_token", "token"} {
		query.Del(key)
	}
	u.RawQuery = query.Encode()
}

func isTrafficReplayRelayPath(path string) bool {
	return trafficReplayHasPathPrefix(path, "/v1") ||
		trafficReplayHasPathPrefix(path, "/v1beta") ||
		trafficReplayHasPathPrefix(path, "/mj") ||
		trafficReplayHasPathPrefix(path, "/suno") ||
		isTrafficReplayModeMjPath(path)
}

func trafficReplayHasPathPrefix(path string, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func isTrafficReplayModeMjPath(path string) bool {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 || segments[1] != "mj" {
		return false
	}
	switch strings.ToLower(segments[0]) {
	case "", "api":
		return false
	default:
		return true
	}
}

func trafficReplayBaseURL() string {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" && common.Port != nil {
		port = strconv.Itoa(*common.Port)
	}
	if port == "" {
		port = "3000"
	}
	return "http://127.0.0.1:" + port
}

func applyTrafficReplayHeaders(header http.Header, values map[string]string) {
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" || shouldSkipTrafficReplayHeader(key) {
			continue
		}
		header.Set(key, value)
	}
}

func shouldSkipTrafficReplayHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "x-api-key", "x-goog-api-key", "mj-api-secret",
		"host", "content-length", "accept-encoding", "connection", "cookie",
		"set-cookie", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func trafficReplayBodyBytes(body string) []byte {
	if strings.HasPrefix(body, "[base64]") {
		if data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(body, "[base64]")); err == nil {
			return data
		}
	}
	return []byte(body)
}

func readTrafficReplayResponseBody(reader io.Reader) (body string, size int64, truncated bool, truncatedBytes int64, err error) {
	if reader == nil {
		return "", 0, false, 0, nil
	}
	limit := int64(common.TrafficLogMaxBodyBytes)
	if limit <= 0 {
		size, err = io.Copy(io.Discard, reader)
		return "", size, size > 0, size, err
	}
	var buf bytes.Buffer
	size, err = io.Copy(&buf, io.LimitReader(reader, limit+1))
	if err != nil {
		return "", size, false, 0, err
	}
	data := buf.Bytes()
	if int64(len(data)) > limit {
		data = data[:limit]
		truncated = true
	}
	if remaining, copyErr := io.Copy(io.Discard, reader); copyErr != nil {
		return "", size, false, 0, copyErr
	} else if remaining > 0 {
		size += remaining
		truncated = true
	}
	if truncated && size > int64(len(data)) {
		truncatedBytes = size - int64(len(data))
	}
	return bodyBytesToTrafficReplayText(data), size, truncated, truncatedBytes, nil
}

func bodyBytesToTrafficReplayText(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return "[base64]" + base64.StdEncoding.EncodeToString(data)
}
