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
