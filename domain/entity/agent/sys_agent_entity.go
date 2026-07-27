package agent

// SysAgent is the agent configuration domain entity.
type SysAgent struct {
	Ulid           string `json:"ulid"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	DeletedAt      int64  `json:"deleted_at"`
	CreatedBy      string `json:"created_by"`
	UpdatedBy      string `json:"updated_by"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Icon           string `json:"icon"`
	Model          string `json:"model"`
	EmbeddingModel string `json:"embedding_model"`
	ImageModel     string `json:"image_model"`
	Config         string `json:"config"`
	ConfigJson     string `json:"config_json"`
	IsSystem       bool   `json:"is_system"`
	Enabled        bool   `json:"enabled"`
	Channels       string `json:"channels"`
	IsPeriodic     bool   `json:"is_periodic"`
	CronRule       string `json:"cron_rule"`
}

// SysAgentUserModel stores one user's model overrides for a public system agent.
type SysAgentUserModel struct {
	Ulid           string `json:"ulid"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	UserID         string `json:"user_id"`
	AgentID        string `json:"agent_id"`
	Model          string `json:"model"`
	EmbeddingModel string `json:"embedding_model"`
	ImageModel     string `json:"image_model"`
}
