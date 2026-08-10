// Package model holds the SysModel domain entity (LLM/embedding provider config).
package model

// SysModel is the model-provider configuration domain entity.
type SysModel struct {
	Ulid          string `json:"ulid"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	DeletedAt     int64  `json:"deleted_at"`
	CreatedBy     string `json:"created_by"`
	UpdatedBy     string `json:"updated_by"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	BaseUrl       string `json:"base_url"`
	KeyID         string `json:"key_id"`
	ModelType     string `json:"model_type"`
	Category      string `json:"category"`
	Status        string `json:"status"`
	Latency       string `json:"latency"`
	ContextWindow string `json:"context_window"`
	Capabilities  string `json:"capabilities"`
	Usage         int    `json:"usage"`
	Enabled       bool   `json:"enabled"`
	RuntimeMode   string `json:"runtime_mode"`
}

// ModelUsageMetric is a recent invocation aggregate used to enrich model cards.
type ModelUsageMetric struct {
	UserID         string
	ModelID        string
	ModelName      string
	RequestCount   int64
	SuccessCount   int64
	LatencyTotalMs int64
	LatencyCount   int64
}

// SysModelKey is a reusable provider credential owned by one user.
type SysModelKey struct {
	Ulid      string `json:"ulid"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt int64  `json:"deleted_at"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	APIKey    string `json:"api_key"`
	BaseURL   string `json:"base_url"`
	Enabled   bool   `json:"enabled"`
}

// ModelCatalog is a built-in selectable model preset.
type ModelCatalog struct {
	Ulid           string `json:"ulid"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	Provider       string `json:"provider"`
	ModelType      string `json:"model_type"`
	ModelFamily    string `json:"model_family"`
	ModelVersion   string `json:"model_version"`
	DisplayName    string `json:"display_name"`
	DefaultBaseUrl string `json:"default_base_url"`
	ContextWindow  string `json:"context_window"`
	IsFree         bool   `json:"is_free"`
	Installable    bool   `json:"installable"`
	Runtime        string `json:"runtime"`
	DownloadSize   string `json:"download_size"`
	MinMemoryGB    int    `json:"min_memory_gb"`
	Capabilities   string `json:"capabilities"`
	Description    string `json:"description"`
	Enabled        bool   `json:"enabled"`
	Sort           int    `json:"sort"`
}
