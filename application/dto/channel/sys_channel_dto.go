package channel

import "github.com/good-fish-man/agent-runtime-client/pkg/query"

// Request DTOs.
type (
	CreateSysChannelReq struct {
		CreatedBy   string         `json:"created_by"`
		Name        string         `json:"name" validate:"required"`
		Code        string         `json:"code" validate:"required"`
		Description string         `json:"description"`
		Icon        string         `json:"icon"`
		Enabled     bool           `json:"enabled"`
		Sort        int            `json:"sort"`
		Config      map[string]any `json:"config"`
	}

	DelSysChannelReq struct {
		Ulid string `validate:"required" uri:"ulid" json:"ulid"`
	}

	UpdateSysChannelReq struct {
		Ulid        string         `validate:"required" uri:"ulid" json:"ulid"`
		UpdatedBy   string         `json:"updated_by"`
		Name        string         `json:"name"`
		Code        string         `json:"code"`
		Description string         `json:"description"`
		Icon        string         `json:"icon"`
		Enabled     *bool          `json:"enabled"`
		Sort        *int           `json:"sort"`
		Config      map[string]any `json:"config"`
	}

	FindSysChannelByIdReq struct {
		Ulid string `validate:"required" uri:"ulid" json:"ulid"`
	}

	FindSysChannelAllReq struct {
		Name string `json:"name"`
	}

	FindSysChannelPageReq struct {
		Query    []*query.Query  `json:"query"`
		PageData *query.PageData `json:"page_data"`
		SortData *query.SortData `json:"sort_data"`
	}
)

// Response DTOs.
type (
	CreateSysChannelRsp struct {
		Ulid string `json:"ulid"`
	}

	FindSysChannelRsp struct {
		Ulid        string         `json:"ulid"`
		CreatedAt   int64          `json:"created_at"`
		UpdatedAt   int64          `json:"updated_at"`
		CreatedBy   string         `json:"created_by"`
		UpdatedBy   string         `json:"updated_by"`
		Name        string         `json:"name"`
		Code        string         `json:"code"`
		Description string         `json:"description"`
		Icon        string         `json:"icon"`
		Enabled     bool           `json:"enabled"`
		Sort        int            `json:"sort"`
		Config      map[string]any `json:"config"`
	}

	FindSysChannelPageRsp struct {
		Entries  []*FindSysChannelRsp `json:"entries"`
		PageData *query.PageData      `json:"page_data"`
	}
)
