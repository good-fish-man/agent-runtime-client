package job

import "github.com/good-fish-man/agent-runtime-client/pkg/query"

// Request DTOs.
type (
	FindJobExecutionByIdReq struct {
		Ulid string `validate:"required" uri:"ulid" json:"ulid"`
	}

	FindJobExecutionByAgentIdReq struct {
		AgentId string `form:"agent_id" validate:"required" json:"agent_id"`
		Limit   int    `form:"limit" json:"limit"`
	}

	FindJobExecutionPageReq struct {
		Query    []*query.Query  `json:"query"`
		PageData *query.PageData `json:"page_data"`
		SortData *query.SortData `json:"sort_data"`
	}
)

// Response DTOs.
type (
	FindJobExecutionRsp struct {
		Ulid          string `json:"ulid"`
		CreatedAt     int64  `json:"created_at"`
		UpdatedAt     int64  `json:"updated_at"`
		AgentId       string `json:"agent_id"`
		AgentName     string `json:"agent_name"`
		SessionId     string `json:"session_id"`
		Status        string `json:"status"`
		TriggerTime   int64  `json:"trigger_time"`
		StartedAt     int64  `json:"started_at"`
		FinishedAt    int64  `json:"finished_at"`
		InputSummary  string `json:"input_summary"`
		OutputSummary string `json:"output_summary"`
		OutputFull    string `json:"output_full"`
		ErrorMsg      string `json:"error_msg"`
		TokensUsed    int    `json:"tokens_used"`
		LatencyMs     int64  `json:"latency_ms"`
	}

	FindJobExecutionPageRsp struct {
		Entries  []*FindJobExecutionRsp `json:"entries"`
		PageData *query.PageData        `json:"page_data"`
	}

	FindJobExecutionListRsp struct {
		Entries []*FindJobExecutionRsp `json:"entries"`
	}
)
