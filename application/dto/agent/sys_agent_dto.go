package agent

import "github.com/good-fish-man/agent-runtime-client/pkg/query"

// Request DTOs.
type (
	CreateSysAgentReq struct {
		CreatedBy      string `json:"created_by"`
		Name           string `json:"name" validate:"required"`
		Description    string `json:"description"`
		Icon           string `json:"icon"`
		Model          string `json:"model"`
		EmbeddingModel string `json:"embedding_model"`
		ImageModel     string `json:"image_model"`
		VideoModel     string `json:"video_model"`
		Config         string `json:"config"`
		ConfigJson     string `json:"config_json"`
		Enabled        bool   `json:"enabled"`
		IsSystem       bool   `json:"is_system"`
		Channels       string `json:"channels"`
		IsPeriodic     bool   `json:"is_periodic"`
		CronRule       string `json:"cron_rule"`
	}

	DelSysAgentReq struct {
		Ulid   string `validate:"required" uri:"ulid" json:"ulid"`
		UserID string `json:"-"`
	}

	UpdateSysAgentReq struct {
		Ulid           string `validate:"required" uri:"ulid" json:"ulid"`
		UpdatedBy      string `json:"updated_by"`
		Name           string `json:"name"`
		Description    string `json:"description"`
		Icon           string `json:"icon"`
		Model          string `json:"model"`
		EmbeddingModel string `json:"embedding_model"`
		ImageModel     string `json:"image_model"`
		VideoModel     string `json:"video_model"`
		Config         string `json:"config"`
		ConfigJson     string `json:"config_json"`
		Enabled        *bool  `json:"enabled"`
		Channels       string `json:"channels"`
		IsPeriodic     *bool  `json:"is_periodic"`
		CronRule       string `json:"cron_rule"`
		UserID         string `json:"-"`
	}

	UpdateSysAgentEnabledReq struct {
		Ulid    string `validate:"required" uri:"ulid" json:"ulid"`
		Enabled bool   `json:"enabled"`
		UserID  string `json:"-"`
	}

	FindSysAgentByIdReq struct {
		Ulid   string `validate:"required" uri:"ulid" json:"ulid"`
		UserID string `json:"-"`
	}

	FindSysAgentAllReq struct {
		Name   string `json:"name"`
		UserID string `json:"-"`
	}

	FindSysAgentPageReq struct {
		Query    []*query.Query  `json:"query"`
		PageData *query.PageData `json:"page_data"`
		SortData *query.SortData `json:"sort_data"`
		UserID   string          `json:"-"`
	}

	UploadSysAgentReq struct {
		CreatedBy      string `json:"created_by"`
		Name           string `json:"name" validate:"required"`
		Description    string `json:"description"`
		Icon           string `json:"icon"`
		Model          string `json:"model"`
		EmbeddingModel string `json:"embedding_model"`
		ImageModel     string `json:"image_model"`
		VideoModel     string `json:"video_model"`
		Config         string `json:"config"`
		ConfigJson     string `json:"config_json"`
		Enabled        bool   `json:"enabled"`
		Channels       string `json:"channels"`
		IsPeriodic     bool   `json:"is_periodic"`
		CronRule       string `json:"cron_rule"`
	}
)

// Response DTOs.
type (
	CreateSysAgentRsp struct {
		Ulid string `json:"ulid"`
	}

	FindSysAgentRsp struct {
		Ulid           string `json:"ulid"`
		CreatedAt      int64  `json:"created_at"`
		UpdatedAt      int64  `json:"updated_at"`
		CreatedBy      string `json:"created_by"`
		UpdatedBy      string `json:"updated_by"`
		Name           string `json:"name"`
		Description    string `json:"description"`
		Icon           string `json:"icon"`
		Model          string `json:"model"`
		EmbeddingModel string `json:"embedding_model"`
		ImageModel     string `json:"image_model"`
		VideoModel     string `json:"video_model"`
		Config         string `json:"config,omitempty"`
		ConfigJson     string `json:"config_json,omitempty"`
		IsSystem       bool   `json:"is_system"`
		Enabled        bool   `json:"enabled"`
		Channels       string `json:"channels"`
		IsPeriodic     bool   `json:"is_periodic"`
		CronRule       string `json:"cron_rule"`
	}

	FindSysAgentPageRsp struct {
		Entries  []*FindSysAgentRsp `json:"entries"`
		PageData *query.PageData    `json:"page_data"`
	}
)
