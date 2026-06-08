package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestResetTrafficInterceptRuleMatchCountForEnabledCycle(t *testing.T) {
	rule := &model.TrafficInterceptRule{Enabled: true, MatchCount: 3}
	resetTrafficInterceptRuleMatchCountForEnabledCycle(rule, true)
	if rule.MatchCount != 3 {
		t.Fatalf("expected enabled rule in same cycle to keep match count, got %d", rule.MatchCount)
	}

	rule = &model.TrafficInterceptRule{Enabled: false, MatchCount: 3}
	resetTrafficInterceptRuleMatchCountForEnabledCycle(rule, true)
	if rule.MatchCount != 0 {
		t.Fatalf("expected disabled rule to reset match count, got %d", rule.MatchCount)
	}

	rule = &model.TrafficInterceptRule{Enabled: true, MatchCount: 3}
	resetTrafficInterceptRuleMatchCountForEnabledCycle(rule, false)
	if rule.MatchCount != 0 {
		t.Fatalf("expected newly enabled rule to start a new match count cycle, got %d", rule.MatchCount)
	}
}

func TestNormalizeTrafficInterceptCommonMatchFields(t *testing.T) {
	rule := &model.TrafficInterceptRule{
		ResponseUserId:      42,
		ResponseUsername:    "alice",
		ResponsePathPattern: "^/v1/chat/completions$",
		ResponseMethod:      "POST",
	}

	normalizeTrafficInterceptCommonMatchFields(rule)

	if rule.UserId != 42 {
		t.Fatalf("expected response user id to move to shared user id, got %d", rule.UserId)
	}
	if rule.Username != "alice" {
		t.Fatalf("expected response username to move to shared username, got %q", rule.Username)
	}
	if rule.PathPattern != "^/v1/chat/completions$" {
		t.Fatalf("expected response path pattern to move to shared path pattern, got %q", rule.PathPattern)
	}
	if rule.Method != "POST" {
		t.Fatalf("expected response method to move to shared method, got %q", rule.Method)
	}
	if rule.ResponseUserId != 0 || rule.ResponseUsername != "" || rule.ResponsePathPattern != "" || rule.ResponseMethod != "" {
		t.Fatal("expected deprecated response basic fields to be cleared")
	}
}
