package model

import "github.com/good-fish-man/agent-runtime-client/pkg/query"

// Request DTOs.
type (
	CreateSysModelReq struct {
		CreatedBy     string `json:"created_by"`
		Name          string `json:"name" validate:"required"`
		Provider      string `json:"provider" validate:"required"`
		BaseUrl       string `json:"baseUrl"`
		KeyID         string `json:"keyId"`
		ModelType     string `json:"modelType" validate:"required"`
		Category      string `json:"category"`
		ContextWindow string `json:"contextWindow"`
		Capabilities  string `json:"capabilities"`
	}

	DelSysModelReq struct {
		Ulid   string `validate:"required" uri:"ulid" json:"ulid"`
		UserID string `json:"-"`
	}

	UpdateSysModelReq struct {
		Ulid          string  `validate:"required" uri:"ulid" json:"ulid"`
		UpdatedBy     string  `json:"updated_by"`
		Name          string  `json:"name"`
		Provider      string  `json:"provider"`
		BaseUrl       string  `json:"baseUrl"`
		KeyID         *string `json:"keyId"`
		ModelType     string  `json:"modelType"`
		Category      string  `json:"category"`
		Status        string  `json:"status"`
		Latency       string  `json:"latency"`
		ContextWindow string  `json:"contextWindow"`
		Capabilities  string  `json:"capabilities"`
		UserID        string  `json:"-"`
	}

	UpdateSysModelEnabledReq struct {
		Ulid      string `validate:"required" uri:"ulid" json:"-"`
		Enabled   *bool  `validate:"required" json:"enabled"`
		UpdatedBy string `json:"-"`
	}

	UpdateSysModelRuntimeModeReq struct {
		Ulid        string `validate:"required" uri:"ulid" json:"-"`
		RuntimeMode string `validate:"required,oneof=always_on on_demand off" json:"runtimeMode"`
		UpdatedBy   string `json:"-"`
	}

	FindSysModelByIdReq struct {
		Ulid   string `validate:"required" uri:"ulid" json:"ulid"`
		UserID string `json:"-"`
	}

	FindSysModelAllReq struct {
		ModelType string `json:"modelType"`
		UserID    string `json:"-"`
	}

	FindModelCatalogReq struct {
		ModelType string `form:"modelType" json:"modelType"`
		Provider  string `form:"provider" json:"provider"`
	}

	FindSysModelPageReq struct {
		Query    []*query.Query  `json:"query"`
		PageData *query.PageData `json:"page_data"`
		SortData *query.SortData `json:"sort_data"`
		UserID   string          `json:"-"`
	}
)

// Response DTOs.
type (
	CreateSysModelRsp struct {
		Ulid string `json:"ulid"`
	}

	FindSysModelRsp struct {
		Ulid          string  `json:"ulid"`
		CreatedAt     int64   `json:"created_at"`
		UpdatedAt     int64   `json:"updated_at"`
		CreatedBy     string  `json:"created_by"`
		UpdatedBy     string  `json:"updated_by"`
		Name          string  `json:"name"`
		Provider      string  `json:"provider"`
		BaseUrl       string  `json:"baseUrl"`
		ModelType     string  `json:"modelType"`
		Category      string  `json:"category"`
		Status        string  `json:"status"`
		Latency       string  `json:"latency"`
		ContextWindow string  `json:"contextWindow"`
		Capabilities  string  `json:"capabilities"`
		Usage         int     `json:"usage"`
		UsageRate     float64 `json:"usageRate"`
		UsageCount    int64   `json:"usageCount"`
		SuccessRate   float64 `json:"successRate"`
		InputTokens   int64   `json:"inputTokens"`
		OutputTokens  int64   `json:"outputTokens"`
		TotalTokens   int64   `json:"totalTokens"`
		Enabled       bool    `json:"enabled"`
		RuntimeMode   string  `json:"runtimeMode"`
		KeyID         string  `json:"keyId"`
		KeyName       string  `json:"keyName"`
	}

	CreateModelKeyReq struct {
		UserID   string `json:"-"`
		Name     string `json:"name" validate:"required"`
		Provider string `json:"provider" validate:"required"`
		APIKey   string `json:"apiKey"`
		BaseURL  string `json:"baseUrl"`
	}

	UpdateModelKeyReq struct {
		Ulid     string `uri:"ulid" json:"-" validate:"required"`
		UserID   string `json:"-"`
		Name     string `json:"name"`
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
		BaseURL  string `json:"baseUrl"`
		Enabled  *bool  `json:"enabled"`
	}

	ModelKeyRsp struct {
		Ulid       string `json:"ulid"`
		CreatedAt  int64  `json:"created_at"`
		UpdatedAt  int64  `json:"updated_at"`
		Name       string `json:"name"`
		Provider   string `json:"provider"`
		BaseURL    string `json:"baseUrl"`
		KeyMask    string `json:"keyMask"`
		HasKey     bool   `json:"hasKey"`
		Enabled    bool   `json:"enabled"`
		ModelCount int64  `json:"modelCount"`
	}

	FindSysModelPageRsp struct {
		Entries  []*FindSysModelRsp `json:"entries"`
		PageData *query.PageData    `json:"page_data"`
	}

	FindModelCatalogRsp struct {
		Ulid           string `json:"ulid"`
		CreatedAt      int64  `json:"created_at"`
		UpdatedAt      int64  `json:"updated_at"`
		Provider       string `json:"provider"`
		ModelType      string `json:"modelType"`
		ModelFamily    string `json:"modelFamily"`
		ModelVersion   string `json:"modelVersion"`
		DisplayName    string `json:"displayName"`
		DefaultBaseUrl string `json:"defaultBaseUrl"`
		ContextWindow  string `json:"contextWindow"`
		IsFree         bool   `json:"isFree"`
		Installable    bool   `json:"installable"`
		Runtime        string `json:"runtime"`
		DownloadSize   string `json:"downloadSize"`
		MinMemoryGB    int    `json:"minMemoryGB"`
		Capabilities   string `json:"capabilities"`
		Description    string `json:"description"`
		Enabled        bool   `json:"enabled"`
		Sort           int    `json:"sort"`
	}

	LocalModelEnvironmentRsp struct {
		OS                      string `json:"os"`
		Arch                    string `json:"arch"`
		MemoryGB                int    `json:"memoryGB"`
		MemoryTotalBytes        uint64 `json:"memoryTotalBytes"`
		MemoryAvailableBytes    uint64 `json:"memoryAvailableBytes"`
		StorageTotalBytes       uint64 `json:"storageTotalBytes"`
		StorageAvailableBytes   uint64 `json:"storageAvailableBytes"`
		Runtime                 string `json:"runtime"`
		RuntimeInstalled        bool   `json:"runtimeInstalled"`
		RuntimeRunning          bool   `json:"runtimeRunning"`
		RuntimeVersion          string `json:"runtimeVersion"`
		RuntimeInstallSupported bool   `json:"runtimeInstallSupported"`
		ModelInstalled          bool   `json:"modelInstalled"`
		Compatible              bool   `json:"compatible"`
		Message                 string `json:"message"`
	}

	LocalModelInstallRsp struct {
		JobID string `json:"jobId"`
	}

	LocalModelInstallJobRsp struct {
		JobID        string `json:"jobId"`
		CatalogID    string `json:"catalogId"`
		ModelVersion string `json:"modelVersion"`
		Status       string `json:"status"`
		Stage        string `json:"stage"`
		Progress     int    `json:"progress"`
		Message      string `json:"message"`
		Error        string `json:"error,omitempty"`
	}

	ModelTrainingEnvironmentRsp struct {
		OS                   string `json:"os"`
		Arch                 string `json:"arch"`
		Backend              string `json:"backend"`
		Supported            bool   `json:"supported"`
		PythonInstalled      bool   `json:"pythonInstalled"`
		DependenciesReady    bool   `json:"dependenciesReady"`
		AcceleratorAvailable bool   `json:"acceleratorAvailable"`
		Message              string `json:"message"`
	}

	CreateModelTrainingReq struct {
		Mode                    string  `form:"mode" validate:"required,oneof=fine_tune distill"`
		Name                    string  `form:"name" validate:"required"`
		StudentModelID          string  `form:"studentModelId" validate:"required"`
		TeacherModelID          string  `form:"teacherModelId"`
		OutputName              string  `form:"outputName" validate:"required"`
		Iterations              int     `form:"iterations"`
		BatchSize               int     `form:"batchSize"`
		LearningRate            float64 `form:"learningRate"`
		LoraRank                int     `form:"loraRank"`
		MaxSamples              int     `form:"maxSamples"`
		AcknowledgeDistillation bool    `form:"acknowledgeDistillation"`
	}

	ModelTrainingConfig struct {
		Iterations   int     `json:"iterations"`
		BatchSize    int     `json:"batchSize"`
		LearningRate float64 `json:"learningRate"`
		LoraRank     int     `json:"loraRank"`
		MaxSamples   int     `json:"maxSamples"`
	}

	ModelTrainingJobRsp struct {
		Ulid                string `json:"ulid"`
		CreatedAt           int64  `json:"createdAt"`
		UpdatedAt           int64  `json:"updatedAt"`
		Mode                string `json:"mode"`
		Name                string `json:"name"`
		StudentModelID      string `json:"studentModelId"`
		StudentModelName    string `json:"studentModelName"`
		TeacherModelID      string `json:"teacherModelId"`
		TeacherModelName    string `json:"teacherModelName"`
		DatasetOriginalName string `json:"datasetOriginalName"`
		OutputName          string `json:"outputName"`
		OutputModelID       string `json:"outputModelId"`
		Backend             string `json:"backend"`
		Status              string `json:"status"`
		Stage               string `json:"stage"`
		Progress            int    `json:"progress"`
		SampleCount         int    `json:"sampleCount"`
		ConfigJSON          string `json:"configJson"`
		MetricsJSON         string `json:"metricsJson"`
		LogText             string `json:"logText,omitempty"`
		ErrorMsg            string `json:"errorMsg,omitempty"`
		StartedAt           int64  `json:"startedAt"`
		FinishedAt          int64  `json:"finishedAt"`
	}
)
