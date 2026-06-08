package service

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/expr-lang/expr"
)

func TestMatchTrafficInterceptRuleUser(t *testing.T) {
	rule := &model.TrafficInterceptRule{
		RequestMatchEnabled: true,
		UserId:              12,
		Username:            "alice",
	}
	cr := &trafficInterceptCachedRule{Rule: rule}

	if !matchTrafficInterceptRule(cr, &TrafficInterceptRequestContext{UserId: 12, Username: "alice"}) {
		t.Fatal("expected matching user to match rule")
	}
	if !matchTrafficInterceptRule(cr, &TrafficInterceptRequestContext{UserId: 12, Username: "renamed"}) {
		t.Fatal("expected user id to match even when username changed")
	}
	if matchTrafficInterceptRule(cr, &TrafficInterceptRequestContext{UserId: 13, Username: "alice"}) {
		t.Fatal("expected different user id to miss rule")
	}

	usernameRule := &trafficInterceptCachedRule{Rule: &model.TrafficInterceptRule{RequestMatchEnabled: true, Username: "alice"}}
	if !matchTrafficInterceptRule(usernameRule, &TrafficInterceptRequestContext{Username: "alice"}) {
		t.Fatal("expected matching username to match rule")
	}
	if matchTrafficInterceptRule(usernameRule, &TrafficInterceptRequestContext{Username: "bob"}) {
		t.Fatal("expected different username to miss username-only rule")
	}
}

func TestMatchTrafficInterceptResponseCondition(t *testing.T) {
	cr := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			ResponseMatchEnabled: true,
			ResponseContentMatch: "target",
		},
	}
	reqCtx := &TrafficInterceptRequestContext{Path: "/v1/chat/completions"}

	if !matchTrafficInterceptResponseCandidateRule(cr, reqCtx) {
		t.Fatal("expected request-side candidate match without request condition")
	}
	if !matchTrafficInterceptResponseCondition(cr, reqCtx, &TrafficInterceptResponseContext{Body: `{"content":"target"}`}) {
		t.Fatal("expected response body to match response condition")
	}
	if matchTrafficInterceptResponseCondition(cr, reqCtx, &TrafficInterceptResponseContext{Body: `{"content":"other"}`}) {
		t.Fatal("expected different response body to miss response condition")
	}
	if !matchTrafficInterceptRequestRule(cr, reqCtx) {
		t.Fatal("response condition should not block request-side actions")
	}
}

func TestMatchTrafficInterceptResponseUsesConfiguredRequestMatch(t *testing.T) {
	responseOnly := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptRequest:     false,
			InterceptResponse:    true,
			ResponseMatchEnabled: true,
			ResponseContentMatch: "answer",
		},
	}

	matchingReq := &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"ask"}]}`}
	if !matchTrafficInterceptResponseCandidateRule(responseOnly, matchingReq) {
		t.Fatal("expected response-only candidate to match without request match fields")
	}
	if !matchTrafficInterceptResponseCondition(responseOnly, &TrafficInterceptRequestContext{Body: `{"messages":[]}`}, &TrafficInterceptResponseContext{Status: 200, Body: `{"content":"answer"}`}) {
		t.Fatal("expected response-only condition to match without request match fields")
	}

	responseWithRequestMatch := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptRequest:      false,
			InterceptResponse:     true,
			RequestMatchEnabled:   true,
			ResponseMatchEnabled:  true,
			RequestMessageMatches: `[{"role":"user","mode":"latest","content":"ask"}]`,
			ResponseContentMatch:  "answer",
		},
	}
	if !matchTrafficInterceptResponseCandidateRule(responseWithRequestMatch, matchingReq) {
		t.Fatal("expected response candidate to match when configured request condition matches")
	}
	if matchTrafficInterceptResponseCandidateRule(responseWithRequestMatch, &TrafficInterceptRequestContext{Body: `{"messages":[]}`}) {
		t.Fatal("expected configured request condition to block response candidate")
	}
	if !matchTrafficInterceptResponseCondition(responseWithRequestMatch, matchingReq, &TrafficInterceptResponseContext{Status: 200, Body: `{"content":"answer"}`}) {
		t.Fatal("expected configured request and response conditions to match together")
	}
	if matchTrafficInterceptResponseCondition(responseWithRequestMatch, &TrafficInterceptRequestContext{Body: `{"messages":[]}`}, &TrafficInterceptResponseContext{Status: 200, Body: `{"content":"answer"}`}) {
		t.Fatal("expected request condition mismatch to block response condition")
	}
	if matchTrafficInterceptResponseCondition(responseWithRequestMatch, matchingReq, &TrafficInterceptResponseContext{Status: 200, Body: `{"content":"other"}`}) {
		t.Fatal("expected response content mismatch to miss response condition")
	}
}

func TestTrafficInterceptDirectContentMatch(t *testing.T) {
	requestRule := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			RequestMatchEnabled:   true,
			RequestMessageMatches: `[{"role":"user","mode":"latest","content":"latest question"}]`,
		},
	}
	if !matchTrafficInterceptRequestRule(requestRule, &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"old"},{"role":"user","content":"latest question"}]}`}) {
		t.Fatal("expected latest request content to match")
	}
	if matchTrafficInterceptRequestRule(requestRule, &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"old"}]}`}) {
		t.Fatal("expected request content mismatch to miss")
	}
	if matchTrafficInterceptRequestRule(requestRule, &TrafficInterceptRequestContext{Body: `{"messages":[{"content":"latest question"}]}`}) {
		t.Fatal("expected message without user role to miss")
	}
	if matchTrafficInterceptRequestRule(requestRule, &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"old"},{"role":"assistant","content":"latest question"}]}`}) {
		t.Fatal("expected assistant content to be ignored for request match")
	}

	fallbackRule := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			RequestMatchEnabled: true,
			RequestContentMatch: "fallback question",
		},
	}
	if !matchTrafficInterceptRequestRule(fallbackRule, &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"fallback question"}]}`}) {
		t.Fatal("expected legacy request content match to remain as a fallback")
	}

	responseRule := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			ResponseMatchEnabled:   true,
			ResponseContentMatch:   "answer",
			ResponseToolCallsMatch: "lookup",
		},
	}
	respCtx := &TrafficInterceptResponseContext{Body: `{"choices":[{"message":{"content":"answer","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`}
	if !matchTrafficInterceptResponseCondition(responseRule, &TrafficInterceptRequestContext{}, respCtx) {
		t.Fatal("expected response content and tool_calls to match")
	}
	if matchTrafficInterceptResponseCondition(responseRule, &TrafficInterceptRequestContext{}, &TrafficInterceptResponseContext{Body: `{"choices":[{"message":{"content":"answer"}}]}`}) {
		t.Fatal("expected missing tool_calls match to miss")
	}
	if matchTrafficInterceptResponseCondition(responseRule, &TrafficInterceptRequestContext{}, &TrafficInterceptResponseContext{Body: `{"choices":[{"message":{"content":"answer"}}],"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{}"}}]}]}`}) {
		t.Fatal("expected historical tool_calls outside response choices to be ignored")
	}
}

func TestTrafficInterceptMessagesContentMatchModes(t *testing.T) {
	body := `{"messages":[{"role":"system","content":"system rules"},{"role":"user","content":"first question"},{"role":"assistant","content":"assistant reply"},{"role":"user","content":[{"type":"text","text":"latest"},{"type":"text","text":"question"}]}]}`
	rule := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			RequestMatchEnabled: true,
			RequestMessageMatches: `[
				{"role":"system","mode":"first","content":"rules"},
				{"role":"assistant","mode":"index","index":2,"content":"reply"},
				{"role":"user","mode":"latest","content":"latest\nquestion"},
				{"role":"user","mode":"all","content":"first question"}
			]`,
		},
	}
	if !matchTrafficInterceptRequestRule(rule, &TrafficInterceptRequestContext{Body: body}) {
		t.Fatal("expected all configured message content matches to pass")
	}

	rule.Rule.RequestMessageMatches = `[
		{"role":"system","mode":"first","content":"rules"},
		{"role":"user","mode":"latest","content":"missing"}
	]`
	if matchTrafficInterceptRequestRule(rule, &TrafficInterceptRequestContext{Body: body}) {
		t.Fatal("expected message content matches to be ANDed")
	}

	rule.Rule.RequestMessageMatchOp = "or"
	if !matchTrafficInterceptRequestRule(rule, &TrafficInterceptRequestContext{Body: body}) {
		t.Fatal("expected OR message content matches to pass when any configured message matches")
	}
	rule.Rule.RequestMessageMatchOp = ""

	rule.Rule.RequestMessageMatches = `[{"role":"assistant","mode":"latest","content":"latest"}]`
	if matchTrafficInterceptRequestRule(rule, &TrafficInterceptRequestContext{Body: body}) {
		t.Fatal("expected role-specific latest match to ignore user messages")
	}
}

func TestTrafficInterceptMessageContentKeywordMatchOp(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"alpha beta"}]}`
	rule := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			RequestMatchEnabled:   true,
			RequestMessageMatches: `[{"role":"user","mode":"latest","content":"alpha\nbeta","content_op":"and"}]`,
		},
	}
	if !matchTrafficInterceptRequestRule(rule, &TrafficInterceptRequestContext{Body: body}) {
		t.Fatal("expected keyword AND match to pass when all keywords are present")
	}

	rule.Rule.RequestMessageMatches = `[{"role":"user","mode":"latest","content":"alpha\ngamma","content_op":"and"}]`
	if matchTrafficInterceptRequestRule(rule, &TrafficInterceptRequestContext{Body: body}) {
		t.Fatal("expected keyword AND match to miss when one keyword is absent")
	}

	rule.Rule.RequestMessageMatches = `[{"role":"user","mode":"latest","content":"alpha\ngamma","content_op":"or"}]`
	if !matchTrafficInterceptRequestRule(rule, &TrafficInterceptRequestContext{Body: body}) {
		t.Fatal("expected keyword OR match to pass when any keyword is present")
	}

	rule.Rule.RequestMessageMatches = `[{"role":"user","mode":"latest","content":"gamma\ndelta","content_op":"or"}]`
	if matchTrafficInterceptRequestRule(rule, &TrafficInterceptRequestContext{Body: body}) {
		t.Fatal("expected keyword OR match to miss when no keyword is present")
	}
}

func TestTrafficInterceptResponseMatchOp(t *testing.T) {
	body := `{"choices":[{"message":{"content":"answer","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`
	if !trafficInterceptResponseMatches(body, "answer", "lookup", "") {
		t.Fatal("expected default response match operation to require both matching configured conditions")
	}
	if trafficInterceptResponseMatches(body, "missing", "lookup", "") {
		t.Fatal("expected default response match operation to miss when one configured condition misses")
	}
	if !trafficInterceptResponseMatches(body, "missing", "lookup", "or") {
		t.Fatal("expected OR response match operation to pass when tool_calls match")
	}
	if !trafficInterceptResponseMatches(body, "answer", "missing", "or") {
		t.Fatal("expected OR response match operation to pass when content matches")
	}
	if trafficInterceptResponseMatches(body, "missing", "other", "or") {
		t.Fatal("expected OR response match operation to miss when no configured condition matches")
	}
}

func TestMatchTrafficInterceptCommonBaseGatesRequestAndResponse(t *testing.T) {
	cr := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			PathPattern: "^/shared$",
			Method:      http.MethodPost,
			UserId:      7,
		},
		pathRegex: regexp.MustCompile("^/shared$"),
	}
	matchingReq := &TrafficInterceptRequestContext{Path: "/shared", Method: http.MethodPost, UserId: 7}

	if !matchTrafficInterceptRequestRule(cr, matchingReq) {
		t.Fatal("expected shared basic fields to match request rule")
	}
	if !matchTrafficInterceptResponseCandidateRule(cr, matchingReq) {
		t.Fatal("expected shared basic fields to match response candidate")
	}
	if matchTrafficInterceptRequestRule(cr, &TrafficInterceptRequestContext{Path: "/other", Method: http.MethodPost, UserId: 7}) {
		t.Fatal("expected shared path pattern to gate request rule")
	}
	if matchTrafficInterceptResponseCandidateRule(cr, &TrafficInterceptRequestContext{Path: "/shared", Method: http.MethodGet, UserId: 7}) {
		t.Fatal("expected shared method to gate response candidate")
	}
	if matchTrafficInterceptResponseCandidateRule(cr, &TrafficInterceptRequestContext{Path: "/shared", Method: http.MethodPost, UserId: 8}) {
		t.Fatal("expected shared user id to gate response candidate")
	}

	legacy := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			ResponseUserId:      9,
			ResponsePathPattern: "^/legacy-response$",
			ResponseMethod:      http.MethodPost,
		},
		pathRegex: regexp.MustCompile("^/legacy-response$"),
	}
	if !matchTrafficInterceptResponseCandidateRule(legacy, &TrafficInterceptRequestContext{Path: "/legacy-response", Method: http.MethodPost, UserId: 9}) {
		t.Fatal("expected legacy response basic fields to be treated as shared basic fields")
	}
	if matchTrafficInterceptResponseCandidateRule(legacy, &TrafficInterceptRequestContext{Path: "/other", Method: http.MethodPost, UserId: 9}) {
		t.Fatal("expected legacy response path fallback to gate response candidate")
	}
}

func TestTrafficInterceptMatchSwitchSemantics(t *testing.T) {
	requestGlobal := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptRequest:    true,
			RequestMatchEnabled: false,
			PathPattern:         "^/only-this-path$",
			RequestContentMatch: "only this content",
		},
		pathRegex: regexp.MustCompile("^/only-this-path$"),
	}
	if matchTrafficInterceptRequestRule(requestGlobal, &TrafficInterceptRequestContext{Path: "/other", Body: `{"messages":[]}`}) {
		t.Fatal("expected shared basic path to gate request even when request match is disabled")
	}
	if !matchTrafficInterceptRequestRule(requestGlobal, &TrafficInterceptRequestContext{Path: "/only-this-path", Body: `{"messages":[]}`}) {
		t.Fatal("expected disabled request match to ignore stale request body condition after shared basic match")
	}

	requestMatched := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptRequest:      true,
			RequestMatchEnabled:   true,
			RequestMessageMatches: `[{"role":"user","mode":"latest","content":"latest question"}]`,
		},
	}
	if !matchTrafficInterceptRequestRule(requestMatched, &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"latest question"}]}`}) {
		t.Fatal("expected enabled request match to pass when latest user content matches")
	}
	if matchTrafficInterceptRequestRule(requestMatched, &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"other"}]}`}) {
		t.Fatal("expected enabled request match to require latest user content")
	}

	responseByRequestOnly := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptResponse:     true,
			RequestMatchEnabled:   true,
			ResponseMatchEnabled:  false,
			RequestMessageMatches: `[{"role":"user","mode":"latest","content":"ask"}]`,
			ResponseContentMatch:  "ignored because response match is disabled",
		},
	}
	matchingReq := &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"ask"}]}`}
	if !matchTrafficInterceptResponseCandidateRule(responseByRequestOnly, matchingReq) {
		t.Fatal("expected response rewrite candidate to use enabled request match")
	}
	if matchTrafficInterceptResponseCandidateRule(responseByRequestOnly, &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"other"}]}`}) {
		t.Fatal("expected request match mismatch to block response rewrite candidate")
	}
	if !matchTrafficInterceptResponseCondition(responseByRequestOnly, matchingReq, &TrafficInterceptResponseContext{Body: `{"content":"other"}`}) {
		t.Fatal("expected disabled response match to pass after request match passes")
	}

	responseOnly := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptResponse:    true,
			RequestMatchEnabled:  false,
			ResponseMatchEnabled: true,
			RequestContentMatch:  "ignored because request match is disabled",
			ResponseContentMatch: "answer",
		},
	}
	if !matchTrafficInterceptResponseCandidateRule(responseOnly, &TrafficInterceptRequestContext{Body: `{"messages":[{"role":"user","content":"other"}]}`}) {
		t.Fatal("expected disabled request match not to block response-only matching")
	}
	if !matchTrafficInterceptResponseCondition(responseOnly, &TrafficInterceptRequestContext{}, &TrafficInterceptResponseContext{Body: `{"content":"answer"}`}) {
		t.Fatal("expected enabled response match to pass matching response content")
	}
	if matchTrafficInterceptResponseCondition(responseOnly, &TrafficInterceptRequestContext{}, &TrafficInterceptResponseContext{Body: `{"content":"other"}`}) {
		t.Fatal("expected enabled response match to require response content")
	}

	both := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptResponse:     true,
			RequestMatchEnabled:   true,
			ResponseMatchEnabled:  true,
			RequestMessageMatches: `[{"role":"user","mode":"latest","content":"ask"}]`,
			ResponseContentMatch:  "answer",
		},
	}
	if !matchTrafficInterceptResponseCondition(both, matchingReq, &TrafficInterceptResponseContext{Body: `{"content":"answer"}`}) {
		t.Fatal("expected request and response matches to be ANDed")
	}
	if matchTrafficInterceptResponseCondition(both, matchingReq, &TrafficInterceptResponseContext{Body: `{"content":"other"}`}) {
		t.Fatal("expected response mismatch to block when both matches are enabled")
	}
}

func TestTrafficInterceptBodyReadHelpersRespectMatchSwitches(t *testing.T) {
	requestMatchDisabled := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptRequest:    true,
			RequestMatchEnabled: false,
			RequestContentMatch: "stale",
		},
	}
	if trafficRulesNeedRequestBody([]*trafficInterceptCachedRule{requestMatchDisabled}, true) {
		t.Fatal("disabled request match should not force reading request body")
	}

	requestMatchEnabled := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptRequest:      true,
			RequestMatchEnabled:   true,
			RequestMessageMatches: `[{"role":"user","mode":"latest","content":"latest"}]`,
		},
	}
	if !trafficRulesNeedRequestBody([]*trafficInterceptCachedRule{requestMatchEnabled}, true) {
		t.Fatal("enabled request message content match should read request body")
	}

	responseRequestMatch := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptResponse:     true,
			RequestMatchEnabled:   true,
			RequestMessageMatches: `[{"role":"user","mode":"latest","content":"ask"}]`,
		},
	}
	if !trafficResponseRulesNeedRequestBody([]*trafficInterceptCachedRule{responseRequestMatch}) {
		t.Fatal("response rewrite gated by request message content match should preserve request body")
	}

	responseRequestMatchDisabled := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptResponse:   true,
			RequestMatchEnabled: false,
			RequestContentMatch: "stale",
		},
	}
	if trafficResponseRulesNeedRequestBody([]*trafficInterceptCachedRule{responseRequestMatchDisabled}) {
		t.Fatal("disabled request match should not preserve request body for response rules")
	}

	responseMatchDisabled := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptResponse:    true,
			ResponseMatchEnabled: false,
			ResponseContentMatch: "stale",
		},
	}
	if trafficRulesNeedResponseBody([]*trafficInterceptCachedRule{responseMatchDisabled}) {
		t.Fatal("disabled response match should not force reading response body")
	}

	responseMatchEnabled := &trafficInterceptCachedRule{
		Rule: &model.TrafficInterceptRule{
			InterceptResponse:    true,
			ResponseMatchEnabled: true,
			ResponseContentMatch: "answer",
		},
	}
	if !trafficRulesNeedResponseBody([]*trafficInterceptCachedRule{responseMatchEnabled}) {
		t.Fatal("enabled response content match should read response body")
	}
}

func TestTrafficInterceptMatchLimitAndActionHelpers(t *testing.T) {
	if !trafficInterceptRuleMatchLimitReached(&trafficInterceptCachedRule{Rule: &model.TrafficInterceptRule{MatchLimit: 1, MatchCount: 1}}) {
		t.Fatal("expected finite rule at its match limit to be exhausted")
	}
	if trafficInterceptRuleMatchLimitReached(&trafficInterceptCachedRule{Rule: &model.TrafficInterceptRule{MatchLimit: 1, MatchCount: 0}}) {
		t.Fatal("expected finite rule below its match limit to remain active")
	}
	if trafficInterceptRuleMatchLimitReached(&trafficInterceptCachedRule{Rule: &model.TrafficInterceptRule{MatchLimit: 0, MatchCount: 100}}) {
		t.Fatal("expected unlimited rule to remain active regardless of match count")
	}
	if trafficInterceptRuleHasRequestRewriteAction(&model.TrafficInterceptRule{RequestHeaderOps: "[]"}) {
		t.Fatal("empty request header ops should not count as a rewrite action")
	}
	if trafficInterceptRuleHasRequestRewriteAction(&model.TrafficInterceptRule{RequestBodyRewrite: `"body"`}) {
		t.Fatal("request body rewrite should no longer count as a rewrite action")
	}
	if trafficInterceptRuleHasRequestRewriteAction(&model.TrafficInterceptRule{RequestURLRewrite: `"/v1/other"`}) {
		t.Fatal("request URL rewrite should no longer count as a rewrite action")
	}
	if !trafficInterceptRuleHasRequestRewriteAction(&model.TrafficInterceptRule{RequestMessageRewrites: `[{"role":"user","mode":"latest","content":"new"}]`}) {
		t.Fatal("message content rewrite should count as a request rewrite action")
	}
	if trafficInterceptRuleHasResponseRewriteAction(&model.TrafficInterceptRule{ResponseHeaderOps: "[]", ResponseBodyRewrite: `"body"`}, false, false) {
		t.Fatal("legacy full response body rewrite should not count as an executable response action")
	}
	if !trafficInterceptRuleHasResponseRewriteAction(&model.TrafficInterceptRule{ResponseContentRewrite: `"body"`}, true, false) {
		t.Fatal("response content rewrite should count as a stream response action")
	}
	if !trafficInterceptRuleHasResponseRewriteAction(&model.TrafficInterceptRule{ResponseToolCallsRewrite: `[{"type":"function"}]`}, true, false) {
		t.Fatal("response tool_calls rewrite should count as a stream response action")
	}
	if !trafficInterceptRuleHasResponseRewriteAction(&model.TrafficInterceptRule{ResponseStatusRewrite: "200"}, true, false) {
		t.Fatal("response status rewrite should count as a response action")
	}
	if !trafficInterceptRuleHasScriptAction(&model.TrafficInterceptRule{ScriptEnabled: true, Script: "async function onRequest() {}"}) {
		t.Fatal("enabled script should count as a script action")
	}
	if trafficInterceptRuleHasScriptAction(&model.TrafficInterceptRule{ScriptEnabled: false, Script: "async function onRequest() {}"}) {
		t.Fatal("disabled script should not count as a script action")
	}
}

func TestTrafficInterceptConsumeRuleMatchInvalidatesCache(t *testing.T) {
	trafficInterceptCacheMu.Lock()
	trafficInterceptCache = []*trafficInterceptCachedRule{}
	trafficInterceptCacheTime = time.Now()
	trafficInterceptCacheMu.Unlock()

	oldConsume := consumeTrafficInterceptRuleMatch
	consumeTrafficInterceptRuleMatch = func(id int) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() {
		consumeTrafficInterceptRuleMatch = oldConsume
	})

	if !trafficInterceptConsumeRuleMatch(nil, &trafficInterceptCachedRule{Rule: &model.TrafficInterceptRule{Id: 1, MatchLimit: 1}}) {
		t.Fatal("expected rule match consumption to succeed")
	}

	trafficInterceptCacheMu.RLock()
	defer trafficInterceptCacheMu.RUnlock()
	if trafficInterceptCache != nil || !trafficInterceptCacheTime.IsZero() {
		t.Fatal("expected rule cache to be invalidated after finite match consumption")
	}
}

func TestTrafficInterceptMessageContentRewrites(t *testing.T) {
	body := `{"messages":[{"role":"system","content":"old system"},{"role":"user","content":"old user"},{"role":"assistant","content":"old assistant"},{"role":"user","content":[{"type":"text","text":"latest user"},{"type":"text","text":"tail"}]}]}`
	rewrites := `[{"role":"system","mode":"all","content":"new system"},{"role":"user","mode":"latest","content":"new user"},{"role":"assistant","mode":"first","content":"\"new assistant\""}]`
	rewritten, ok := applyTrafficInterceptMessageContentRewrites(body, rewrites, trafficInterceptEnv(&TrafficInterceptRequestContext{Body: body}, nil))
	if !ok {
		t.Fatal("expected message content rewrite to apply")
	}
	if !strings.Contains(rewritten, `"content":"new system"`) {
		t.Fatalf("expected system content to be rewritten, got %s", rewritten)
	}
	if !strings.Contains(rewritten, `"content":"new assistant"`) {
		t.Fatalf("expected assistant content expression to be rewritten, got %s", rewritten)
	}
	if got := trafficInterceptLatestContent(rewritten); got != "new user" {
		t.Fatalf("expected latest user content to be rewritten, got %q", got)
	}
}

func TestTrafficInterceptResponseContentRewrite(t *testing.T) {
	if got := trafficInterceptResponseContentRewriteValue("这是我的真实数据", trafficInterceptEnv(&TrafficInterceptRequestContext{}, &TrafficInterceptResponseContext{})); got != "这是我的真实数据" {
		t.Fatalf("expected raw Chinese rewrite content to be preserved, got %q", got)
	}

	body := `{"choices":[{"message":{"role":"assistant","content":"old","reasoning_content":"keep reasoning"}}],"usage":{"total_tokens":1}}`
	rewritten, ok := applyTrafficInterceptResponseRewrites(
		body,
		`"new content"`,
		"",
		trafficInterceptEnv(&TrafficInterceptRequestContext{}, &TrafficInterceptResponseContext{Body: body}),
	)
	if !ok {
		t.Fatal("expected response content rewrite to apply")
	}
	if got := trafficInterceptResponseContent(rewritten); got != "new content" {
		t.Fatalf("expected rewritten response content, got %q in %s", got, rewritten)
	}
	if !strings.Contains(rewritten, `"reasoning_content":"keep reasoning"`) {
		t.Fatalf("expected reasoning content to be preserved, got %s", rewritten)
	}
}

func TestTrafficInterceptJSScriptOnRequest(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hello"}],"client_metadata":{"debug":true}}`
	req, err := http.NewRequest("POST", "http://upstream.example/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	reqCtx := &TrafficInterceptRequestContext{
		Method:      req.Method,
		URL:         req.URL.String(),
		UpstreamURL: req.URL.String(),
		Path:        req.URL.Path,
		ContentType: req.Header.Get("Content-Type"),
		Header:      headerToStringMap(req.Header),
		Headers:     headerToStringMap(req.Header),
		Body:        body,
	}

	script := `
async function onRequest(context, request) {
  if (request.body && request.headers["content-type"] && request.headers["content-type"].includes("application/json")) {
    var body = JSON.parse(request.body);
    delete body.client_metadata;
    request.body = JSON.stringify(body);
    request.headers["content-length"] = String(request.body.length);
  }
  return request;
}`
	output, ran, err := runTrafficInterceptJSHook(script, "onRequest", &model.TrafficInterceptRule{Id: 9}, reqCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("expected onRequest hook to run")
	}
	applyTrafficInterceptScriptRequest(req, reqCtx, output.Request)
	rewritten, err := readAndRestoreHTTPRequestBody(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rewritten, "client_metadata") {
		t.Fatalf("expected client_metadata to be removed, got %s", rewritten)
	}
	if req.Header.Get("Content-Length") != strconv.Itoa(len(rewritten)) {
		t.Fatalf("expected content-length to be updated, got %q for body %q", req.Header.Get("Content-Length"), rewritten)
	}
}

func TestTrafficInterceptJSScriptOnResponse(t *testing.T) {
	req, err := http.NewRequest("POST", "http://upstream.example/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	reqCtx := &TrafficInterceptRequestContext{
		Method:      req.Method,
		URL:         req.URL.String(),
		UpstreamURL: req.URL.String(),
		Path:        req.URL.Path,
		ContentType: req.Header.Get("Content-Type"),
		Header:      headerToStringMap(req.Header),
		Headers:     headerToStringMap(req.Header),
		Body:        `{"messages":[]}`,
	}
	resp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	respCtx := &TrafficInterceptResponseContext{
		URL:         req.URL.String(),
		Status:      200,
		ContentType: "application/json",
		Header:      headerToStringMap(resp.Header),
		Headers:     headerToStringMap(resp.Header),
		Body:        `{"content":"old"}`,
	}
	script := `
async function onRequest(context, request) {
  request.headers["x-request-script"] = "1";
  return request;
}

async function onResponse(context, request, response) {
  var body = JSON.parse(response.body);
  body.content = "new";
  response.body = JSON.stringify(body);
  response.status = 201;
  response.headers["x-response-script"] = request.headers["x-request-script"] || "missing";
  return { request, response };
}`
	requestOutput, ran, err := runTrafficInterceptJSHook(script, "onRequest", &model.TrafficInterceptRule{Id: 10}, reqCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("expected onRequest hook to run from response script")
	}
	applyTrafficInterceptScriptRequest(req, reqCtx, requestOutput.Request)
	responseOutput, ran, err := runTrafficInterceptJSHook(script, "onResponse", &model.TrafficInterceptRule{Id: 10}, reqCtx, respCtx)
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("expected onResponse hook to run")
	}
	applyTrafficInterceptScriptRequest(req, reqCtx, responseOutput.Request)
	applyTrafficInterceptScriptResponse(req, resp, respCtx, responseOutput.Response, false)
	if respCtx.Status != 201 {
		t.Fatalf("expected response status to be rewritten, got %d", respCtx.Status)
	}
	if got := trafficInterceptResponseContent(respCtx.Body); got != "new" {
		t.Fatalf("expected response body to be rewritten, got %q in %s", got, respCtx.Body)
	}
	if resp.Header.Get("X-Response-Script") != "1" {
		t.Fatalf("expected response header to see request mutation, got %q", resp.Header.Get("X-Response-Script"))
	}
}

func TestTrafficInterceptResponseToolCallsRewrite(t *testing.T) {
	body := `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"type":"function","function":{"name":"old","arguments":"{}"}}]}}]}`
	rewritten, ok := applyTrafficInterceptResponseRewrites(
		body,
		"",
		`[{"type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]`,
		trafficInterceptEnv(&TrafficInterceptRequestContext{}, &TrafficInterceptResponseContext{Body: body}),
	)
	if !ok {
		t.Fatal("expected response tool_calls rewrite to apply")
	}
	if got := trafficInterceptResponseFunctionNames(rewritten); got != "lookup" {
		t.Fatalf("expected rewritten response tool function name, got %q in %s", got, rewritten)
	}
	if strings.Contains(rewritten, `"name":"old"`) {
		t.Fatalf("expected old tool call to be replaced, got %s", rewritten)
	}
}

func TestTrafficInterceptResponseEventStreamRewrite(t *testing.T) {
	body := `data: {"choices":[{"delta":{"content":"hello ","tool_calls":[{"index":0,"type":"function","function":{"name":"old","arguments":"{\"a\":"}}]}}]}

data: {"choices":[{"delta":{"content":"world","tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}

data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}

data: [DONE]

`
	rewritten, ok := applyTrafficInterceptResponseRewrites(
		body,
		`"replacement"`,
		`[{"index":0,"type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]`,
		trafficInterceptEnv(&TrafficInterceptRequestContext{}, &TrafficInterceptResponseContext{Body: body}),
	)
	if !ok {
		t.Fatal("expected event stream response rewrite to apply")
	}
	if got := trafficInterceptResponseContent(rewritten); got != "replacement" {
		t.Fatalf("expected rewritten stream content, got %q in %s", got, rewritten)
	}
	segments := make([]trafficInterceptResponseTextSegment, 0)
	for _, payload := range trafficInterceptEventStreamPayloads(rewritten) {
		trafficInterceptCollectResponseContentRewriteSegments(payload, &segments)
	}
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		if text, _ := segment.target[segment.key].(string); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) != 1 || parts[0] != "replacement" {
		t.Fatalf("expected rewritten stream content to be emitted as one canonical content chunk, got %#v in %s", parts, rewritten)
	}
	if strings.Count(rewritten, "data: ") < 3 {
		t.Fatalf("expected rewritten stream to keep SSE data framing, got %s", rewritten)
	}
	if !strings.Contains(rewritten, `"usage"`) {
		t.Fatalf("expected stream rewrite to preserve usage chunk, got %s", rewritten)
	}
	if got := trafficInterceptResponseFunctionNames(rewritten); got != "lookup" {
		t.Fatalf("expected rewritten stream tool function name, got %q in %s", got, rewritten)
	}
	if !strings.Contains(rewritten, "data: [DONE]") {
		t.Fatalf("expected stream rewrite to preserve [DONE], got %s", rewritten)
	}
}

func TestTrafficInterceptChatCompletionHelpers(t *testing.T) {
	requestBody := `{"messages":[{"role":"system","content":"rules"},{"role":"user","content":"old"},{"role":"assistant","content":"answer"},{"role":"user","content":[{"type":"text","text":"latest"},{"type":"text","text":"content"}]}]}`
	if got := trafficInterceptLatestContent(requestBody); got != "latest\ncontent" {
		t.Fatalf("expected latest user content, got %q", got)
	}

	responseBody := `data: {"choices":[{"delta":{"content":"hello "}}]}

data: {"choices":[{"delta":{"content":"world","tool_calls":[{"index":0,"type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]}}]}

data: [DONE]

`
	if got := trafficInterceptResponseContent(responseBody); got != "hello world" {
		t.Fatalf("expected response content, got %q", got)
	}
	if got := trafficInterceptResponseFunctionNames(responseBody); got != "lookup" {
		t.Fatalf("expected response function name, got %q", got)
	}
	if got := trafficInterceptResponsePreview(responseBody); !strings.Contains(got, "hello world") || !strings.Contains(got, "lookup") {
		t.Fatalf("expected response preview to include content and tool call, got %q", got)
	}

	prog, err := expr.Compile(`latestContent(request.Body) == "latest\ncontent" && responseFunctionNames(response.Body) contains "lookup"`, expr.Env(trafficInterceptCompileEnv()), expr.AsBool())
	if err != nil {
		t.Fatal(err)
	}
	out, err := expr.Run(prog, trafficInterceptEnv(
		&TrafficInterceptRequestContext{Body: requestBody},
		&TrafficInterceptResponseContext{Body: responseBody},
	))
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := out.(bool); !ok {
		t.Fatal("expected chat completion helper expression to match")
	}
}
