package job

// JobExecution is the job execution log domain entity.
type JobExecution struct {
	Ulid          string `json:"ulid"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	DeletedAt     int64  `json:"deleted_at"`
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

const (
	JobStatusRunning = "running"
	JobStatusSuccess = "success"
	JobStatusFailed  = "failed"
)
