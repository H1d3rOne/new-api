package model

import (
	"errors"

	"gorm.io/gorm"
)

type HeaderOp struct {
	Op    string `json:"op"` // "set" or "remove"
	Key   string `json:"key"`
	Value string `json:"value"` // expr expression or static string
}

type MessageContentRewrite struct {
	Role    string `json:"role"`
	Mode    string `json:"mode"` // "latest", "first", "all", or "index"
	Index   int    `json:"index"`
	Content string `json:"content"` // expr expression or static string
}

type MessageContentMatch struct {
	Role      string `json:"role"`
	Mode      string `json:"mode"` // "latest", "first", "all", or "index"
	Index     int    `json:"index"`
	Content   string `json:"content"`
	ContentOp string `json:"content_op"` // "and" or "or" between keyword lines
}

type TrafficInterceptRule struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	Name        string `json:"name" gorm:"type:varchar(128);not null;default:''"`
	Description string `json:"description" gorm:"type:text"`
	Priority    int    `json:"priority" gorm:"default:0;index"`
	Enabled     bool   `json:"enabled" gorm:"default:true;index"`
	MatchLimit  int    `json:"match_limit" gorm:"default:0"`
	MatchCount  int    `json:"match_count" gorm:"default:0"`
	UserId      int    `json:"user_id" gorm:"default:0;index"`
	Username    string `json:"username" gorm:"index;default:''"`

	// Basic matching conditions shared by request, response, and script actions.
	PathPattern string `json:"path_pattern" gorm:"type:varchar(512);default:''"`
	Method      string `json:"method" gorm:"type:varchar(16);default:''"`

	// Request matching conditions
	ModelPattern          string `json:"model_pattern" gorm:"type:varchar(256);default:''"`
	RequestContentMatch   string `json:"request_content_match" gorm:"type:text"`
	RequestMessageMatches string `json:"request_message_matches" gorm:"type:text"` // JSON array of MessageContentMatch
	RequestMessageMatchOp string `json:"request_message_match_op" gorm:"type:varchar(8);default:'and'"`
	ConditionExpr         string `json:"condition_expr" gorm:"type:text"` // expr-lang expression

	// Response matching conditions
	// Deprecated: response user/path/method fields are normalized into the shared basic fields.
	ResponseUserId         int    `json:"response_user_id" gorm:"default:0;index"`
	ResponseUsername       string `json:"response_username" gorm:"index;default:''"`
	ResponsePathPattern    string `json:"response_path_pattern" gorm:"type:varchar(512);default:''"`
	ResponseMethod         string `json:"response_method" gorm:"type:varchar(16);default:''"`
	ResponseModelPattern   string `json:"response_model_pattern" gorm:"type:varchar(256);default:''"`
	ResponseContentMatch   string `json:"response_content_match" gorm:"type:text"`
	ResponseToolCallsMatch string `json:"response_tool_calls_match" gorm:"type:text"`
	ResponseMatchOp        string `json:"response_match_op" gorm:"type:varchar(8);default:'and'"`
	ResponseConditionExpr  string `json:"response_condition_expr" gorm:"type:text"` // expr-lang expression

	// Intercept phases
	RequestMatchEnabled  bool `json:"request_match_enabled" gorm:"default:false"`
	ResponseMatchEnabled bool `json:"response_match_enabled" gorm:"default:false"`
	InterceptRequest     bool `json:"intercept_request" gorm:"default:false"`
	InterceptResponse    bool `json:"intercept_response" gorm:"default:false"`
	ScriptEnabled        bool `json:"script_enabled" gorm:"default:false"`

	// Request actions
	BlockEnabled           bool   `json:"block_enabled" gorm:"default:false"`
	BlockStatusCode        int    `json:"block_status_code" gorm:"default:403"`
	BlockContentType       string `json:"block_content_type" gorm:"type:varchar(128);default:'application/json'"`
	BlockBody              string `json:"block_body" gorm:"type:text"`
	RequestHeaderOps       string `json:"request_header_ops" gorm:"type:text"` // JSON array of HeaderOp
	RequestBodyRewrite     string `json:"request_body_rewrite" gorm:"type:text"`
	RequestMessageRewrites string `json:"request_message_rewrites" gorm:"type:text"` // JSON array of MessageContentRewrite
	RequestURLRewrite      string `json:"request_url_rewrite" gorm:"type:text"`
	RequestScript          string `json:"request_script" gorm:"type:text"`

	// Response actions
	ResponseHeaderOps        string `json:"response_header_ops" gorm:"type:text"` // JSON array of HeaderOp
	ResponseBodyRewrite      string `json:"response_body_rewrite" gorm:"type:text"`
	ResponseContentRewrite   string `json:"response_content_rewrite" gorm:"type:text"`
	ResponseToolCallsRewrite string `json:"response_tool_calls_rewrite" gorm:"type:text"`
	ResponseStatusRewrite    string `json:"response_status_rewrite" gorm:"type:varchar(64);default:''"`
	ResponseURLRewrite       string `json:"response_url_rewrite" gorm:"type:text"`
	ResponseScript           string `json:"response_script" gorm:"type:text"`

	// Script action
	Script string `json:"script" gorm:"type:text"`

	CreatedAt int64 `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt int64 `json:"updated_at" gorm:"autoUpdateTime"`
}

func GetInterceptRules(page, pageSize int) ([]*TrafficInterceptRule, int64, error) {
	var rules []*TrafficInterceptRule
	var total int64

	tx := DB.Model(&TrafficInterceptRule{})
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := tx.Order("priority DESC, id ASC").Offset(offset).Limit(pageSize).Find(&rules).Error; err != nil {
		return nil, 0, err
	}

	return rules, total, nil
}

func GetInterceptRuleById(id int) (*TrafficInterceptRule, error) {
	if id <= 0 {
		return nil, errors.New("invalid intercept rule id")
	}
	var rule TrafficInterceptRule
	if err := DB.First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func GetEnabledInterceptRules() ([]*TrafficInterceptRule, error) {
	var rules []*TrafficInterceptRule
	if err := DB.Where("enabled = ?", true).Order("priority DESC, id ASC").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *TrafficInterceptRule) Create() error {
	return DB.Create(r).Error
}

func (r *TrafficInterceptRule) Update() error {
	return DB.Save(r).Error
}

func (r *TrafficInterceptRule) Delete() error {
	return DB.Delete(r).Error
}

func ConsumeInterceptRuleMatch(id int) (bool, error) {
	if id <= 0 {
		return false, errors.New("invalid intercept rule id")
	}
	tx := DB.Model(&TrafficInterceptRule{}).
		Where("id = ? AND (match_limit <= ? OR match_count < match_limit)", id, 0).
		UpdateColumn("match_count", gorm.Expr("match_count + ?", 1))
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}
