package runtime

import (
	"gorm.io/gorm"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
)

// MediaGenerationJob stores durable image and video generation history.
type MediaGenerationJob struct {
	Ulid            string `gorm:"column:ulid;primaryKey;type:varchar(128)"`
	CreatedAt       int64  `gorm:"column:created_at;autoCreateTime:milli;type:bigint"`
	UpdatedAt       int64  `gorm:"column:updated_at;autoUpdateTime:milli;type:bigint"`
	DeletedAt       int64  `gorm:"column:deleted_at;type:bigint;default:0;index"`
	UserID          string `gorm:"column:user_id;type:varchar(128);not null;index:idx_media_job_owner_status"`
	ModelID         string `gorm:"column:model_id;type:varchar(128);not null;index"`
	ModelName       string `gorm:"column:model_name;type:varchar(160);not null"`
	MediaType       string `gorm:"column:media_type;type:varchar(16);not null;index:idx_media_job_owner_status"`
	Prompt          string `gorm:"column:prompt;type:text;not null"`
	NegativePrompt  string `gorm:"column:negative_prompt;type:text"`
	SourceURL       string `gorm:"column:source_url;type:text"`
	Size            string `gorm:"column:size;type:varchar(32)"`
	Quality         string `gorm:"column:quality;type:varchar(32)"`
	DurationSeconds int    `gorm:"column:duration_seconds;type:int;default:0"`
	Status          string `gorm:"column:status;type:varchar(20);not null;index:idx_media_job_owner_status"`
	Progress        int    `gorm:"column:progress;type:int;default:0"`
	MediaURL        string `gorm:"column:media_url;type:text"`
	MimeType        string `gorm:"column:mime_type;type:varchar(100)"`
	ProviderJobID   string `gorm:"column:provider_job_id;type:varchar(255)"`
	ErrorMessage    string `gorm:"column:error_message;type:text"`
	TraceID         string `gorm:"column:trace_id;type:varchar(128);index"`
	StartedAt       int64  `gorm:"column:started_at;type:bigint;default:0"`
	FinishedAt      int64  `gorm:"column:finished_at;type:bigint;default:0"`
}

func (job *MediaGenerationJob) BeforeCreate(*gorm.DB) error {
	if job.Ulid == "" {
		job.Ulid = ulid.New()
	}
	return nil
}

func (*MediaGenerationJob) TableName() string { return "sys_media_generation_job" }
