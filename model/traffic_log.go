package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"gorm.io/gorm"
)

type TrafficLog struct {
	Id                         int    `json:"id" gorm:"index:idx_traffic_created_at_id,priority:1;index:idx_traffic_user_id_id,priority:2"`
	CreatedAt                  int64  `json:"created_at" gorm:"bigint;index:idx_traffic_created_at_id,priority:2"`
	UserId                     int    `json:"user_id" gorm:"index;index:idx_traffic_user_id_id,priority:1"`
	Username                   string `json:"username" gorm:"index;default:''"`
	TokenId                    int    `json:"token_id" gorm:"default:0;index"`
	TokenName                  string `json:"token_name" gorm:"index;default:''"`
	ModelName                  string `json:"model_name" gorm:"index;default:''"`
	ChannelId                  int    `json:"channel" gorm:"index"`
	ChannelName                string `json:"channel_name" gorm:"-"`
	Group                      string `json:"group" gorm:"index"`
	Ip                         string `json:"ip" gorm:"index;default:''"`
	RequestId                  string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_traffic_request_id;default:''"`
	UpstreamRequestId          string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_traffic_upstream_request_id;default:''"`
	Method                     string `json:"method" gorm:"type:varchar(16);index;default:''"`
	Path                       string `json:"path" gorm:"type:text"`
	RequestURL                 string `json:"request_url" gorm:"type:text"`
	StatusCode                 int    `json:"status_code" gorm:"index;default:0"`
	IsStream                   bool   `json:"is_stream"`
	RequestContentType         string `json:"request_content_type" gorm:"type:varchar(255);default:''"`
	ResponseContentType        string `json:"response_content_type" gorm:"type:varchar(255);default:''"`
	RequestHeaders             string `json:"request_headers" gorm:"type:text"`
	ResponseHeaders            string `json:"response_headers" gorm:"type:text"`
	RequestBody                string `json:"request_body" gorm:"type:text"`
	ResponseBody               string `json:"response_body" gorm:"type:text"`
	RequestBodySize            int64  `json:"request_body_size" gorm:"bigint;default:0"`
	ResponseBodySize           int64  `json:"response_body_size" gorm:"bigint;default:0"`
	RequestBodyTruncated       bool   `json:"request_body_truncated"`
	ResponseBodyTruncated      bool   `json:"response_body_truncated"`
	RequestBodyTruncatedBytes  int64  `json:"request_body_truncated_bytes" gorm:"bigint;default:0"`
	ResponseBodyTruncatedBytes int64  `json:"response_body_truncated_bytes" gorm:"bigint;default:0"`
	DurationMs                 int64  `json:"duration_ms" gorm:"bigint;default:0"`
	UserAgent                  string `json:"user_agent" gorm:"type:varchar(512);default:''"`
}

type TrafficLogQuery struct {
	StartTimestamp    int64
	EndTimestamp      int64
	UserId            int
	Username          string
	TokenName         string
	ModelName         string
	ChannelId         int
	Group             string
	StatusCode        int
	RequestId         string
	UpstreamRequestId string
	StartIdx          int
	Num               int
}

func RecordTrafficLog(log *TrafficLog) error {
	if log == nil {
		return nil
	}
	return LOG_DB.Create(log).Error
}

func applyTrafficLogFilters(tx *gorm.DB, query TrafficLogQuery) (*gorm.DB, error) {
	var err error
	if query.UserId != 0 {
		tx = tx.Where("traffic_logs.user_id = ?", query.UserId)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "traffic_logs.username", query.Username); err != nil {
		return nil, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "traffic_logs.model_name", query.ModelName); err != nil {
		return nil, err
	}
	if query.TokenName != "" {
		tx = tx.Where("traffic_logs.token_name = ?", query.TokenName)
	}
	if query.ChannelId != 0 {
		tx = tx.Where("traffic_logs.channel_id = ?", query.ChannelId)
	}
	if query.Group != "" {
		tx = tx.Where("traffic_logs."+logGroupCol+" = ?", query.Group)
	}
	if query.StatusCode != 0 {
		tx = tx.Where("traffic_logs.status_code = ?", query.StatusCode)
	}
	if query.RequestId != "" {
		tx = tx.Where("traffic_logs.request_id = ?", query.RequestId)
	}
	if query.UpstreamRequestId != "" {
		tx = tx.Where("traffic_logs.upstream_request_id = ?", query.UpstreamRequestId)
	}
	if query.StartTimestamp != 0 {
		tx = tx.Where("traffic_logs.created_at >= ?", query.StartTimestamp)
	}
	if query.EndTimestamp != 0 {
		tx = tx.Where("traffic_logs.created_at <= ?", query.EndTimestamp)
	}
	return tx, nil
}

func fillTrafficLogChannelNames(logs []*TrafficLog) error {
	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}
	if channelIds.Len() == 0 {
		return nil
	}

	var channels []struct {
		Id   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if common.MemoryCacheEnabled {
		for _, channelId := range channelIds.Items() {
			if cacheChannel, err := CacheGetChannel(channelId); err == nil {
				channels = append(channels, struct {
					Id   int    `gorm:"column:id"`
					Name string `gorm:"column:name"`
				}{
					Id:   channelId,
					Name: cacheChannel.Name,
				})
			}
		}
	} else if err := DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
		return err
	}

	channelMap := make(map[int]string, len(channels))
	for _, channel := range channels {
		channelMap[channel.Id] = channel.Name
	}
	for i := range logs {
		logs[i].ChannelName = channelMap[logs[i].ChannelId]
	}
	return nil
}

func GetTrafficLogs(query TrafficLogQuery) (logs []*TrafficLog, total int64, err error) {
	tx, err := applyTrafficLogFilters(LOG_DB.Model(&TrafficLog{}), query)
	if err != nil {
		return nil, 0, err
	}

	if err = tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err = tx.
		Omit("request_headers", "response_headers", "request_body", "response_body").
		Order("traffic_logs.id desc").
		Limit(query.Num).
		Offset(query.StartIdx).
		Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	if err = fillTrafficLogChannelNames(logs); err != nil {
		return logs, total, err
	}
	return logs, total, nil
}

func GetTrafficLogById(id int) (*TrafficLog, error) {
	if id <= 0 {
		return nil, errors.New("invalid traffic log id")
	}
	var log TrafficLog
	if err := LOG_DB.First(&log, id).Error; err != nil {
		return nil, err
	}
	logs := []*TrafficLog{&log}
	if err := fillTrafficLogChannelNames(logs); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return &log, err
	}
	fillTrafficLogHeaderFallbacks(&log)
	return &log, nil
}

func fillTrafficLogHeaderFallbacks(log *TrafficLog) {
	if log == nil {
		return
	}
	if strings.TrimSpace(log.RequestHeaders) == "" {
		log.RequestHeaders = trafficLogContentTypeHeader(log.RequestContentType)
	}
	if strings.TrimSpace(log.ResponseHeaders) == "" {
		log.ResponseHeaders = trafficLogContentTypeHeader(log.ResponseContentType)
	}
}

func trafficLogContentTypeHeader(contentType string) string {
	if strings.TrimSpace(contentType) == "" {
		return ""
	}
	data, err := common.Marshal(map[string]string{"Content-Type": contentType})
	if err != nil {
		common.SysLog("failed to marshal traffic log content type header: " + err.Error())
		return ""
	}
	return string(data)
}
