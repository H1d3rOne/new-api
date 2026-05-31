package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/dop251/goja"
	"github.com/expr-lang/expr"
	"github.com/gin-gonic/gin"
)

const trafficInterceptCacheTTL = 10 * time.Second
const trafficInterceptScriptTimeout = 500 * time.Millisecond
const trafficInterceptOriginalRequestBodyKey = "traffic_intercept_original_request_body"
const trafficInterceptConsumedRuleMatchesKey = "traffic_intercept_consumed_rule_matches"
const trafficInterceptStreamContentRewriteKey = "traffic_intercept_stream_content_rewrite"
const TrafficInterceptLoggedRequestBodyKey = "traffic_intercept_logged_request_body"
const TrafficInterceptLoggedRequestHeadersKey = "traffic_intercept_logged_request_headers"

type trafficInterceptCachedRule struct {
	Rule               *model.TrafficInterceptRule
	pathRegex          *regexp.Regexp
	modelRegex         *regexp.Regexp
	responsePathRegex  *regexp.Regexp
	responseModelRegex *regexp.Regexp
}

type TrafficInterceptSettings struct {
	Enabled bool `json:"enabled"`
}

type TrafficInterceptRequestContext struct {
	Method      string            `expr:"Method"`
	Path        string            `expr:"Path"`
	URL         string            `expr:"URL"`
	UpstreamURL string            `expr:"UpstreamURL"`
	Model       string            `expr:"Model"`
	UserId      int               `expr:"UserId"`
	Username    string            `expr:"Username"`
	TokenName   string            `expr:"TokenName"`
	Group       string            `expr:"Group"`
	ChannelId   int               `expr:"ChannelId"`
	ContentType string            `expr:"ContentType"`
	Body        string            `expr:"Body"`
	Header      map[string]string `expr:"Header"`
	Headers     map[string]string `expr:"Headers"`
}

type TrafficInterceptResponseContext struct {
	URL         string            `expr:"URL"`
	Status      int               `expr:"Status"`
	ContentType string            `expr:"ContentType"`
	Body        string            `expr:"Body"`
	Header      map[string]string `expr:"Header"`
	Headers     map[string]string `expr:"Headers"`
}

var (
	trafficInterceptCacheMu   sync.RWMutex
	trafficInterceptCacheTime time.Time
	trafficInterceptCache     []*trafficInterceptCachedRule
)

var consumeTrafficInterceptRuleMatch = model.ConsumeInterceptRuleMatch

var trafficInterceptHopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

func GetTrafficInterceptSettings() TrafficInterceptSettings {
	return TrafficInterceptSettings{Enabled: common.TrafficInterceptEnabled}
}

func UpdateTrafficInterceptSettings(settings TrafficInterceptSettings) error {
	if err := model.UpdateOption("TrafficInterceptEnabled", strconv.FormatBool(settings.Enabled)); err != nil {
		return err
	}
	InvalidateTrafficInterceptRulesCache()
	return nil
}

func InvalidateTrafficInterceptRulesCache() {
	trafficInterceptCacheMu.Lock()
	defer trafficInterceptCacheMu.Unlock()
	trafficInterceptCache = nil
	trafficInterceptCacheTime = time.Time{}
}

func loadTrafficInterceptRules() []*trafficInterceptCachedRule {
	if !common.TrafficInterceptEnabled {
		return nil
	}

	trafficInterceptCacheMu.RLock()
	if time.Since(trafficInterceptCacheTime) < trafficInterceptCacheTTL && trafficInterceptCache != nil {
		rules := trafficInterceptCache
		trafficInterceptCacheMu.RUnlock()
		return rules
	}
	trafficInterceptCacheMu.RUnlock()

	trafficInterceptCacheMu.Lock()
	defer trafficInterceptCacheMu.Unlock()
	if time.Since(trafficInterceptCacheTime) < trafficInterceptCacheTTL && trafficInterceptCache != nil {
		return trafficInterceptCache
	}

	enabledRules, err := model.GetEnabledInterceptRules()
	if err != nil {
		common.SysLog("failed to load traffic intercept rules: " + err.Error())
		trafficInterceptCache = nil
		trafficInterceptCacheTime = time.Now()
		return nil
	}

	cached := make([]*trafficInterceptCachedRule, 0, len(enabledRules))
	for _, rule := range enabledRules {
		cr := &trafficInterceptCachedRule{Rule: rule}

		if strings.TrimSpace(rule.PathPattern) != "" {
			re, err := regexp.Compile(rule.PathPattern)
			if err != nil {
				common.SysLog(fmt.Sprintf("traffic intercept rule %d: invalid path_pattern regex: %v", rule.Id, err))
			} else {
				cr.pathRegex = re
			}
		}

		if strings.TrimSpace(rule.ModelPattern) != "" {
			re, err := regexp.Compile(rule.ModelPattern)
			if err != nil {
				common.SysLog(fmt.Sprintf("traffic intercept rule %d: invalid model_pattern regex: %v", rule.Id, err))
			} else {
				cr.modelRegex = re
			}
		}

		if strings.TrimSpace(rule.ResponsePathPattern) != "" {
			re, err := regexp.Compile(rule.ResponsePathPattern)
			if err != nil {
				common.SysLog(fmt.Sprintf("traffic intercept rule %d: invalid response_path_pattern regex: %v", rule.Id, err))
			} else {
				cr.responsePathRegex = re
			}
		}

		if strings.TrimSpace(rule.ResponseModelPattern) != "" {
			re, err := regexp.Compile(rule.ResponseModelPattern)
			if err != nil {
				common.SysLog(fmt.Sprintf("traffic intercept rule %d: invalid response_model_pattern regex: %v", rule.Id, err))
			} else {
				cr.responseModelRegex = re
			}
		}

		cached = append(cached, cr)
	}

	trafficInterceptCache = cached
	trafficInterceptCacheTime = time.Now()
	return cached
}

func ApplyTrafficInboundInterceptor(c *gin.Context) bool {
	if c == nil || c.Request == nil || !common.TrafficInterceptEnabled || isTrafficInterceptWebsocket(c) {
		return false
	}
	if c.GetString("route_tag") != "relay" {
		return false
	}

	rules := loadTrafficInterceptRules()
	if len(rules) == 0 {
		return false
	}

	reqCtx := buildTrafficInterceptRequestContext(c, nil, nil, trafficRulesNeedRequestBody(rules, true))
	for _, cr := range rules {
		rule := cr.Rule
		if rule == nil || trafficInterceptRuleMatchLimitReached(cr) || !rule.InterceptRequest || !matchTrafficInterceptRequestRule(cr, reqCtx) {
			continue
		}

		if rule.BlockEnabled {
			if !trafficInterceptConsumeRuleMatch(c, cr) {
				continue
			}
			headers := trafficInterceptHeaderOpsMap(rule.ResponseHeaderOps, trafficInterceptEnv(reqCtx, nil))
			writeTrafficInterceptBlock(c, rule.BlockStatusCode, rule.BlockContentType, rule.BlockBody, headers)
			return true
		}

		if strings.TrimSpace(rule.RequestScript) != "" {
			if output, ran, err := runTrafficInterceptJSHook(rule.RequestScript, "onRequest", rule, reqCtx, nil); ran || err != nil {
				if err != nil {
					common.SysLog(fmt.Sprintf("traffic intercept rule %d: request script eval error: %v", rule.Id, err))
					continue
				}
				if block, ok := trafficInterceptScriptBlockAction(output, rule); ok {
					if !trafficInterceptConsumeRuleMatch(c, cr) {
						continue
					}
					writeTrafficInterceptBlock(c, block.status, block.contentType, block.body, block.headers)
					return true
				}
				continue
			}
			out, err := evalTrafficInterceptExpression(rule.RequestScript, trafficInterceptEnv(reqCtx, nil))
			if err != nil {
				common.SysLog(fmt.Sprintf("traffic intercept rule %d: request script eval error: %v", rule.Id, err))
				continue
			}
			if action := trafficInterceptMap(out); action != nil && truthy(action["block"]) {
				if !trafficInterceptConsumeRuleMatch(c, cr) {
					continue
				}
				status := anyToInt(action["status"], http.StatusForbidden)
				body := anyToString(action["body"], rule.BlockBody)
				headers := trafficInterceptStringMap(action["headers"])
				contentType := headers["Content-Type"]
				if contentType == "" {
					contentType = rule.BlockContentType
				}
				writeTrafficInterceptBlock(c, status, contentType, body, headers)
				return true
			}
		}
	}

	return false
}

func ApplyTrafficUpstreamRequestInterceptor(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	if req == nil || !common.TrafficInterceptEnabled {
		return nil
	}

	rules := loadTrafficInterceptRules()
	if len(rules) == 0 {
		return nil
	}

	reqCtx := buildTrafficInterceptRequestContext(c, req, info, trafficRulesNeedRequestBody(rules, true))
	if c != nil && reqCtx.Body != "" {
		c.Set(trafficInterceptOriginalRequestBodyKey, reqCtx.Body)
	}
	for _, cr := range rules {
		rule := cr.Rule
		if rule == nil || trafficInterceptRuleMatchLimitReached(cr) || !rule.InterceptRequest || !matchTrafficInterceptRequestRule(cr, reqCtx) {
			continue
		}
		if !trafficInterceptRuleHasRequestRewriteAction(rule) || !trafficInterceptConsumeRuleMatch(c, cr) {
			continue
		}

		env := trafficInterceptEnv(reqCtx, nil)

		if strings.TrimSpace(rule.RequestHeaderOps) != "" {
			applyTrafficInterceptHeaderOps(req.Header, rule.RequestHeaderOps, env)
			reqCtx.Header = headerToStringMap(req.Header)
			reqCtx.Headers = reqCtx.Header
			setTrafficInterceptLoggedRequest(c, reqCtx)
			env = trafficInterceptEnv(reqCtx, nil)
		}

		if trafficInterceptHasConfiguredJSONArray(rule.RequestMessageRewrites) && reqCtx.Body != "" {
			if body, ok := applyTrafficInterceptMessageContentRewrites(reqCtx.Body, rule.RequestMessageRewrites, env); ok {
				setHTTPRequestBody(req, body)
				reqCtx.Body = body
				reqCtx.Header = headerToStringMap(req.Header)
				reqCtx.Headers = reqCtx.Header
				setTrafficInterceptLoggedRequest(c, reqCtx)
				env = trafficInterceptEnv(reqCtx, nil)
			}
		}

		if strings.TrimSpace(rule.RequestScript) != "" {
			if output, ran, err := runTrafficInterceptJSHook(rule.RequestScript, "onRequest", rule, reqCtx, nil); ran || err != nil {
				if err != nil {
					common.SysLog(fmt.Sprintf("traffic intercept rule %d: request script eval error: %v", rule.Id, err))
					continue
				}
				applyTrafficInterceptScriptRequest(req, reqCtx, output.Request)
				setTrafficInterceptLoggedRequest(c, reqCtx)
				env = trafficInterceptEnv(reqCtx, nil)
			} else {
				out, err := evalTrafficInterceptExpression(rule.RequestScript, env)
				if err != nil {
					common.SysLog(fmt.Sprintf("traffic intercept rule %d: request script eval error: %v", rule.Id, err))
					continue
				}
				action := trafficInterceptMap(out)
				if len(action) == 0 {
					continue
				}
				if hasAction(action, "headers") {
					applyHeaderActionMap(req.Header, action["headers"])
					reqCtx.Header = headerToStringMap(req.Header)
					reqCtx.Headers = reqCtx.Header
					setTrafficInterceptLoggedRequest(c, reqCtx)
				}
			}
		}
	}

	for _, cr := range rules {
		rule := cr.Rule
		if rule == nil || trafficInterceptRuleMatchLimitReached(cr) || !rule.InterceptResponse || strings.TrimSpace(rule.ResponseScript) == "" || !matchTrafficInterceptResponseCandidateRule(cr, reqCtx) {
			continue
		}
		output, ran, err := runTrafficInterceptJSHook(rule.ResponseScript, "onRequest", rule, reqCtx, nil)
		if err != nil {
			common.SysLog(fmt.Sprintf("traffic intercept rule %d: response script onRequest eval error: %v", rule.Id, err))
			continue
		}
		if !ran {
			continue
		}
		if !trafficInterceptConsumeRuleMatch(c, cr) {
			continue
		}
		applyTrafficInterceptScriptRequest(req, reqCtx, output.Request)
		setTrafficInterceptLoggedRequest(c, reqCtx)
	}

	for _, cr := range rules {
		rule := cr.Rule
		if rule == nil || trafficInterceptRuleMatchLimitReached(cr) || !trafficInterceptRuleHasScriptAction(rule) || !matchTrafficInterceptRequestRule(cr, reqCtx) {
			continue
		}
		output, ran, err := runTrafficInterceptJSHook(rule.Script, "onRequest", rule, reqCtx, nil)
		if err != nil {
			common.SysLog(fmt.Sprintf("traffic intercept rule %d: script onRequest eval error: %v", rule.Id, err))
			continue
		}
		if !ran {
			continue
		}
		if !trafficInterceptConsumeRuleMatch(c, cr) {
			continue
		}
		applyTrafficInterceptScriptRequest(req, reqCtx, output.Request)
		setTrafficInterceptLoggedRequest(c, reqCtx)
	}

	return nil
}

func ApplyTrafficUpstreamResponseInterceptor(c *gin.Context, req *http.Request, resp *http.Response, info *relaycommon.RelayInfo) error {
	if resp == nil || !common.TrafficInterceptEnabled {
		return nil
	}

	rules := loadTrafficInterceptRules()
	if len(rules) == 0 {
		return nil
	}

	reqCtx := buildTrafficInterceptRequestContext(c, req, info, false)
	if trafficResponseRulesNeedRequestBody(rules) {
		reqCtx.Body = trafficInterceptOriginalRequestBody(c)
	}
	matched := make([]*trafficInterceptCachedRule, 0, len(rules))
	for _, cr := range rules {
		rule := cr.Rule
		if rule == nil || trafficInterceptRuleMatchLimitReached(cr) || (!rule.InterceptResponse && !trafficInterceptRuleHasScriptAction(rule)) || !matchTrafficInterceptResponseCandidateRule(cr, reqCtx) {
			continue
		}
		matched = append(matched, cr)
	}
	if len(matched) == 0 {
		return nil
	}

	isStream := false
	if info != nil && info.IsStream {
		isStream = true
	}
	if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		isStream = true
	}
	encodedBody := strings.TrimSpace(resp.Header.Get("Content-Encoding")) != ""
	needsBody := trafficRulesNeedResponseBody(matched)
	body := ""
	if needsBody && !encodedBody && resp.Body != nil {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		body = string(data)
		resp.Body = io.NopCloser(bytes.NewReader(data))
	}

	respCtx := &TrafficInterceptResponseContext{
		URL:         responseURL(req, resp),
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
		Header:      headerToStringMap(resp.Header),
	}
	respCtx.Headers = respCtx.Header

	for _, cr := range matched {
		rule := cr.Rule
		if !matchTrafficInterceptResponseCondition(cr, reqCtx, respCtx) {
			continue
		}

		if rule.InterceptResponse && trafficInterceptRuleHasResponseRewriteAction(rule, isStream, encodedBody) {
			if !trafficInterceptConsumeRuleMatch(c, cr) {
				continue
			}
			env := trafficInterceptEnv(reqCtx, respCtx)

			if strings.TrimSpace(rule.ResponseHeaderOps) != "" {
				applyTrafficInterceptHeaderOps(resp.Header, rule.ResponseHeaderOps, env)
				respCtx.Header = headerToStringMap(resp.Header)
				respCtx.Headers = respCtx.Header
				env = trafficInterceptEnv(reqCtx, respCtx)
			}

			if trafficInterceptHasResponseStructuredRewrite(rule) && !encodedBody {
				responseContentRewrite := rule.ResponseContentRewrite
				responseToolCallsRewrite := rule.ResponseToolCallsRewrite
				if isStream && strings.TrimSpace(responseContentRewrite) != "" {
					setTrafficInterceptStreamContentRewrite(c, trafficInterceptResponseContentRewriteValue(responseContentRewrite, env))
					responseContentRewrite = ""
				}
				if newBody, ok := applyTrafficInterceptResponseRewrites(
					respCtx.Body,
					responseContentRewrite,
					responseToolCallsRewrite,
					env,
				); ok {
					respCtx.Body = newBody
					env = trafficInterceptEnv(reqCtx, respCtx)
				}
			}

			if strings.TrimSpace(rule.ResponseStatusRewrite) != "" {
				if out, err := evalTrafficInterceptExpression(rule.ResponseStatusRewrite, env); err == nil {
					respCtx.Status = anyToInt(out, respCtx.Status)
					env = trafficInterceptEnv(reqCtx, respCtx)
				} else {
					common.SysLog(fmt.Sprintf("traffic intercept rule %d: response status rewrite error: %v", rule.Id, err))
				}
			}

			if strings.TrimSpace(rule.ResponseURLRewrite) != "" {
				if newURL, ok := evalTrafficInterceptString(rule.ResponseURLRewrite, env); ok {
					respCtx.URL = applyHTTPResponseURL(req, resp, newURL)
					env = trafficInterceptEnv(reqCtx, respCtx)
				}
			}

			if strings.TrimSpace(rule.ResponseScript) != "" {
				if output, ran, err := runTrafficInterceptJSHook(rule.ResponseScript, "onResponse", rule, reqCtx, respCtx); ran || err != nil {
					if err != nil {
						common.SysLog(fmt.Sprintf("traffic intercept rule %d: response script eval error: %v", rule.Id, err))
						continue
					}
					applyTrafficInterceptScriptRequest(req, reqCtx, output.Request)
					applyTrafficInterceptScriptResponse(req, resp, respCtx, output.Response, encodedBody)
					env = trafficInterceptEnv(reqCtx, respCtx)
				} else {
					out, err := evalTrafficInterceptExpression(rule.ResponseScript, trafficInterceptEnv(reqCtx, respCtx))
					if err != nil {
						common.SysLog(fmt.Sprintf("traffic intercept rule %d: response script eval error: %v", rule.Id, err))
						continue
					}
					action := trafficInterceptMap(out)
					if len(action) == 0 {
						continue
					}
					if hasAction(action, "headers") {
						applyHeaderActionMap(resp.Header, action["headers"])
						respCtx.Header = headerToStringMap(resp.Header)
						respCtx.Headers = respCtx.Header
					}
					if status, ok := action["status"]; ok {
						respCtx.Status = anyToInt(status, respCtx.Status)
					}
					if newURL, ok := actionString(action, "url"); ok {
						respCtx.URL = applyHTTPResponseURL(req, resp, newURL)
					}
					if newBody, ok := actionString(action, "body"); ok && !isStream && !encodedBody {
						respCtx.Body = newBody
					}
				}
			}
		}

		if trafficInterceptRuleHasScriptAction(rule) {
			output, ran, err := runTrafficInterceptJSHook(rule.Script, "onResponse", rule, reqCtx, respCtx)
			if err != nil {
				common.SysLog(fmt.Sprintf("traffic intercept rule %d: script onResponse eval error: %v", rule.Id, err))
				continue
			}
			if !ran {
				continue
			}
			if !trafficInterceptConsumeRuleMatch(c, cr) {
				continue
			}
			applyTrafficInterceptScriptRequest(req, reqCtx, output.Request)
			applyTrafficInterceptScriptResponse(req, resp, respCtx, output.Response, encodedBody)
		}
	}

	resp.StatusCode = respCtx.Status
	resp.Status = fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	if needsBody && !encodedBody {
		setHTTPResponseBody(resp, respCtx.Body)
	}
	return nil
}

func setTrafficInterceptStreamContentRewrite(c *gin.Context, content string) {
	if c == nil {
		return
	}
	c.Set(trafficInterceptStreamContentRewriteKey, content)
}

func GetTrafficInterceptStreamContentRewrite(c *gin.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	value, ok := c.Get(trafficInterceptStreamContentRewriteKey)
	if !ok {
		return "", false
	}
	content, ok := value.(string)
	return content, ok
}

func trafficInterceptRuleMatchLimitReached(cr *trafficInterceptCachedRule) bool {
	if cr == nil || cr.Rule == nil {
		return true
	}
	rule := cr.Rule
	return rule.MatchLimit > 0 && rule.MatchCount >= rule.MatchLimit
}

func trafficInterceptConsumeRuleMatch(c *gin.Context, cr *trafficInterceptCachedRule) bool {
	if cr == nil || cr.Rule == nil {
		return false
	}
	rule := cr.Rule
	if rule.MatchLimit <= 0 {
		return true
	}
	if trafficInterceptRuleMatchAlreadyConsumed(c, rule.Id) {
		return true
	}
	ok, err := consumeTrafficInterceptRuleMatch(rule.Id)
	if err != nil {
		common.SysLog(fmt.Sprintf("traffic intercept rule %d: consume match count error: %v", rule.Id, err))
		return false
	}
	if !ok {
		return false
	}
	trafficInterceptMarkRuleMatchConsumed(c, rule.Id)
	InvalidateTrafficInterceptRulesCache()
	return true
}

func trafficInterceptRuleMatchAlreadyConsumed(c *gin.Context, ruleId int) bool {
	if c == nil || ruleId <= 0 {
		return false
	}
	value, ok := c.Get(trafficInterceptConsumedRuleMatchesKey)
	if !ok {
		return false
	}
	consumed, ok := value.(map[int]struct{})
	if !ok {
		return false
	}
	_, ok = consumed[ruleId]
	return ok
}

func trafficInterceptMarkRuleMatchConsumed(c *gin.Context, ruleId int) {
	if c == nil || ruleId <= 0 {
		return
	}
	value, _ := c.Get(trafficInterceptConsumedRuleMatchesKey)
	consumed, _ := value.(map[int]struct{})
	if consumed == nil {
		consumed = map[int]struct{}{}
		c.Set(trafficInterceptConsumedRuleMatchesKey, consumed)
	}
	consumed[ruleId] = struct{}{}
}

func trafficInterceptRuleHasRequestRewriteAction(rule *model.TrafficInterceptRule) bool {
	if rule == nil {
		return false
	}
	return trafficInterceptHasConfiguredHeaderOps(rule.RequestHeaderOps) ||
		trafficInterceptHasConfiguredJSONArray(rule.RequestMessageRewrites) ||
		strings.TrimSpace(rule.RequestScript) != ""
}

func trafficInterceptRuleHasResponseRewriteAction(rule *model.TrafficInterceptRule, isStream bool, encodedBody bool) bool {
	if rule == nil {
		return false
	}
	return trafficInterceptHasConfiguredHeaderOps(rule.ResponseHeaderOps) ||
		(trafficInterceptHasResponseStructuredRewrite(rule) && !encodedBody) ||
		strings.TrimSpace(rule.ResponseStatusRewrite) != "" ||
		strings.TrimSpace(rule.ResponseURLRewrite) != "" ||
		strings.TrimSpace(rule.ResponseScript) != ""
}

func trafficInterceptRuleHasScriptAction(rule *model.TrafficInterceptRule) bool {
	return rule != nil && rule.ScriptEnabled && strings.TrimSpace(rule.Script) != ""
}

func trafficInterceptHasResponseStructuredRewrite(rule *model.TrafficInterceptRule) bool {
	if rule == nil {
		return false
	}
	return strings.TrimSpace(rule.ResponseContentRewrite) != "" ||
		strings.TrimSpace(rule.ResponseToolCallsRewrite) != ""
}

func trafficInterceptHasConfiguredHeaderOps(ops string) bool {
	return trafficInterceptHasConfiguredJSONArray(ops)
}

func trafficInterceptHasConfiguredJSONArray(ops string) bool {
	ops = strings.TrimSpace(ops)
	return ops != "" && ops != "[]"
}

func matchTrafficInterceptRule(cr *trafficInterceptCachedRule, reqCtx *TrafficInterceptRequestContext) bool {
	return matchTrafficInterceptRequestRule(cr, reqCtx)
}

func matchTrafficInterceptRequestRule(cr *trafficInterceptCachedRule, reqCtx *TrafficInterceptRequestContext) bool {
	if cr == nil || cr.Rule == nil || reqCtx == nil {
		return false
	}
	if !trafficInterceptRuleHasRequestMatch(cr) {
		return true
	}
	if !matchTrafficInterceptRequestBaseRule(cr, reqCtx) {
		return false
	}
	return matchTrafficInterceptRequestCondition(cr, reqCtx)
}

func matchTrafficInterceptResponseCandidateRule(cr *trafficInterceptCachedRule, reqCtx *TrafficInterceptRequestContext) bool {
	if cr == nil || cr.Rule == nil {
		return false
	}
	if trafficInterceptRuleHasRequestMatch(cr) {
		if !matchTrafficInterceptRequestRule(cr, reqCtx) {
			return false
		}
	}
	if trafficInterceptRuleHasResponseMatch(cr) {
		return matchTrafficInterceptResponseBaseRule(cr, reqCtx)
	}
	return true
}

func matchTrafficInterceptRequestBaseRule(cr *trafficInterceptCachedRule, reqCtx *TrafficInterceptRequestContext) bool {
	if cr == nil || cr.Rule == nil || reqCtx == nil {
		return false
	}
	rule := cr.Rule
	return matchTrafficInterceptBaseFields(reqCtx, rule.UserId, rule.Username, cr.pathRegex, rule.Method, cr.modelRegex)
}

func matchTrafficInterceptResponseBaseRule(cr *trafficInterceptCachedRule, reqCtx *TrafficInterceptRequestContext) bool {
	if cr == nil || cr.Rule == nil || reqCtx == nil {
		return false
	}
	rule := cr.Rule
	return matchTrafficInterceptBaseFields(reqCtx, rule.ResponseUserId, rule.ResponseUsername, cr.responsePathRegex, rule.ResponseMethod, cr.responseModelRegex)
}

func matchTrafficInterceptBaseFields(reqCtx *TrafficInterceptRequestContext, userId int, username string, pathRegex *regexp.Regexp, method string, modelRegex *regexp.Regexp) bool {
	if reqCtx == nil {
		return false
	}
	if userId != 0 {
		if reqCtx.UserId != userId {
			return false
		}
	} else if strings.TrimSpace(username) != "" && reqCtx.Username != username {
		return false
	}
	if pathRegex != nil && !pathRegex.MatchString(reqCtx.Path) && !pathRegex.MatchString(reqCtx.URL) && !pathRegex.MatchString(reqCtx.UpstreamURL) {
		return false
	}
	if strings.TrimSpace(method) != "" && !strings.EqualFold(method, reqCtx.Method) {
		return false
	}
	if modelRegex != nil && !modelRegex.MatchString(reqCtx.Model) {
		return false
	}
	return true
}

func trafficInterceptRuleHasRequestMatch(cr *trafficInterceptCachedRule) bool {
	if cr == nil || cr.Rule == nil {
		return false
	}
	return cr.Rule.RequestMatchEnabled
}

func trafficInterceptRuleHasResponseMatch(cr *trafficInterceptCachedRule) bool {
	if cr == nil || cr.Rule == nil {
		return false
	}
	return cr.Rule.ResponseMatchEnabled
}

func matchTrafficInterceptRequestCondition(cr *trafficInterceptCachedRule, reqCtx *TrafficInterceptRequestContext) bool {
	if cr == nil || cr.Rule == nil || reqCtx == nil {
		return false
	}
	if !trafficInterceptRuleHasRequestMatch(cr) {
		return true
	}
	rule := cr.Rule
	if trafficInterceptHasConfiguredJSONArray(rule.RequestMessageMatches) {
		return trafficInterceptRequestMessageMatches(reqCtx.Body, rule.RequestMessageMatches, rule.RequestMessageMatchOp)
	}
	if !trafficInterceptContainsMatch(trafficInterceptLatestContent(reqCtx.Body), rule.RequestContentMatch) {
		return false
	}
	return true
}

func matchTrafficInterceptResponseCondition(cr *trafficInterceptCachedRule, reqCtx *TrafficInterceptRequestContext, respCtx *TrafficInterceptResponseContext) bool {
	if cr == nil || cr.Rule == nil || reqCtx == nil || respCtx == nil {
		return false
	}
	rule := cr.Rule
	if trafficInterceptRuleHasRequestMatch(cr) && !matchTrafficInterceptRequestCondition(cr, reqCtx) {
		return false
	}
	if trafficInterceptRuleHasResponseMatch(cr) {
		if !trafficInterceptResponseMatches(respCtx.Body, rule.ResponseContentMatch, rule.ResponseToolCallsMatch, rule.ResponseMatchOp) {
			return false
		}
	}
	return true
}

func trafficInterceptResponseMatches(body string, contentPattern string, toolCallsPattern string, op string) bool {
	hasContent := strings.TrimSpace(contentPattern) != ""
	hasToolCalls := strings.TrimSpace(toolCallsPattern) != ""
	if !hasContent && !hasToolCalls {
		return true
	}
	contentMatched := !hasContent || trafficInterceptContainsMatch(trafficInterceptResponseContent(body), contentPattern)
	toolCallsMatched := !hasToolCalls || trafficInterceptToolCallsMatch(body, toolCallsPattern)
	if trafficInterceptMatchOpIsOr(op) {
		return (hasContent && contentMatched) || (hasToolCalls && toolCallsMatched)
	}
	return contentMatched && toolCallsMatched
}

func trafficInterceptMatchOpIsOr(op string) bool {
	op = strings.ToLower(strings.TrimSpace(op))
	return op == "or" || op == "any" || op == "||"
}

func trafficInterceptContainsMatch(value string, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}
	return strings.Contains(value, pattern)
}

func trafficInterceptToolCallsMatch(body string, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}
	return strings.Contains(trafficInterceptResponseToolCalls(body), pattern) ||
		strings.Contains(trafficInterceptResponseFunctionNames(body), pattern)
}

func buildTrafficInterceptRequestContext(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo, includeBody bool) *TrafficInterceptRequestContext {
	ctx := &TrafficInterceptRequestContext{
		Header: map[string]string{},
	}
	ctx.Headers = ctx.Header
	if c != nil && c.Request != nil {
		ctx.Method = c.Request.Method
		ctx.Path = c.Request.URL.Path
		ctx.URL = c.Request.URL.String()
		ctx.ContentType = c.GetHeader("Content-Type")
		ctx.UserId = c.GetInt("id")
		ctx.Username = c.GetString("username")
		ctx.TokenName = c.GetString("token_name")
		ctx.Group = c.GetString("group")
		ctx.ChannelId = c.GetInt("channel_id")
		ctx.Model = c.GetString("original_model")
		for key := range c.Request.Header {
			ctx.Header[key] = c.GetHeader(key)
		}
	}
	if info != nil {
		if info.OriginModelName != "" {
			ctx.Model = info.OriginModelName
		}
		if info.UserId != 0 {
			ctx.UserId = info.UserId
		}
		if info.TokenId != 0 && ctx.TokenName == "" && c != nil {
			ctx.TokenName = c.GetString("token_name")
		}
		if info.UsingGroup != "" {
			ctx.Group = info.UsingGroup
		}
		if info.ChannelMeta != nil {
			ctx.ChannelId = info.ChannelId
		}
	}
	if req != nil {
		ctx.Method = req.Method
		ctx.UpstreamURL = req.URL.String()
		if ctx.URL == "" {
			ctx.URL = req.URL.String()
		}
		if req.URL != nil {
			ctx.Path = req.URL.Path
		}
		ctx.Header = headerToStringMap(req.Header)
		ctx.Headers = ctx.Header
		if ct := req.Header.Get("Content-Type"); ct != "" {
			ctx.ContentType = ct
		}
		if includeBody {
			body, err := readAndRestoreHTTPRequestBody(req)
			if err != nil {
				common.SysLog("traffic intercept failed to read upstream request body: " + err.Error())
			} else {
				ctx.Body = body
			}
		}
	} else if includeBody && c != nil {
		ctx.Body = readGinRequestBody(c)
	}
	if ctx.Model == "" && c != nil {
		ctx.Model = c.Query("model")
	}
	return ctx
}

func trafficRulesNeedRequestBody(rules []*trafficInterceptCachedRule, requestPhase bool) bool {
	for _, cr := range rules {
		if cr == nil || cr.Rule == nil {
			continue
		}
		if trafficInterceptRuleMatchLimitReached(cr) {
			continue
		}
		r := cr.Rule
		if trafficRequestRuleNeedsRequestBody(r) {
			return true
		}
		if requestPhase && trafficResponseRuleNeedsRequestBody(r) {
			return true
		}
		if requestPhase && trafficInterceptRuleHasScriptAction(r) && trafficExpressionReferencesBody(r.Script) {
			return true
		}
	}
	return false
}

func trafficRulesNeedResponseBody(rules []*trafficInterceptCachedRule) bool {
	for _, cr := range rules {
		if cr == nil || cr.Rule == nil {
			continue
		}
		if trafficInterceptRuleMatchLimitReached(cr) {
			continue
		}
		r := cr.Rule
		if !r.InterceptResponse && !trafficInterceptRuleHasScriptAction(r) {
			continue
		}
		if r.ResponseMatchEnabled &&
			(strings.TrimSpace(r.ResponseContentMatch) != "" ||
				strings.TrimSpace(r.ResponseToolCallsMatch) != "") {
			return true
		}
		if trafficInterceptHasResponseStructuredRewrite(r) ||
			strings.TrimSpace(r.ResponseScript) != "" ||
			trafficExpressionReferencesBody(r.ResponseHeaderOps, r.ResponseStatusRewrite, r.ResponseURLRewrite, r.Script) {
			return true
		}
	}
	return false
}

func trafficResponseRulesNeedRequestBody(rules []*trafficInterceptCachedRule) bool {
	for _, cr := range rules {
		if cr == nil || cr.Rule == nil {
			continue
		}
		if trafficInterceptRuleMatchLimitReached(cr) {
			continue
		}
		r := cr.Rule
		if !r.InterceptResponse && !trafficInterceptRuleHasScriptAction(r) {
			continue
		}
		if trafficResponseRuleNeedsRequestBody(r) {
			return true
		}
	}
	return false
}

func trafficRequestRuleNeedsRequestBody(rule *model.TrafficInterceptRule) bool {
	if rule == nil || !rule.InterceptRequest {
		return false
	}
	if rule.RequestMatchEnabled &&
		(strings.TrimSpace(rule.RequestContentMatch) != "" ||
			trafficInterceptHasConfiguredJSONArray(rule.RequestMessageMatches)) {
		return true
	}
	return trafficInterceptHasConfiguredJSONArray(rule.RequestMessageRewrites) ||
		trafficExpressionReferencesBody(rule.RequestHeaderOps, rule.RequestScript)
}

func trafficResponseRuleNeedsRequestBody(rule *model.TrafficInterceptRule) bool {
	if rule == nil || !rule.InterceptResponse {
		if !trafficInterceptRuleHasScriptAction(rule) {
			return false
		}
	}
	if rule.RequestMatchEnabled &&
		(strings.TrimSpace(rule.RequestContentMatch) != "" ||
			trafficInterceptHasConfiguredJSONArray(rule.RequestMessageMatches)) {
		return true
	}
	return trafficExpressionReferencesBody(
		rule.ResponseHeaderOps,
		rule.ResponseContentRewrite,
		rule.ResponseToolCallsRewrite,
		rule.ResponseStatusRewrite,
		rule.ResponseURLRewrite,
		rule.ResponseScript,
		rule.Script,
	)
}

func trafficExpressionReferencesBody(expressions ...string) bool {
	for _, expression := range expressions {
		if strings.Contains(strings.ToLower(expression), "body") {
			return true
		}
	}
	return false
}

func trafficInterceptEnv(req *TrafficInterceptRequestContext, resp *TrafficInterceptResponseContext) map[string]interface{} {
	env := trafficInterceptCompileEnv()
	env["request"] = req
	env["response"] = resp
	if resp != nil {
		env["status"] = resp.Status
		env["body"] = resp.Body
	} else if req != nil {
		env["body"] = req.Body
	}
	return env
}

func trafficInterceptCompileEnv() map[string]interface{} {
	req := &TrafficInterceptRequestContext{
		Header:  map[string]string{},
		Headers: map[string]string{},
	}
	resp := &TrafficInterceptResponseContext{
		Header:  map[string]string{},
		Headers: map[string]string{},
	}
	return map[string]interface{}{
		"request":  req,
		"response": resp,
		"status":   0,
		"body":     "",
		"merge":    trafficInterceptMerge,
		"set":      trafficInterceptSet,
		"del":      trafficInterceptDel,
		"replace":  strings.ReplaceAll,
		"contains": strings.Contains,
		"header": func(headers interface{}, name string) string {
			return lookupTrafficInterceptHeader(trafficInterceptStringMap(headers), name)
		},
		"jsonParse": func(input string) interface{} {
			var out interface{}
			if err := common.Unmarshal([]byte(input), &out); err != nil {
				return nil
			}
			return out
		},
		"jsonStringify": func(input interface{}) string {
			data, err := common.Marshal(input)
			if err != nil {
				return ""
			}
			return string(data)
		},
		"latestContent":         trafficInterceptLatestContent,
		"requestContent":        trafficInterceptLatestContent,
		"responseContent":       trafficInterceptResponseContent,
		"responseToolCalls":     trafficInterceptResponseToolCalls,
		"responseFunctions":     trafficInterceptResponseToolCalls,
		"responseFunctionNames": trafficInterceptResponseFunctionNames,
		"responsePreview":       trafficInterceptResponsePreview,
	}
}

type trafficInterceptScriptOutput struct {
	Request  map[string]interface{}
	Response map[string]interface{}
	Result   map[string]interface{}
}

type trafficInterceptScriptBlock struct {
	status      int
	contentType string
	body        string
	headers     map[string]string
}

func runTrafficInterceptJSHook(script string, hook string, rule *model.TrafficInterceptRule, reqCtx *TrafficInterceptRequestContext, respCtx *TrafficInterceptResponseContext) (*trafficInterceptScriptOutput, bool, error) {
	script = strings.TrimSpace(script)
	if script == "" {
		return nil, false, nil
	}

	vm := goja.New()
	installTrafficInterceptJSConsole(vm, rule)
	timer := time.AfterFunc(trafficInterceptScriptTimeout, func() {
		vm.Interrupt("traffic intercept script timeout")
	})
	defer timer.Stop()

	if _, err := vm.RunString(script); err != nil {
		if !trafficInterceptScriptLooksLikeJSHook(script, hook) {
			return nil, false, nil
		}
		return nil, false, err
	}
	fn, ok := goja.AssertFunction(vm.Get(hook))
	if !ok {
		return nil, false, nil
	}

	requestObject := trafficInterceptScriptRequestObject(reqCtx)
	responseObject := trafficInterceptScriptResponseObject(respCtx)
	requestValue := vm.ToValue(requestObject)
	responseValue := goja.Null()
	if responseObject != nil {
		responseValue = vm.ToValue(responseObject)
	}
	result, err := fn(
		goja.Undefined(),
		vm.ToValue(trafficInterceptScriptContextObject(hook, rule)),
		requestValue,
		responseValue,
	)
	if err != nil {
		return nil, true, err
	}
	result, err = trafficInterceptResolveJSPromise(result)
	if err != nil {
		return nil, true, err
	}

	output := &trafficInterceptScriptOutput{
		Request:  trafficInterceptMap(requestValue.Export()),
		Response: trafficInterceptMap(responseValue.Export()),
		Result:   trafficInterceptJSValueMap(result),
	}
	if output.Result != nil {
		if request := trafficInterceptMap(output.Result["request"]); len(request) > 0 {
			output.Request = request
		} else if hook == "onRequest" && trafficInterceptLooksLikeScriptRequest(output.Result) {
			output.Request = output.Result
		}
		if response := trafficInterceptMap(output.Result["response"]); len(response) > 0 {
			output.Response = response
		} else if hook == "onResponse" && trafficInterceptLooksLikeScriptResponse(output.Result) {
			output.Response = output.Result
		}
	}
	return output, true, nil
}

func installTrafficInterceptJSConsole(vm *goja.Runtime, rule *model.TrafficInterceptRule) {
	console := vm.NewObject()
	_ = console.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, 0, len(call.Arguments))
		for _, arg := range call.Arguments {
			parts = append(parts, arg.String())
		}
		ruleId := 0
		if rule != nil {
			ruleId = rule.Id
		}
		common.SysLog(fmt.Sprintf("traffic intercept rule %d script: %s", ruleId, strings.Join(parts, " ")))
		return goja.Undefined()
	})
	_ = vm.Set("console", console)
}

func trafficInterceptResolveJSPromise(value goja.Value) (goja.Value, error) {
	object, ok := value.(*goja.Object)
	if !ok {
		return value, nil
	}
	promise, ok := object.Export().(*goja.Promise)
	if !ok {
		return value, nil
	}
	switch promise.State() {
	case goja.PromiseStateFulfilled:
		return promise.Result(), nil
	case goja.PromiseStateRejected:
		return nil, fmt.Errorf("promise rejected: %s", promise.Result().String())
	default:
		return nil, fmt.Errorf("pending promises are not supported")
	}
}

func trafficInterceptJSValueMap(value goja.Value) map[string]interface{} {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil
	}
	return trafficInterceptMap(value.Export())
}

func trafficInterceptScriptLooksLikeJSHook(script string, hook string) bool {
	return strings.Contains(script, hook)
}

func trafficInterceptScriptContextObject(hook string, rule *model.TrafficInterceptRule) map[string]interface{} {
	phase := strings.TrimPrefix(strings.TrimPrefix(hook, "on"), "On")
	phase = strings.ToLower(phase)
	out := map[string]interface{}{
		"phase": phase,
	}
	if rule != nil {
		out["rule"] = map[string]interface{}{
			"id":       rule.Id,
			"name":     rule.Name,
			"priority": rule.Priority,
		}
	}
	return out
}

func trafficInterceptScriptRequestObject(ctx *TrafficInterceptRequestContext) map[string]interface{} {
	if ctx == nil {
		ctx = &TrafficInterceptRequestContext{Header: map[string]string{}, Headers: map[string]string{}}
	}
	return map[string]interface{}{
		"method":       ctx.Method,
		"path":         ctx.Path,
		"url":          ctx.URL,
		"upstream_url": ctx.UpstreamURL,
		"model":        ctx.Model,
		"user_id":      ctx.UserId,
		"username":     ctx.Username,
		"token_name":   ctx.TokenName,
		"group":        ctx.Group,
		"channel_id":   ctx.ChannelId,
		"content_type": ctx.ContentType,
		"headers":      trafficInterceptScriptHeaders(ctx.Header),
		"body":         ctx.Body,
	}
}

func trafficInterceptScriptResponseObject(ctx *TrafficInterceptResponseContext) map[string]interface{} {
	if ctx == nil {
		return nil
	}
	return map[string]interface{}{
		"url":          ctx.URL,
		"status":       ctx.Status,
		"statusCode":   ctx.Status,
		"content_type": ctx.ContentType,
		"headers":      trafficInterceptScriptHeaders(ctx.Header),
		"body":         ctx.Body,
	}
}

func trafficInterceptScriptHeaders(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[strings.ToLower(key)] = value
	}
	return out
}

func trafficInterceptLooksLikeScriptRequest(input map[string]interface{}) bool {
	return trafficInterceptHasAnyKey(input, "method", "path", "url", "upstream_url", "model", "user_id", "username", "content_type", "headers", "body")
}

func trafficInterceptLooksLikeScriptResponse(input map[string]interface{}) bool {
	return trafficInterceptHasAnyKey(input, "url", "status", "content_type", "headers", "body")
}

func trafficInterceptHasAnyKey(input map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if _, ok := input[key]; ok {
			return true
		}
	}
	return false
}

func applyTrafficInterceptScriptRequest(req *http.Request, ctx *TrafficInterceptRequestContext, input map[string]interface{}) {
	if len(input) == 0 {
		return
	}
	if ctx == nil {
		ctx = &TrafficInterceptRequestContext{}
	}
	if method, ok := trafficInterceptMapString(input, "method"); ok && strings.TrimSpace(method) != "" {
		ctx.Method = method
		if req != nil {
			req.Method = method
		}
	}
	if rawURL, ok := trafficInterceptFirstMapString(input, "upstream_url", "url"); ok && strings.TrimSpace(rawURL) != "" && req != nil {
		if err := applyHTTPRequestURL(req, rawURL); err != nil {
			common.SysLog("traffic intercept script request url error: " + err.Error())
		} else {
			updateRequestURLContext(ctx, req)
		}
	}
	if headers, ok := input["headers"]; ok {
		if req != nil {
			replaceTrafficInterceptHeaders(req.Header, headers)
			ctx.Header = headerToStringMap(req.Header)
		} else {
			ctx.Header = trafficInterceptStringMap(headers)
		}
		ctx.Headers = ctx.Header
		ctx.ContentType = lookupTrafficInterceptHeader(ctx.Header, "Content-Type")
	}
	if contentType, ok := trafficInterceptMapString(input, "content_type"); ok {
		ctx.ContentType = contentType
		if req != nil {
			req.Header.Set("Content-Type", contentType)
			ctx.Header = headerToStringMap(req.Header)
			ctx.Headers = ctx.Header
		}
	}
	if body, ok := trafficInterceptMapString(input, "body"); ok {
		ctx.Body = body
		if req != nil {
			setHTTPRequestBody(req, body)
			ctx.Header = headerToStringMap(req.Header)
			ctx.Headers = ctx.Header
		}
	}
}

func applyTrafficInterceptScriptResponse(req *http.Request, resp *http.Response, ctx *TrafficInterceptResponseContext, input map[string]interface{}, encodedBody bool) {
	if ctx == nil || len(input) == 0 {
		return
	}
	if status, ok := input["status"]; ok {
		ctx.Status = anyToInt(status, ctx.Status)
	}
	if status, ok := input["statusCode"]; ok {
		ctx.Status = anyToInt(status, ctx.Status)
	}
	if rawURL, ok := trafficInterceptMapString(input, "url"); ok {
		ctx.URL = applyHTTPResponseURL(req, resp, rawURL)
	}
	if headers, ok := input["headers"]; ok {
		if resp != nil {
			replaceTrafficInterceptHeaders(resp.Header, headers)
			ctx.Header = headerToStringMap(resp.Header)
		} else {
			ctx.Header = trafficInterceptStringMap(headers)
		}
		ctx.Headers = ctx.Header
		ctx.ContentType = lookupTrafficInterceptHeader(ctx.Header, "Content-Type")
	}
	if contentType, ok := trafficInterceptMapString(input, "content_type"); ok {
		ctx.ContentType = contentType
		if resp != nil {
			resp.Header.Set("Content-Type", contentType)
			ctx.Header = headerToStringMap(resp.Header)
			ctx.Headers = ctx.Header
		}
	}
	if body, ok := trafficInterceptMapString(input, "body"); ok && !encodedBody {
		ctx.Body = body
	}
}

func replaceTrafficInterceptHeaders(header http.Header, input interface{}) {
	if header == nil || input == nil {
		return
	}
	for key := range header {
		if !shouldSkipTrafficInterceptHeader(key) {
			header.Del(key)
		}
	}
	for key, value := range trafficInterceptMap(input) {
		key = strings.TrimSpace(key)
		if key == "" || shouldSkipTrafficInterceptHeader(key) || value == nil {
			continue
		}
		text := anyToString(value, "")
		if strings.EqualFold(strings.TrimSpace(text), "__delete__") {
			continue
		}
		header.Set(key, text)
	}
}

func trafficInterceptScriptBlockAction(output *trafficInterceptScriptOutput, rule *model.TrafficInterceptRule) (trafficInterceptScriptBlock, bool) {
	if output == nil {
		return trafficInterceptScriptBlock{}, false
	}
	for _, input := range []map[string]interface{}{output.Result, output.Request, output.Response} {
		if len(input) == 0 || !truthy(input["block"]) {
			continue
		}
		body := ""
		contentType := ""
		if rule != nil {
			body = rule.BlockBody
			contentType = rule.BlockContentType
		}
		if text, ok := trafficInterceptFirstMapString(input, "block_body", "response_body", "body"); ok {
			body = text
		}
		if text, ok := trafficInterceptFirstMapString(input, "block_content_type", "content_type"); ok {
			contentType = text
		}
		return trafficInterceptScriptBlock{
			status:      anyToInt(input["status"], http.StatusForbidden),
			contentType: contentType,
			body:        body,
			headers:     trafficInterceptStringMap(input["headers"]),
		}, true
	}
	return trafficInterceptScriptBlock{}, false
}

func applyTrafficInterceptMessageContentRewrites(body string, rewritesJSON string, env map[string]interface{}) (string, bool) {
	var rewrites []model.MessageContentRewrite
	if err := common.Unmarshal([]byte(rewritesJSON), &rewrites); err != nil {
		common.SysLog("traffic intercept message rewrite JSON error: " + err.Error())
		return "", false
	}
	if len(rewrites) == 0 {
		return "", false
	}

	var root interface{}
	if err := common.Unmarshal([]byte(body), &root); err != nil {
		common.SysLog("traffic intercept request body JSON error: " + err.Error())
		return "", false
	}
	rootMap := trafficInterceptMap(root)
	if len(rootMap) == 0 {
		return "", false
	}
	messages := trafficInterceptSlice(rootMap["messages"])
	if len(messages) == 0 {
		return "", false
	}

	changed := false
	for _, rewrite := range rewrites {
		if trafficInterceptApplyMessageContentRewrite(messages, rewrite, env) {
			changed = true
		}
	}
	if !changed {
		return "", false
	}

	data, err := common.Marshal(root)
	if err != nil {
		common.SysLog("traffic intercept request body JSON marshal error: " + err.Error())
		return "", false
	}
	return string(data), true
}

type trafficInterceptResponseTextSegment struct {
	target map[string]interface{}
	key    string
}

type trafficInterceptResponseToolCallSegment struct {
	target map[string]interface{}
	key    string
}

type trafficInterceptEventStreamBlock struct {
	raw     string
	data    string
	json    interface{}
	hasJSON bool
}

func applyTrafficInterceptResponseRewrites(body string, contentRewrite string, toolCallsRewrite string, env map[string]interface{}) (string, bool) {
	hasContentRewrite := strings.TrimSpace(contentRewrite) != ""
	hasToolCallsRewrite := strings.TrimSpace(toolCallsRewrite) != ""
	if strings.TrimSpace(body) == "" || (!hasContentRewrite && !hasToolCallsRewrite) {
		return "", false
	}

	content := ""
	if hasContentRewrite {
		content = trafficInterceptResponseContentRewriteValue(contentRewrite, env)
	}

	toolCalls := []interface{}(nil)
	if hasToolCallsRewrite {
		var ok bool
		toolCalls, ok = trafficInterceptResponseToolCallsRewriteValue(toolCallsRewrite, env)
		if !ok {
			return "", false
		}
	}

	if blocks, ok := trafficInterceptParseEventStreamBlocks(body); ok {
		payloads := trafficInterceptEventStreamBlockPayloads(blocks)
		changed := false
		if hasContentRewrite {
			if rewrittenBlocks, ok := trafficInterceptRewriteEventStreamContentBlocks(blocks, payloads, content); ok {
				blocks = rewrittenBlocks
				payloads = trafficInterceptEventStreamBlockPayloads(blocks)
				changed = true
			}
		}
		if hasToolCallsRewrite && trafficInterceptApplyResponseToolCallsRewrite(payloads, toolCalls) {
			changed = true
		}
		if !changed {
			return "", false
		}
		return trafficInterceptSerializeEventStreamBlocks(blocks)
	}

	var root interface{}
	if err := common.Unmarshal([]byte(body), &root); err != nil {
		common.SysLog("traffic intercept response body JSON error: " + err.Error())
		return "", false
	}
	payloads := trafficInterceptRewritePayloads(root)
	changed := false
	if hasContentRewrite && trafficInterceptApplyResponseContentRewrite(payloads, content) {
		changed = true
	}
	if hasToolCallsRewrite && trafficInterceptApplyResponseToolCallsRewrite(payloads, toolCalls) {
		changed = true
	}
	if !changed {
		return "", false
	}
	data, err := common.Marshal(root)
	if err != nil {
		common.SysLog("traffic intercept response body JSON marshal error: " + err.Error())
		return "", false
	}
	return string(data), true
}

func trafficInterceptEventStreamBlockPayloads(blocks []*trafficInterceptEventStreamBlock) []interface{} {
	payloads := make([]interface{}, 0, len(blocks))
	for _, block := range blocks {
		if block != nil && block.hasJSON {
			payloads = append(payloads, block.json)
		}
	}
	return payloads
}

func trafficInterceptRewriteEventStreamContentBlocks(blocks []*trafficInterceptEventStreamBlock, payloads []interface{}, content string) ([]*trafficInterceptEventStreamBlock, bool) {
	if trafficInterceptResponseContent(payloads) == content {
		return nil, false
	}
	contentPayload := trafficInterceptBuildEventStreamContentPayload(payloads, content)
	if contentPayload == nil {
		return nil, false
	}

	next := []*trafficInterceptEventStreamBlock{trafficInterceptNewEventStreamJSONBlock(trafficInterceptBuildEventStreamStartPayload(payloads))}
	next = append(next, trafficInterceptNewEventStreamJSONBlock(contentPayload))
	next = append(next, trafficInterceptNewEventStreamJSONBlock(trafficInterceptBuildEventStreamStopPayload(payloads)))
	next = append(next, trafficInterceptEventStreamUsageBlocks(blocks)...)
	next = append(next, trafficInterceptEventStreamDoneBlock(blocks))
	return next, true
}

func trafficInterceptBuildEventStreamStartPayload(payloads []interface{}) map[string]interface{} {
	base := trafficInterceptEventStreamBasePayload(payloads)
	base["choices"] = []interface{}{
		map[string]interface{}{
			"index": 0,
			"delta": map[string]interface{}{
				"role":    "assistant",
				"content": "",
			},
		},
	}
	return base
}

func trafficInterceptBuildEventStreamContentPayload(payloads []interface{}, content string) map[string]interface{} {
	base := trafficInterceptEventStreamBasePayload(payloads)
	choice := map[string]interface{}{
		"index": 0,
		"delta": map[string]interface{}{
			"content": content,
		},
	}
	if finishReason := anyToString(trafficInterceptEventStreamFirstChoiceField(payloads, "finish_reason"), ""); finishReason != "" {
		choice["finish_reason"] = nil
	}
	base["choices"] = []interface{}{choice}
	return base
}

func trafficInterceptBuildEventStreamStopPayload(payloads []interface{}) map[string]interface{} {
	base := trafficInterceptEventStreamBasePayload(payloads)
	finishReason := anyToString(trafficInterceptEventStreamFirstChoiceField(payloads, "finish_reason"), "")
	if finishReason == "" {
		finishReason = "stop"
	}
	base["choices"] = []interface{}{
		map[string]interface{}{
			"index":         0,
			"delta":         map[string]interface{}{},
			"finish_reason": finishReason,
		},
	}
	return base
}

func trafficInterceptEventStreamBasePayload(payloads []interface{}) map[string]interface{} {
	base := map[string]interface{}{
		"object": "chat.completion.chunk",
	}
	for _, payload := range payloads {
		root := trafficInterceptMap(payload)
		if len(root) == 0 {
			continue
		}
		for _, key := range []string{"id", "object", "created", "model", "system_fingerprint", "service_tier"} {
			if value, ok := root[key]; ok {
				base[key] = value
			}
		}
		break
	}
	return base
}

func trafficInterceptEventStreamFirstChoiceField(payloads []interface{}, key string) interface{} {
	for _, payload := range payloads {
		root := trafficInterceptMap(payload)
		if len(root) == 0 {
			continue
		}
		for _, choiceValue := range trafficInterceptSlice(root["choices"]) {
			choice := trafficInterceptMap(choiceValue)
			if len(choice) == 0 {
				continue
			}
			if value, ok := choice[key]; ok && value != nil {
				return value
			}
		}
	}
	return nil
}

func trafficInterceptEventStreamUsageBlocks(blocks []*trafficInterceptEventStreamBlock) []*trafficInterceptEventStreamBlock {
	usageBlocks := make([]*trafficInterceptEventStreamBlock, 0)
	for _, block := range blocks {
		if block == nil || !block.hasJSON {
			continue
		}
		root := trafficInterceptMap(block.json)
		if len(root) == 0 {
			continue
		}
		if _, ok := root["usage"]; !ok {
			continue
		}
		if choices := trafficInterceptSlice(root["choices"]); choices != nil && len(choices) > 0 {
			continue
		}
		usageBlocks = append(usageBlocks, block)
	}
	return usageBlocks
}

func trafficInterceptEventStreamDoneBlock(blocks []*trafficInterceptEventStreamBlock) *trafficInterceptEventStreamBlock {
	for _, block := range blocks {
		if block != nil && strings.TrimSpace(block.data) == "[DONE]" {
			return block
		}
	}
	return &trafficInterceptEventStreamBlock{raw: "data: [DONE]", data: "[DONE]"}
}

func trafficInterceptNewEventStreamJSONBlock(payload interface{}) *trafficInterceptEventStreamBlock {
	return &trafficInterceptEventStreamBlock{json: payload, hasJSON: true}
}

func trafficInterceptResponseContentRewriteValue(expression string, env map[string]interface{}) string {
	value, ok := trafficInterceptRewriteValue(expression, env)
	if !ok {
		return ""
	}
	return anyToString(value, "")
}

func trafficInterceptResponseToolCallsRewriteValue(expression string, env map[string]interface{}) ([]interface{}, bool) {
	value, ok := trafficInterceptRewriteValue(expression, env)
	if !ok {
		return nil, false
	}
	toolCalls, ok := trafficInterceptNormalizeToolCallsRewrite(value)
	if !ok {
		common.SysLog("traffic intercept response tool_calls rewrite must be a JSON object or array")
		return nil, false
	}
	return toolCalls, true
}

func trafficInterceptRewriteValue(expression string, env map[string]interface{}) (interface{}, bool) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, false
	}
	var parsed interface{}
	if err := common.Unmarshal([]byte(expression), &parsed); err == nil {
		return parsed, true
	}
	out, err := evalTrafficInterceptExpression(expression, env)
	if err == nil && out != nil {
		return out, true
	}
	return expression, true
}

func trafficInterceptNormalizeToolCallsRewrite(value interface{}) ([]interface{}, bool) {
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return []interface{}{}, true
		}
		var parsed interface{}
		if err := common.Unmarshal([]byte(text), &parsed); err != nil {
			return nil, false
		}
		return trafficInterceptNormalizeToolCallsRewrite(parsed)
	}
	if items := trafficInterceptSlice(value); items != nil {
		return items, true
	}
	valueMap := trafficInterceptMap(value)
	if len(valueMap) == 0 {
		return nil, false
	}
	if items := trafficInterceptSlice(valueMap["tool_calls"]); items != nil {
		return items, true
	}
	if functionCall, ok := valueMap["function_call"]; ok {
		return []interface{}{functionCall}, true
	}
	return []interface{}{valueMap}, true
}

func trafficInterceptRewritePayloads(root interface{}) []interface{} {
	if items := trafficInterceptSlice(root); items != nil {
		return items
	}
	if trafficInterceptMap(root) != nil {
		return []interface{}{root}
	}
	return nil
}

func trafficInterceptApplyResponseContentRewrite(payloads []interface{}, content string) bool {
	segments := make([]trafficInterceptResponseTextSegment, 0)
	for _, payload := range payloads {
		trafficInterceptCollectResponseContentRewriteSegments(payload, &segments)
	}
	if len(segments) == 0 {
		target := trafficInterceptResponseContentInsertionTarget(payloads)
		if target == nil {
			return false
		}
		if current, ok := target["content"].(string); ok && current == content {
			return false
		}
		target["content"] = content
		return true
	}

	return trafficInterceptWriteResponseContentToFirstSegment(segments, content)
}

func trafficInterceptWriteResponseContentToFirstSegment(segments []trafficInterceptResponseTextSegment, content string) bool {
	changed := false
	for index, segment := range segments {
		next := ""
		if index == 0 {
			next = content
		}
		if current, _ := segment.target[segment.key].(string); current != next {
			segment.target[segment.key] = next
			changed = true
		}
	}
	return changed
}

func trafficInterceptApplyResponseToolCallsRewrite(payloads []interface{}, toolCalls []interface{}) bool {
	segments := make([]trafficInterceptResponseToolCallSegment, 0)
	for _, payload := range payloads {
		trafficInterceptCollectResponseToolCallRewriteSegments(payload, &segments)
	}
	if len(segments) == 0 {
		if len(toolCalls) == 0 {
			return false
		}
		target := trafficInterceptResponseToolCallsInsertionTarget(payloads)
		if target == nil {
			return false
		}
		target["tool_calls"] = toolCalls
		return true
	}

	changed := false
	for index, segment := range segments {
		if segment.key == "tool_calls" {
			next := []interface{}{}
			if index == 0 {
				next = toolCalls
			}
			if !reflect.DeepEqual(segment.target["tool_calls"], next) {
				segment.target["tool_calls"] = next
				changed = true
			}
			continue
		}

		if index == 0 && len(toolCalls) > 0 {
			if !reflect.DeepEqual(segment.target["function_call"], toolCalls[0]) {
				segment.target["function_call"] = toolCalls[0]
				changed = true
			}
			continue
		}
		if _, ok := segment.target["function_call"]; ok {
			delete(segment.target, "function_call")
			changed = true
		}
	}
	return changed
}

func trafficInterceptCollectResponseContentRewriteSegments(input interface{}, segments *[]trafficInterceptResponseTextSegment) {
	root := trafficInterceptMap(input)
	if len(root) == 0 {
		return
	}
	if choices := trafficInterceptSlice(root["choices"]); choices != nil {
		for _, choiceValue := range choices {
			choice := trafficInterceptMap(choiceValue)
			if len(choice) == 0 {
				continue
			}
			trafficInterceptCollectMessageContentRewriteSegments(choice["delta"], segments)
			trafficInterceptCollectMessageContentRewriteSegments(choice["message"], segments)
			trafficInterceptCollectStringRewriteSegment(choice, "text", segments)
		}
		return
	}
	trafficInterceptCollectStringRewriteSegment(root, "output_text", segments)
	trafficInterceptCollectStringRewriteSegment(root, "text", segments)
	trafficInterceptCollectStringRewriteSegment(root, "content", segments)
	if output := trafficInterceptSlice(root["output"]); output != nil {
		for _, item := range output {
			trafficInterceptCollectResponseContentRewriteSegments(item, segments)
			trafficInterceptCollectMessageContentRewriteSegments(item, segments)
		}
	}
}

func trafficInterceptCollectMessageContentRewriteSegments(input interface{}, segments *[]trafficInterceptResponseTextSegment) {
	message := trafficInterceptMap(input)
	if len(message) == 0 {
		return
	}
	trafficInterceptCollectStringRewriteSegment(message, "content", segments)
	trafficInterceptCollectStringRewriteSegment(message, "output_text", segments)
}

func trafficInterceptCollectStringRewriteSegment(target map[string]interface{}, key string, segments *[]trafficInterceptResponseTextSegment) {
	if target == nil {
		return
	}
	if _, ok := target[key].(string); ok {
		*segments = append(*segments, trafficInterceptResponseTextSegment{target: target, key: key})
	}
}

func trafficInterceptCollectResponseToolCallRewriteSegments(input interface{}, segments *[]trafficInterceptResponseToolCallSegment) {
	root := trafficInterceptMap(input)
	if len(root) == 0 {
		return
	}
	if choices := trafficInterceptSlice(root["choices"]); choices != nil {
		for _, choiceValue := range choices {
			choice := trafficInterceptMap(choiceValue)
			if len(choice) == 0 {
				continue
			}
			trafficInterceptCollectToolCallRewriteSegments(choice["delta"], segments)
			trafficInterceptCollectToolCallRewriteSegments(choice["message"], segments)
			if _, hasDelta := choice["delta"]; !hasDelta {
				if _, hasMessage := choice["message"]; !hasMessage {
					trafficInterceptCollectToolCallRewriteSegments(choice, segments)
				}
			}
		}
		return
	}
	trafficInterceptCollectToolCallRewriteSegments(root, segments)
}

func trafficInterceptCollectToolCallRewriteSegments(input interface{}, segments *[]trafficInterceptResponseToolCallSegment) {
	if items := trafficInterceptSlice(input); items != nil {
		for _, item := range items {
			trafficInterceptCollectToolCallRewriteSegments(item, segments)
		}
		return
	}
	value := trafficInterceptMap(input)
	if value == nil {
		return
	}
	if _, ok := value["tool_calls"]; ok {
		*segments = append(*segments, trafficInterceptResponseToolCallSegment{target: value, key: "tool_calls"})
	}
	if _, ok := value["function_call"]; ok {
		*segments = append(*segments, trafficInterceptResponseToolCallSegment{target: value, key: "function_call"})
	}
	for key, child := range value {
		switch key {
		case "tool_calls", "function_call":
			continue
		default:
			trafficInterceptCollectToolCallRewriteSegments(child, segments)
		}
	}
}

func trafficInterceptResponseContentInsertionTarget(payloads []interface{}) map[string]interface{} {
	for _, payload := range payloads {
		if target := trafficInterceptResponseChoiceMessageTarget(payload); target != nil {
			return target
		}
	}
	for _, payload := range payloads {
		if target := trafficInterceptMap(payload); target != nil {
			return target
		}
	}
	return nil
}

func trafficInterceptResponseToolCallsInsertionTarget(payloads []interface{}) map[string]interface{} {
	return trafficInterceptResponseContentInsertionTarget(payloads)
}

func trafficInterceptResponseChoiceMessageTarget(payload interface{}) map[string]interface{} {
	root := trafficInterceptMap(payload)
	if len(root) == 0 {
		return nil
	}
	choices := trafficInterceptSlice(root["choices"])
	if len(choices) == 0 {
		if output := trafficInterceptSlice(root["output"]); len(output) > 0 {
			for _, item := range output {
				if target := trafficInterceptResponseChoiceMessageTarget(item); target != nil {
					return target
				}
				if target := trafficInterceptMap(item); target != nil {
					return target
				}
			}
		}
		return nil
	}
	for _, choiceValue := range choices {
		choice := trafficInterceptMap(choiceValue)
		if len(choice) == 0 {
			continue
		}
		if message, ok := trafficInterceptNestedMap(choice, "message"); ok {
			return message
		}
		if delta, ok := trafficInterceptNestedMap(choice, "delta"); ok {
			return delta
		}
		message := map[string]interface{}{}
		choice["message"] = message
		return message
	}
	return nil
}

func trafficInterceptNestedMap(parent map[string]interface{}, key string) (map[string]interface{}, bool) {
	if parent == nil {
		return nil, false
	}
	value, ok := parent[key]
	if !ok {
		return nil, false
	}
	valueMap := trafficInterceptMap(value)
	if valueMap == nil {
		valueMap = map[string]interface{}{}
		parent[key] = valueMap
	}
	return valueMap, true
}

func trafficInterceptParseEventStreamBlocks(input string) ([]*trafficInterceptEventStreamBlock, bool) {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	rawBlocks := strings.Split(normalized, "\n\n")
	blocks := make([]*trafficInterceptEventStreamBlock, 0, len(rawBlocks))
	hasEventStreamFields := false
	for _, raw := range rawBlocks {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if trafficInterceptEventBlockHasField(raw) {
			hasEventStreamFields = true
		}
		data := trafficInterceptEventBlockData(raw)
		block := &trafficInterceptEventStreamBlock{raw: raw, data: data}
		if data != "" && data != "[DONE]" {
			var parsed interface{}
			if err := common.Unmarshal([]byte(data), &parsed); err == nil {
				block.json = parsed
				block.hasJSON = true
			}
		}
		blocks = append(blocks, block)
	}
	return blocks, hasEventStreamFields
}

func trafficInterceptEventBlockHasField(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") || strings.HasPrefix(line, "event:") {
			return true
		}
	}
	return false
}

func trafficInterceptSerializeEventStreamBlocks(blocks []*trafficInterceptEventStreamBlock) (string, bool) {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block == nil {
			continue
		}
		if block.hasJSON {
			data, err := common.Marshal(block.json)
			if err != nil {
				common.SysLog("traffic intercept event stream JSON marshal error: " + err.Error())
				return "", false
			}
			parts = append(parts, "data: "+string(data))
			continue
		}
		if strings.TrimSpace(block.raw) != "" {
			parts = append(parts, block.raw)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n\n") + "\n\n", true
}

func trafficInterceptRequestMessageMatches(body string, matchesJSON string, op string) bool {
	var matches []model.MessageContentMatch
	if err := common.Unmarshal([]byte(matchesJSON), &matches); err != nil {
		common.SysLog("traffic intercept message match JSON error: " + err.Error())
		return false
	}
	if len(matches) == 0 {
		return true
	}

	value := trafficInterceptJSONValue(body)
	root := trafficInterceptMap(value)
	if len(root) == 0 {
		return false
	}
	messages := trafficInterceptSlice(root["messages"])
	if len(messages) == 0 {
		return false
	}

	if trafficInterceptMatchOpIsOr(op) {
		for _, match := range matches {
			if trafficInterceptRequestMessageMatch(messages, match) {
				return true
			}
		}
		return false
	}

	for _, match := range matches {
		if !trafficInterceptRequestMessageMatch(messages, match) {
			return false
		}
	}
	return true
}

func trafficInterceptRequestMessageMatch(messages []interface{}, match model.MessageContentMatch) bool {
	keywords := trafficInterceptMatchKeywords(match.Content)
	if len(keywords) == 0 {
		return true
	}
	role := strings.TrimSpace(match.Role)
	if role == "" {
		role = "user"
	}
	mode := strings.ToLower(strings.TrimSpace(match.Mode))
	if mode == "" {
		mode = "latest"
	}

	switch mode {
	case "all":
		parts := make([]string, 0)
		for _, value := range messages {
			message := trafficInterceptMap(value)
			if !trafficInterceptMessageRoleMatches(message, role) {
				continue
			}
			if text := trafficInterceptMessageContentText(message["content"]); text != "" {
				parts = append(parts, text)
			}
		}
		return trafficInterceptContentKeywordsMatch(strings.Join(parts, "\n"), keywords, match.ContentOp)
	case "first":
		for _, value := range messages {
			message := trafficInterceptMap(value)
			if trafficInterceptMessageRoleMatches(message, role) {
				return trafficInterceptContentKeywordsMatch(trafficInterceptMessageContentText(message["content"]), keywords, match.ContentOp)
			}
		}
	case "index":
		if match.Index >= 0 && match.Index < len(messages) {
			message := trafficInterceptMap(messages[match.Index])
			return trafficInterceptMessageRoleMatches(message, role) &&
				trafficInterceptContentKeywordsMatch(trafficInterceptMessageContentText(message["content"]), keywords, match.ContentOp)
		}
	default:
		for index := len(messages) - 1; index >= 0; index-- {
			message := trafficInterceptMap(messages[index])
			if trafficInterceptMessageRoleMatches(message, role) {
				return trafficInterceptContentKeywordsMatch(trafficInterceptMessageContentText(message["content"]), keywords, match.ContentOp)
			}
		}
	}
	return false
}

func trafficInterceptMatchKeywords(content string) []string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	keywords := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			keywords = append(keywords, line)
		}
	}
	return keywords
}

func trafficInterceptContentKeywordsMatch(content string, keywords []string, op string) bool {
	if len(keywords) == 0 {
		return true
	}
	if trafficInterceptMatchOpIsOr(op) {
		for _, keyword := range keywords {
			if strings.Contains(content, keyword) {
				return true
			}
		}
		return false
	}
	for _, keyword := range keywords {
		if !strings.Contains(content, keyword) {
			return false
		}
	}
	return true
}

func trafficInterceptApplyMessageContentRewrite(messages []interface{}, rewrite model.MessageContentRewrite, env map[string]interface{}) bool {
	role := strings.TrimSpace(rewrite.Role)
	if role == "" {
		return false
	}
	content := trafficInterceptMessageRewriteContent(rewrite.Content, env)
	mode := strings.ToLower(strings.TrimSpace(rewrite.Mode))
	if mode == "" {
		mode = "latest"
	}

	switch mode {
	case "all":
		changed := false
		for _, value := range messages {
			message := trafficInterceptMap(value)
			if trafficInterceptMessageRoleMatches(message, role) && trafficInterceptSetMessageContent(message, content) {
				changed = true
			}
		}
		return changed
	case "first":
		for _, value := range messages {
			message := trafficInterceptMap(value)
			if trafficInterceptMessageRoleMatches(message, role) {
				return trafficInterceptSetMessageContent(message, content)
			}
		}
	case "index":
		if rewrite.Index >= 0 && rewrite.Index < len(messages) {
			message := trafficInterceptMap(messages[rewrite.Index])
			if trafficInterceptMessageRoleMatches(message, role) {
				return trafficInterceptSetMessageContent(message, content)
			}
		}
	default:
		for index := len(messages) - 1; index >= 0; index-- {
			message := trafficInterceptMap(messages[index])
			if trafficInterceptMessageRoleMatches(message, role) {
				return trafficInterceptSetMessageContent(message, content)
			}
		}
	}
	return false
}

func trafficInterceptMessageRewriteContent(expression string, env map[string]interface{}) string {
	if strings.TrimSpace(expression) == "" {
		return ""
	}
	out, err := evalTrafficInterceptExpression(expression, env)
	if err != nil {
		return expression
	}
	return anyToString(out, expression)
}

func trafficInterceptMessageRoleMatches(message map[string]interface{}, role string) bool {
	if len(message) == 0 {
		return false
	}
	role = strings.TrimSpace(role)
	if role == "*" || strings.EqualFold(role, "all") {
		return true
	}
	return anyToString(message["role"], "") == role
}

func trafficInterceptSetMessageContent(message map[string]interface{}, content string) bool {
	if len(message) == 0 {
		return false
	}
	switch current := message["content"].(type) {
	case string:
		if current == content {
			return false
		}
		message["content"] = content
		return true
	}

	if items := trafficInterceptSlice(message["content"]); len(items) > 0 {
		changed := false
		wroteFirst := false
		for _, item := range items {
			part := trafficInterceptMap(item)
			if len(part) == 0 {
				continue
			}
			key := trafficInterceptMessageTextPartKey(part)
			if key == "" {
				continue
			}
			next := ""
			if !wroteFirst {
				next = content
				wroteFirst = true
			}
			if anyToString(part[key], "") != next {
				part[key] = next
				changed = true
			}
		}
		if wroteFirst {
			return changed
		}
	}

	message["content"] = content
	return true
}

func trafficInterceptMessageTextPartKey(part map[string]interface{}) string {
	if _, ok := part["text"].(string); ok {
		return "text"
	}
	if _, ok := part["input_text"].(string); ok {
		return "input_text"
	}
	if _, ok := part["content"].(string); ok {
		return "content"
	}
	return ""
}

func trafficInterceptLatestContent(input interface{}) string {
	value := trafficInterceptJSONValue(input)
	root := trafficInterceptMap(value)
	if len(root) == 0 {
		return ""
	}

	if content := trafficInterceptLatestMessageContent(root["messages"]); content != "" {
		return content
	}
	if content := trafficInterceptStringField(root, "input"); content != "" {
		return content
	}
	if content := trafficInterceptStringField(root, "prompt"); content != "" {
		return content
	}
	if content := trafficInterceptLatestMessageContent(root["input"]); content != "" {
		return content
	}
	if content := trafficInterceptMessageContentText(root["content"]); content != "" {
		return content
	}
	return ""
}

func trafficInterceptResponseContent(input interface{}) string {
	parts := make([]string, 0)
	for _, payload := range trafficInterceptResponsePayloads(input) {
		trafficInterceptCollectResponseContent(payload, &parts)
	}
	return strings.Join(parts, "")
}

func trafficInterceptResponseToolCalls(input interface{}) string {
	calls := make([]interface{}, 0)
	for _, payload := range trafficInterceptResponsePayloads(input) {
		trafficInterceptCollectResponseToolCalls(payload, &calls)
	}
	parts := make([]string, 0, len(calls))
	for _, call := range calls {
		if text := anyToString(call, ""); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func trafficInterceptResponseFunctionNames(input interface{}) string {
	calls := make([]interface{}, 0)
	for _, payload := range trafficInterceptResponsePayloads(input) {
		trafficInterceptCollectResponseToolCalls(payload, &calls)
	}
	names := make([]string, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		for _, name := range trafficInterceptFunctionNames(call) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return strings.Join(names, "\n")
}

func trafficInterceptResponsePreview(input interface{}) string {
	content := trafficInterceptResponseContent(input)
	toolCalls := trafficInterceptResponseToolCalls(input)
	parts := make([]string, 0, 2)
	if content != "" {
		parts = append(parts, content)
	}
	if toolCalls != "" {
		parts = append(parts, "tool_calls:\n"+toolCalls)
	}
	return strings.Join(parts, "\n\n")
}

func trafficInterceptJSONValue(input interface{}) interface{} {
	switch v := input.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		var out interface{}
		if err := common.Unmarshal([]byte(text), &out); err != nil {
			return nil
		}
		return out
	default:
		return input
	}
}

func trafficInterceptResponsePayloads(input interface{}) []interface{} {
	switch v := input.(type) {
	case string:
		if payloads := trafficInterceptEventStreamPayloads(v); len(payloads) > 0 {
			return payloads
		}
		value := trafficInterceptJSONValue(v)
		if value == nil {
			return nil
		}
		if items := trafficInterceptSlice(value); len(items) > 0 {
			return items
		}
		return []interface{}{value}
	default:
		if items := trafficInterceptSlice(v); len(items) > 0 {
			return items
		}
		if trafficInterceptMap(v) != nil {
			return []interface{}{v}
		}
		return nil
	}
}

func trafficInterceptEventStreamPayloads(input string) []interface{} {
	if !strings.Contains(input, "data:") && !strings.Contains(input, "event:") {
		return nil
	}
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	blocks := strings.Split(normalized, "\n\n")
	payloads := make([]interface{}, 0, len(blocks))
	for _, block := range blocks {
		data := trafficInterceptEventBlockData(block)
		if data == "" || data == "[DONE]" {
			continue
		}
		if value := trafficInterceptJSONValue(data); value != nil {
			payloads = append(payloads, value)
		}
	}
	return payloads
}

func trafficInterceptEventBlockData(block string) string {
	lines := strings.Split(strings.TrimSpace(block), "\n")
	dataLines := make([]string, 0, len(lines))
	readingData := false
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimLeft(line[5:], " \t"))
			readingData = true
			continue
		}
		if readingData && !trafficInterceptLooksLikeSSEField(line) {
			dataLines = append(dataLines, line)
		}
	}
	return strings.TrimSpace(strings.Join(dataLines, "\n"))
}

func trafficInterceptLooksLikeSSEField(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	index := strings.IndexByte(line, ':')
	if index <= 0 {
		return false
	}
	for _, r := range line[:index] {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

func trafficInterceptLatestMessageContent(input interface{}) string {
	messages := trafficInterceptSlice(input)
	if len(messages) == 0 {
		return ""
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := trafficInterceptMap(messages[index])
		if len(message) == 0 {
			continue
		}
		if role := anyToString(message["role"], ""); role != "user" {
			continue
		}
		if content := trafficInterceptMessageContentText(message["content"]); content != "" {
			return content
		}
	}
	return ""
}

func trafficInterceptMessageContentText(input interface{}) string {
	switch v := input.(type) {
	case string:
		return v
	}
	if items := trafficInterceptSlice(input); len(items) > 0 {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			part := trafficInterceptMap(item)
			if len(part) == 0 {
				continue
			}
			if text := trafficInterceptStringField(part, "text"); text != "" {
				parts = append(parts, text)
				continue
			}
			if text := trafficInterceptStringField(part, "input_text"); text != "" {
				parts = append(parts, text)
				continue
			}
			if text := trafficInterceptStringField(part, "content"); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	part := trafficInterceptMap(input)
	if len(part) == 0 {
		return ""
	}
	if text := trafficInterceptStringField(part, "text"); text != "" {
		return text
	}
	if text := trafficInterceptStringField(part, "input_text"); text != "" {
		return text
	}
	if text := trafficInterceptStringField(part, "content"); text != "" {
		return text
	}
	return ""
}

func trafficInterceptCollectResponseContent(input interface{}, parts *[]string) {
	root := trafficInterceptMap(input)
	if len(root) == 0 {
		return
	}
	if choices := trafficInterceptSlice(root["choices"]); len(choices) > 0 {
		for _, choiceValue := range choices {
			choice := trafficInterceptMap(choiceValue)
			if len(choice) == 0 {
				continue
			}
			trafficInterceptCollectMessageContent(choice["delta"], parts)
			trafficInterceptCollectMessageContent(choice["message"], parts)
			if text := trafficInterceptStringField(choice, "text"); text != "" {
				*parts = append(*parts, text)
			}
		}
	}
	if text := trafficInterceptMessageContentText(root["content"]); text != "" {
		*parts = append(*parts, text)
	}
	if text := trafficInterceptStringField(root, "output_text"); text != "" {
		*parts = append(*parts, text)
	}
	if text := trafficInterceptStringField(root, "text"); text != "" {
		*parts = append(*parts, text)
	}
	if output := trafficInterceptSlice(root["output"]); len(output) > 0 {
		for _, item := range output {
			trafficInterceptCollectResponseContent(item, parts)
			trafficInterceptCollectMessageContent(item, parts)
		}
	}
}

func trafficInterceptCollectMessageContent(input interface{}, parts *[]string) {
	message := trafficInterceptMap(input)
	if len(message) == 0 {
		return
	}
	if text := trafficInterceptMessageContentText(message["content"]); text != "" {
		*parts = append(*parts, text)
	}
	if text := trafficInterceptStringField(message, "output_text"); text != "" {
		*parts = append(*parts, text)
	}
}

func trafficInterceptCollectResponseToolCalls(input interface{}, calls *[]interface{}) {
	root := trafficInterceptMap(input)
	if len(root) == 0 {
		return
	}
	if choices := trafficInterceptSlice(root["choices"]); len(choices) > 0 {
		for _, choiceValue := range choices {
			choice := trafficInterceptMap(choiceValue)
			if len(choice) == 0 {
				continue
			}
			before := len(*calls)
			trafficInterceptCollectToolCalls(choice["delta"], calls)
			trafficInterceptCollectToolCalls(choice["message"], calls)
			if len(*calls) == before {
				trafficInterceptCollectToolCalls(choice, calls)
			}
		}
		return
	}
	trafficInterceptCollectToolCalls(root, calls)
}

func trafficInterceptCollectToolCalls(input interface{}, calls *[]interface{}) {
	if items := trafficInterceptSlice(input); len(items) > 0 {
		for _, item := range items {
			trafficInterceptCollectToolCalls(item, calls)
		}
		return
	}
	value := trafficInterceptMap(input)
	if len(value) == 0 {
		return
	}
	if toolCalls := trafficInterceptSlice(value["tool_calls"]); len(toolCalls) > 0 {
		for _, toolCall := range toolCalls {
			*calls = append(*calls, toolCall)
		}
	}
	if functionCall, ok := value["function_call"]; ok {
		*calls = append(*calls, functionCall)
	}
	if valueType := anyToString(value["type"], ""); valueType == "function_call" {
		*calls = append(*calls, value)
	}
	for key, child := range value {
		switch key {
		case "tool_calls", "function_call":
			continue
		default:
			trafficInterceptCollectToolCalls(child, calls)
		}
	}
}

func trafficInterceptFunctionNames(input interface{}) []string {
	value := trafficInterceptMap(input)
	if len(value) == 0 {
		return nil
	}
	names := make([]string, 0, 2)
	if fn := trafficInterceptMap(value["function"]); len(fn) > 0 {
		if name := trafficInterceptStringField(fn, "name"); name != "" {
			names = append(names, name)
		}
	}
	if name := trafficInterceptStringField(value, "name"); name != "" {
		names = append(names, name)
	}
	return names
}

func trafficInterceptSlice(input interface{}) []interface{} {
	if input == nil {
		return nil
	}
	if items, ok := input.([]interface{}); ok {
		return items
	}
	rv := reflect.ValueOf(input)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]interface{}, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out
}

func trafficInterceptStringField(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
	value, ok := input[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func evalTrafficInterceptExpression(expression string, env map[string]interface{}) (interface{}, error) {
	return expr.Eval(expression, env)
}

func evalTrafficInterceptString(expression string, env map[string]interface{}) (string, bool) {
	out, err := evalTrafficInterceptExpression(expression, env)
	if err != nil {
		common.SysLog("traffic intercept expression eval error: " + err.Error())
		return "", false
	}
	return anyToString(out, ""), true
}

func applyTrafficInterceptHeaderOps(header http.Header, opsJSON string, env map[string]interface{}) {
	var ops []model.HeaderOp
	if err := common.Unmarshal([]byte(opsJSON), &ops); err != nil {
		common.SysLog("traffic intercept header ops JSON error: " + err.Error())
		return
	}
	for _, op := range ops {
		name := strings.TrimSpace(op.Key)
		if name == "" || shouldSkipTrafficInterceptHeader(name) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(op.Op)) {
		case "set":
			value := op.Value
			if strings.TrimSpace(op.Value) != "" {
				if out, err := evalTrafficInterceptExpression(op.Value, env); err == nil {
					value = anyToString(out, value)
				}
			}
			header.Set(name, value)
		case "remove", "delete", "del":
			header.Del(name)
		}
	}
}

func trafficInterceptHeaderOpsMap(opsJSON string, env map[string]interface{}) map[string]string {
	if strings.TrimSpace(opsJSON) == "" {
		return nil
	}
	header := http.Header{}
	applyTrafficInterceptHeaderOps(header, opsJSON, env)
	return headerToStringMap(header)
}

func applyHeaderMap(header http.Header, values map[string]string) {
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" || shouldSkipTrafficInterceptHeader(key) {
			continue
		}
		header.Set(key, value)
	}
}

func applyHeaderActionMap(header http.Header, input interface{}) {
	if header == nil || input == nil {
		return
	}
	for key, value := range trafficInterceptMap(input) {
		key = strings.TrimSpace(key)
		if key == "" || shouldSkipTrafficInterceptHeader(key) {
			continue
		}
		if value == nil {
			header.Del(key)
			continue
		}
		text := anyToString(value, "")
		if strings.EqualFold(strings.TrimSpace(text), "__delete__") {
			header.Del(key)
			continue
		}
		header.Set(key, text)
	}
}

func shouldSkipTrafficInterceptHeader(name string) bool {
	_, ok := trafficInterceptHopByHopHeaders[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func writeTrafficInterceptBlock(c *gin.Context, status int, contentType string, body string, headers map[string]string) {
	if status == 0 {
		status = http.StatusForbidden
	}
	if contentType == "" {
		contentType = "application/json"
	}
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			continue
		}
		if !shouldSkipTrafficInterceptHeader(key) {
			c.Header(key, value)
		}
	}
	c.Data(status, contentType, []byte(body))
	c.Abort()
}

func readGinRequestBody(c *gin.Context) string {
	if c == nil {
		return ""
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return ""
	}
	data, err := storage.Bytes()
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil && err == nil {
		err = seekErr
	}
	if err != nil {
		common.SysLog("traffic intercept failed to read request body: " + err.Error())
		return ""
	}
	c.Request.Body = io.NopCloser(storage)
	return string(data)
}

func trafficInterceptOriginalRequestBody(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(trafficInterceptOriginalRequestBodyKey); ok {
		if body, ok := value.(string); ok {
			return body
		}
	}
	body := readGinRequestBody(c)
	if body != "" {
		c.Set(trafficInterceptOriginalRequestBodyKey, body)
	}
	return body
}

func setTrafficInterceptLoggedRequest(c *gin.Context, reqCtx *TrafficInterceptRequestContext) {
	if c == nil || reqCtx == nil {
		return
	}
	if reqCtx.Body != "" {
		c.Set(TrafficInterceptLoggedRequestBodyKey, reqCtx.Body)
	}
	if len(reqCtx.Header) == 0 {
		return
	}
	headers := make(map[string]string, len(reqCtx.Header))
	for key, value := range reqCtx.Header {
		headers[key] = value
	}
	c.Set(TrafficInterceptLoggedRequestHeadersKey, headers)
}

func readAndRestoreHTTPRequestBody(req *http.Request) (string, error) {
	if req == nil || req.Body == nil {
		return "", nil
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}
	_ = req.Body.Close()
	setHTTPRequestBody(req, string(data))
	return string(data), nil
}

func setHTTPRequestBody(req *http.Request, body string) {
	if req == nil {
		return
	}
	data := []byte(body)
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Length", strconv.Itoa(len(data)))
}

func setHTTPResponseBody(resp *http.Response, body string) {
	if resp == nil {
		return
	}
	data := []byte(body)
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	resp.Header.Set("Content-Length", strconv.Itoa(len(data)))
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Transfer-Encoding")
}

func applyHTTPRequestURL(req *http.Request, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || req == nil || req.URL == nil {
		return nil
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return err
		}
		req.URL = parsed
		req.Host = parsed.Host
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Path != "" {
		req.URL.Path = parsed.Path
	}
	if parsed.RawQuery != "" || strings.Contains(raw, "?") {
		req.URL.RawQuery = parsed.RawQuery
	}
	return nil
}

func applyHTTPResponseURL(req *http.Request, resp *http.Response, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return responseURL(req, resp)
	}
	finalURL := raw
	if parsed, err := url.Parse(raw); err == nil {
		if parsed.IsAbs() {
			if resp != nil && resp.Request != nil {
				resp.Request.URL = parsed
			}
			if req != nil {
				req.URL = parsed
			}
		} else if base := responseRequestURL(req, resp); base != nil {
			clone := *base
			if parsed.Path != "" {
				clone.Path = parsed.Path
			}
			if parsed.RawQuery != "" || strings.Contains(raw, "?") {
				clone.RawQuery = parsed.RawQuery
			}
			if resp != nil && resp.Request != nil {
				resp.Request.URL = &clone
			}
			if req != nil {
				req.URL = &clone
			}
			finalURL = clone.String()
		}
	}
	if resp != nil {
		resp.Header.Set("X-Traffic-Intercept-Response-URL", finalURL)
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			resp.Header.Set("Location", finalURL)
		}
	}
	return finalURL
}

func updateRequestURLContext(ctx *TrafficInterceptRequestContext, req *http.Request) {
	if ctx == nil || req == nil || req.URL == nil {
		return
	}
	ctx.UpstreamURL = req.URL.String()
	ctx.URL = req.URL.String()
	ctx.Path = req.URL.Path
}

func responseRequestURL(req *http.Request, resp *http.Response) *url.URL {
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL
	}
	if req != nil && req.URL != nil {
		return req.URL
	}
	return nil
}

func responseURL(req *http.Request, resp *http.Response) string {
	if u := responseRequestURL(req, resp); u != nil {
		return u.String()
	}
	return ""
}

func headerToStringMap(header http.Header) map[string]string {
	out := make(map[string]string, len(header))
	for key := range header {
		out[key] = header.Get(key)
	}
	return out
}

func trafficInterceptMap(input interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	if v, ok := input.(map[string]interface{}); ok {
		return v
	}
	rv := reflect.ValueOf(input)
	if rv.Kind() != reflect.Map {
		return nil
	}
	out := make(map[string]interface{}, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key := fmt.Sprint(iter.Key().Interface())
		out[key] = iter.Value().Interface()
	}
	return out
}

func trafficInterceptStringMap(input interface{}) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	switch v := input.(type) {
	case map[string]string:
		return v
	case map[string]interface{}:
		out := make(map[string]string, len(v))
		for key, value := range v {
			out[key] = anyToString(value, "")
		}
		return out
	}
	rv := reflect.ValueOf(input)
	if rv.Kind() != reflect.Map {
		return map[string]string{}
	}
	out := make(map[string]string, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[fmt.Sprint(iter.Key().Interface())] = anyToString(iter.Value().Interface(), "")
	}
	return out
}

func trafficInterceptMerge(left interface{}, right interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for key, value := range trafficInterceptMap(left) {
		out[key] = value
	}
	for key, value := range trafficInterceptMap(right) {
		out[key] = value
	}
	return out
}

func trafficInterceptSet(input interface{}, key string, value interface{}) map[string]interface{} {
	out := trafficInterceptMerge(input, nil)
	if strings.TrimSpace(key) != "" {
		out[key] = value
	}
	return out
}

func trafficInterceptDel(input interface{}, key string) map[string]interface{} {
	out := trafficInterceptMerge(input, nil)
	delete(out, key)
	return out
}

func lookupTrafficInterceptHeader(headers map[string]string, name string) string {
	name = strings.TrimSpace(name)
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func hasAction(action map[string]interface{}, key string) bool {
	_, ok := action[key]
	return ok
}

func actionString(action map[string]interface{}, key string) (string, bool) {
	value, ok := action[key]
	if !ok {
		return "", false
	}
	return anyToString(value, ""), true
}

func trafficInterceptMapString(input map[string]interface{}, key string) (string, bool) {
	value, ok := input[key]
	if !ok {
		return "", false
	}
	return anyToString(value, ""), true
}

func trafficInterceptFirstMapString(input map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := trafficInterceptMapString(input, key); ok {
			return value, true
		}
	}
	return "", false
}

func anyToString(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		data, err := common.Marshal(value)
		if err == nil {
			return string(data)
		}
	}
	return fmt.Sprint(value)
}

func anyToInt(value interface{}, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return fallback
}

func truthy(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return anyToInt(v, 0) != 0
	default:
		return value != nil
	}
}

func isTrafficInterceptWebsocket(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.EqualFold(c.GetHeader("Upgrade"), "websocket")
}
