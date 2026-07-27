package skill

import "github.com/good-fish-man/agent-runtime-client/pkg/query"

// Request DTOs.
type (
	CreateSysSkillReq struct {
		CreatedBy   string `json:"created_by"`
		Name        string `json:"name" validate:"required"`
		Description string `json:"description"`
		SkillType   string `json:"skillType" validate:"required"`
		Version     string `json:"version"`
		Path        string `json:"path"`
		Enabled     bool   `json:"enabled"`
		Config      string `json:"config"`
		IsSystem    bool   `json:"is_system"`
		RiskLevel   string `json:"risk_level"`
	}

	DelSysSkillReq struct {
		Ulid string `validate:"required" uri:"ulid" json:"ulid"`
	}

	UpdateSysSkillReq struct {
		Ulid        string `validate:"required" uri:"ulid" json:"ulid"`
		UpdatedBy   string `json:"updated_by"`
		Name        string `json:"name"`
		Description string `json:"description"`
		SkillType   string `json:"skillType"`
		Version     string `json:"version"`
		Path        string `json:"path"`
		Enabled     *bool  `json:"enabled"`
		Config      string `json:"config"`
		RiskLevel   string `json:"risk_level"`
	}

	FindSysSkillByIdReq struct {
		Ulid string `validate:"required" uri:"ulid" json:"ulid"`
	}

	FindSysSkillAllReq struct {
		SkillType string `json:"skill_type"`
		Name      string `json:"name"`
	}

	FindSysSkillPageReq struct {
		Query    []*query.Query  `json:"query"`
		PageData *query.PageData `json:"page_data"`
		SortData *query.SortData `json:"sort_data"`
	}

	CheckSkillNameReq struct {
		Name string `json:"name" validate:"required"`
	}

	UploadSysSkillReq struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}
)

// Response DTOs.
type (
	CreateSysSkillRsp struct {
		Ulid string `json:"ulid"`
	}

	FindSysSkillRsp struct {
		Ulid        string `json:"ulid"`
		CreatedAt   int64  `json:"created_at"`
		UpdatedAt   int64  `json:"updated_at"`
		CreatedBy   string `json:"created_by"`
		UpdatedBy   string `json:"updated_by"`
		Name        string `json:"name"`
		Description string `json:"description"`
		SkillType   string `json:"skill_type"`
		Version     string `json:"version"`
		Path        string `json:"path"`
		Enabled     bool   `json:"enabled"`
		Config      string `json:"config"`
		IsSystem    bool   `json:"is_system"`
		RiskLevel   string `json:"risk_level"`
	}

	FindSysSkillPageRsp struct {
		Entries  []*FindSysSkillRsp `json:"entries"`
		PageData *query.PageData    `json:"page_data"`
	}

	CheckSkillNameRsp struct {
		Exists  bool   `json:"exists"`
		Message string `json:"message"`
	}
)
