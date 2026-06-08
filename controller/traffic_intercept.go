package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func GetInterceptSettings(c *gin.Context) {
	common.ApiSuccess(c, service.GetTrafficInterceptSettings())
}

func UpdateInterceptSettings(c *gin.Context) {
	var settings service.TrafficInterceptSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.UpdateTrafficInterceptSettings(settings); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, service.GetTrafficInterceptSettings())
}

func GetLiveInterceptSettings(c *gin.Context) {
	common.ApiSuccess(c, service.GetTrafficLiveInterceptSettings())
}

func UpdateLiveInterceptSettings(c *gin.Context) {
	var settings service.TrafficLiveInterceptSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		common.ApiError(c, err)
		return
	}
	updated, err := service.UpdateTrafficLiveInterceptSettings(settings)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, updated)
}

func GetLiveInterceptEvents(c *gin.Context) {
	common.ApiSuccess(c, service.ListTrafficLiveInterceptEvents())
}

func DecideLiveInterceptEvent(c *gin.Context) {
	var request service.TrafficLiveInterceptDecision
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.DecideTrafficLiveInterceptEvent(c.Param("id"), request); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetInterceptRules(c *gin.Context) {
	p, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if p < 1 {
		p = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	rules, total, err := model.GetInterceptRules(p, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"items":     rules,
		"total":     total,
		"page":      p,
		"page_size": pageSize,
	})
}

func GetInterceptRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	rule, err := model.GetInterceptRuleById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rule)
}

func CreateInterceptRule(c *gin.Context) {
	var rule model.TrafficInterceptRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		common.ApiError(c, err)
		return
	}

	if rule.Name == "" {
		common.ApiErrorMsg(c, "name is required")
		return
	}

	rule.MatchCount = 0
	normalizeTrafficInterceptCommonMatchFields(&rule)

	if err := rule.Create(); err != nil {
		common.ApiError(c, err)
		return
	}

	service.InvalidateTrafficInterceptRulesCache()
	common.ApiSuccess(c, rule)
}

func UpdateInterceptRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	existing, err := model.GetInterceptRuleById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	wasEnabled := existing.Enabled

	if err := c.ShouldBindJSON(existing); err != nil {
		common.ApiError(c, err)
		return
	}
	normalizeTrafficInterceptCommonMatchFields(existing)
	resetTrafficInterceptRuleMatchCountForEnabledCycle(existing, wasEnabled)

	if err := existing.Update(); err != nil {
		common.ApiError(c, err)
		return
	}

	service.InvalidateTrafficInterceptRulesCache()
	common.ApiSuccess(c, existing)
}

func DeleteInterceptRule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	rule, err := model.GetInterceptRuleById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := rule.Delete(); err != nil {
		common.ApiError(c, err)
		return
	}

	service.InvalidateTrafficInterceptRulesCache()
	common.ApiSuccess(c, nil)
}

func resetTrafficInterceptRuleMatchCountForEnabledCycle(rule *model.TrafficInterceptRule, wasEnabled bool) {
	if rule == nil {
		return
	}
	if !rule.Enabled || (!wasEnabled && rule.Enabled) {
		rule.MatchCount = 0
	}
}

func normalizeTrafficInterceptCommonMatchFields(rule *model.TrafficInterceptRule) {
	if rule == nil {
		return
	}
	if rule.UserId == 0 && rule.ResponseUserId != 0 {
		rule.UserId = rule.ResponseUserId
	}
	if strings.TrimSpace(rule.Username) == "" && strings.TrimSpace(rule.ResponseUsername) != "" {
		rule.Username = rule.ResponseUsername
	}
	if strings.TrimSpace(rule.PathPattern) == "" && strings.TrimSpace(rule.ResponsePathPattern) != "" {
		rule.PathPattern = rule.ResponsePathPattern
	}
	if strings.TrimSpace(rule.Method) == "" && strings.TrimSpace(rule.ResponseMethod) != "" {
		rule.Method = rule.ResponseMethod
	}
	rule.ResponseUserId = 0
	rule.ResponseUsername = ""
	rule.ResponsePathPattern = ""
	rule.ResponseMethod = ""
}
