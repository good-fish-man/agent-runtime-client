package knowledge_base

import "github.com/good-fish-man/agent-runtime-client/pkg/query"

// Request DTOs.
type (
	CreateSysKnowledgeBaseReq struct {
		CreatedBy    string `json:"created_by"`
		Name         string `json:"name" validate:"required"`
		Description  string `json:"description"`
		RetrievalUrl string `json:"retrievalUrl" validate:"required"`
		Token        string `json:"token"`
		Enabled      bool   `json:"enabled"`
	}

	DelSysKnowledgeBaseReq struct {
		Ulid string `validate:"required" uri:"ulid" json:"ulid"`
	}

	UpdateSysKnowledgeBaseReq struct {
		Ulid         string `validate:"required" uri:"ulid" json:"ulid"`
		UpdatedBy    string `json:"updated_by"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		RetrievalUrl string `json:"retrievalUrl"`
		Token        string `json:"token"`
		Enabled      *bool  `json:"enabled"`
	}

	FindSysKnowledgeBaseByIdReq struct {
		Ulid string `validate:"required" uri:"ulid" json:"ulid"`
	}

	FindSysKnowledgeBaseAllReq struct{}

	FindSysKnowledgeBasePageReq struct {
		Query    []*query.Query  `json:"query"`
		PageData *query.PageData `json:"page_data"`
		SortData *query.SortData `json:"sort_data"`
	}

	RecallTestReq struct {
		Query string `json:"query" validate:"required"`
		TopK  int    `json:"top_k"`
	}
)

// Response DTOs.
type (
	CreateSysKnowledgeBaseRsp struct {
		Ulid string `json:"ulid"`
	}

	FindSysKnowledgeBaseRsp struct {
		Ulid         string `json:"ulid"`
		CreatedAt    int64  `json:"created_at"`
		UpdatedAt    int64  `json:"updated_at"`
		CreatedBy    string `json:"created_by"`
		UpdatedBy    string `json:"updated_by"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		RetrievalUrl string `json:"retrievalUrl"`
		Token        string `json:"token"`
		Enabled      bool   `json:"enabled"`
	}

	FindSysKnowledgeBasePageRsp struct {
		Entries  []*FindSysKnowledgeBaseRsp `json:"entries"`
		PageData *query.PageData            `json:"page_data"`
	}

	RecallResult struct {
		Title   string  `json:"title"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	}
)
