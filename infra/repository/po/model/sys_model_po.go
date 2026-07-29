package model

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

// SysModel is the gorm persistence object mapped to table sys_model.
type SysModel struct {
	Ulid          string `gorm:"column:ulid;primaryKey;type:varchar(128);comment:ulid;" json:"ulid"`
	CreatedAt     int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint;comment:创建时间;" json:"created_at"`
	UpdatedAt     int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint;comment:修改时间;" json:"updated_at"`
	DeletedAt     int64  `gorm:"column:deleted_at;type:bigint;comment:删除时间;" json:"deleted_at"`
	CreatedBy     string `gorm:"column:created_by;type:varchar(128);comment:创建者;" json:"created_by"`
	UpdatedBy     string `gorm:"column:updated_by;type:varchar(128);comment:修改者;" json:"updated_by"`
	Name          string `gorm:"column:name;type:varchar(100);comment:模型名称;" json:"name"`
	Provider      string `gorm:"column:provider;type:varchar(50);comment:提供商;" json:"provider"`
	BaseUrl       string `gorm:"column:base_url;type:varchar(255);comment:API地址;" json:"base_url"`
	KeyID         string `gorm:"column:key_id;type:varchar(128);index;comment:用户模型Key ID;" json:"key_id"`
	ModelType     string `gorm:"column:model_type;type:varchar(20);comment:llm/embedding/image/video;" json:"model_type"`
	Category      string `gorm:"column:category;type:varchar(20);comment:default/rewrite/skill/summarize;" json:"category"`
	Status        string `gorm:"column:status;type:varchar(20);comment:active/configured/error;" json:"status"`
	Latency       string `gorm:"column:latency;type:varchar(20);comment:平均延迟;" json:"latency"`
	ContextWindow string `gorm:"column:context_window;type:varchar(20);comment:上下文窗口;" json:"context_window"`
	Capabilities  string `gorm:"column:capabilities;type:varchar(255);comment:模型能力;" json:"capabilities"`
	Usage         int    `gorm:"column:usage;type:int;default:0;comment:使用次数;" json:"usage"`
	Enabled       bool   `gorm:"column:enabled;type:boolean;not null;default:true;index;comment:管理员是否启用;" json:"enabled"`
	RuntimeMode   string `gorm:"column:runtime_mode;type:varchar(20);not null;default:on_demand;index;comment:本地模型运行模式;" json:"runtime_mode"`
}

// BeforeCreate assigns a ULID primary key when absent.
func (po *SysModel) BeforeCreate(tx *gorm.DB) (err error) {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return
}

// TableName maps to the shared sys_model table.
func (po *SysModel) TableName() string { return "sys_model" }

// SysModelKey is a reusable user-owned provider credential.
type SysModelKey struct {
	Ulid      string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	CreatedAt int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint" json:"created_at"`
	UpdatedAt int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint" json:"updated_at"`
	DeletedAt int64  `gorm:"column:deleted_at;type:bigint;default:0;index" json:"deleted_at"`
	UserID    string `gorm:"column:user_id;type:varchar(128);not null;index:idx_model_key_owner" json:"user_id"`
	Name      string `gorm:"column:name;type:varchar(100);not null" json:"name"`
	Provider  string `gorm:"column:provider;type:varchar(50);not null;index:idx_model_key_owner" json:"provider"`
	APIKey    string `gorm:"column:api_key;type:varchar(2000)" json:"-"`
	BaseURL   string `gorm:"column:base_url;type:varchar(500)" json:"base_url"`
	Enabled   bool   `gorm:"column:enabled;type:boolean;default:true" json:"enabled"`
}

func (po *SysModelKey) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return nil
}

func (*SysModelKey) TableName() string { return "sys_model_key" }

// ModelCatalog stores built-in selectable model presets.
type ModelCatalog struct {
	Ulid           string `gorm:"column:ulid;primaryKey;type:varchar(128);comment:ulid;" json:"ulid"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint;comment:创建时间;" json:"created_at"`
	UpdatedAt      int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint;comment:修改时间;" json:"updated_at"`
	Provider       string `gorm:"column:provider;type:varchar(50);index:idx_model_catalog_provider_type;comment:提供商;" json:"provider"`
	ModelType      string `gorm:"column:model_type;type:varchar(20);index:idx_model_catalog_provider_type;comment:llm/embedding;" json:"model_type"`
	ModelFamily    string `gorm:"column:model_family;type:varchar(80);comment:模型大类;" json:"model_family"`
	ModelVersion   string `gorm:"column:model_version;type:varchar(120);uniqueIndex:uk_model_catalog_version;comment:模型小版本;" json:"model_version"`
	DisplayName    string `gorm:"column:display_name;type:varchar(160);comment:显示名称;" json:"display_name"`
	DefaultBaseUrl string `gorm:"column:default_base_url;type:varchar(255);comment:默认请求地址;" json:"default_base_url"`
	ContextWindow  string `gorm:"column:context_window;type:varchar(20);comment:上下文窗口;" json:"context_window"`
	IsFree         bool   `gorm:"column:is_free;type:boolean;default:false;comment:是否免费本地模型;" json:"is_free"`
	Installable    bool   `gorm:"column:installable;type:boolean;default:false;comment:是否支持自动安装;" json:"installable"`
	Runtime        string `gorm:"column:runtime;type:varchar(40);comment:本地模型运行时;" json:"runtime"`
	DownloadSize   string `gorm:"column:download_size;type:varchar(32);comment:预计下载大小;" json:"download_size"`
	MinMemoryGB    int    `gorm:"column:min_memory_gb;type:int;default:0;comment:最低内存GB;" json:"min_memory_gb"`
	Capabilities   string `gorm:"column:capabilities;type:varchar(255);comment:模型能力;" json:"capabilities"`
	Description    string `gorm:"column:description;type:varchar(500);comment:模型说明;" json:"description"`
	Enabled        bool   `gorm:"column:enabled;type:boolean;default:true;comment:是否启用;" json:"enabled"`
	Sort           int    `gorm:"column:sort;type:int;default:0;comment:排序;" json:"sort"`
}

// BeforeCreate assigns a ULID primary key when absent.
func (po *ModelCatalog) BeforeCreate(tx *gorm.DB) (err error) {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return
}

// TableName maps to the model preset catalog table.
func (po *ModelCatalog) TableName() string { return "sys_model_catalog" }

// ModelTrainingJob stores a user-owned fine-tuning or distillation run.
type ModelTrainingJob struct {
	Ulid                string `gorm:"column:ulid;primaryKey;type:varchar(128)" json:"ulid"`
	CreatedAt           int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint" json:"created_at"`
	UpdatedAt           int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint" json:"updated_at"`
	DeletedAt           int64  `gorm:"column:deleted_at;type:bigint;default:0;index" json:"deleted_at"`
	UserID              string `gorm:"column:user_id;type:varchar(128);not null;index:idx_training_owner_status" json:"user_id"`
	Mode                string `gorm:"column:mode;type:varchar(20);not null" json:"mode"`
	Name                string `gorm:"column:name;type:varchar(160);not null" json:"name"`
	StudentModelID      string `gorm:"column:student_model_id;type:varchar(128);not null;index" json:"student_model_id"`
	StudentModelName    string `gorm:"column:student_model_name;type:varchar(160);not null" json:"student_model_name"`
	TeacherModelID      string `gorm:"column:teacher_model_id;type:varchar(128);index" json:"teacher_model_id"`
	TeacherModelName    string `gorm:"column:teacher_model_name;type:varchar(160)" json:"teacher_model_name"`
	DatasetPath         string `gorm:"column:dataset_path;type:varchar(1000);not null" json:"-"`
	DatasetOriginalName string `gorm:"column:dataset_original_name;type:varchar(255)" json:"dataset_original_name"`
	OutputName          string `gorm:"column:output_name;type:varchar(160);not null" json:"output_name"`
	OutputModelID       string `gorm:"column:output_model_id;type:varchar(128);index" json:"output_model_id"`
	Backend             string `gorm:"column:backend;type:varchar(32)" json:"backend"`
	Status              string `gorm:"column:status;type:varchar(20);not null;index:idx_training_owner_status" json:"status"`
	Stage               string `gorm:"column:stage;type:varchar(32)" json:"stage"`
	Progress            int    `gorm:"column:progress;type:int;default:0" json:"progress"`
	SampleCount         int    `gorm:"column:sample_count;type:int;default:0" json:"sample_count"`
	ConfigJSON          string `gorm:"column:config_json;type:text" json:"config_json"`
	MetricsJSON         string `gorm:"column:metrics_json;type:text" json:"metrics_json"`
	LogText             string `gorm:"column:log_text;type:text" json:"log_text"`
	ErrorMsg            string `gorm:"column:error_msg;type:text" json:"error_msg"`
	StartedAt           int64  `gorm:"column:started_at;type:bigint;default:0" json:"started_at"`
	FinishedAt          int64  `gorm:"column:finished_at;type:bigint;default:0" json:"finished_at"`
}

func (po *ModelTrainingJob) BeforeCreate(tx *gorm.DB) error {
	if po.Ulid == "" {
		po.Ulid = ulid.New()
	}
	return nil
}

func (*ModelTrainingJob) TableName() string { return "sys_model_training_job" }
