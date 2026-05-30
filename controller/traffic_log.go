package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func GetTrafficLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	userId, _ := strconv.Atoi(c.Query("user_id"))
	channelId, _ := strconv.Atoi(c.Query("channel"))
	statusCode, _ := strconv.Atoi(c.Query("status_code"))

	logs, total, err := model.GetTrafficLogs(model.TrafficLogQuery{
		StartTimestamp:    startTimestamp,
		EndTimestamp:      endTimestamp,
		UserId:            userId,
		Username:          c.Query("username"),
		TokenName:         c.Query("token_name"),
		ModelName:         c.Query("model_name"),
		ChannelId:         channelId,
		Group:             c.Query("group"),
		StatusCode:        statusCode,
		RequestId:         c.Query("request_id"),
		UpstreamRequestId: c.Query("upstream_request_id"),
		StartIdx:          pageInfo.GetStartIdx(),
		Num:               pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func GetTrafficLog(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	log, err := model.GetTrafficLogById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, log)
}

func ReplayTrafficLog(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	log, err := model.GetTrafficLogById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var request service.TrafficReplayRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}

	response, err := service.ReplayTrafficLog(log, request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}
