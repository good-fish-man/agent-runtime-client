package runtime

import (
	"context"
	"time"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/runtime"
	log "github.com/good-fish-man/logx"
)

type MediaJobRepo struct{ data *data.Data }

func NewMediaJobRepo(data *data.Data) *MediaJobRepo { return &MediaJobRepo{data: data} }

var _ irepo.MediaJobRepository = (*MediaJobRepo)(nil)

func (r *MediaJobRepo) CreateMediaJob(ctx context.Context, job *entity.MediaGenerationJob) error {
	value := entityToPO(job)
	if err := r.data.DB(ctx).Create(value).Error; err != nil {
		return log.WrapError(err, "MediaJobRepo.Create")
	}
	job.Ulid, job.CreatedAt, job.UpdatedAt = value.Ulid, value.CreatedAt, value.UpdatedAt
	return nil
}

func (r *MediaJobRepo) UpdateMediaJob(ctx context.Context, job *entity.MediaGenerationJob) error {
	updates := map[string]any{
		"status": job.Status, "progress": job.Progress, "media_url": job.MediaURL,
		"mime_type": job.MimeType, "provider_job_id": job.ProviderJobID,
		"error_message": job.ErrorMessage, "started_at": job.StartedAt, "finished_at": job.FinishedAt,
	}
	return log.WrapError(r.data.DB(ctx).Model(&po.MediaGenerationJob{}).
		Where("ulid = ? AND user_id = ? AND deleted_at = 0", job.Ulid, job.UserID).Updates(updates).Error, "MediaJobRepo.Update")
}

func (r *MediaJobRepo) FindMediaJob(ctx context.Context, id, userID string) (*entity.MediaGenerationJob, error) {
	var value po.MediaGenerationJob
	if err := r.data.DB(ctx).Where("ulid = ? AND user_id = ? AND deleted_at = 0", id, userID).Limit(1).Find(&value).Error; err != nil {
		return nil, log.WrapError(err, "MediaJobRepo.Find")
	}
	return poToEntity(&value), nil
}

func (r *MediaJobRepo) ListMediaJobs(ctx context.Context, userID, mediaType string, limit int) ([]*entity.MediaGenerationJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	values := make([]po.MediaGenerationJob, 0)
	db := r.data.DB(ctx).Where("user_id = ? AND deleted_at = 0", userID)
	if mediaType != "" {
		db = db.Where("media_type = ?", mediaType)
	}
	if err := db.Order("ulid DESC").Limit(limit).Find(&values).Error; err != nil {
		return nil, log.WrapError(err, "MediaJobRepo.List")
	}
	result := make([]*entity.MediaGenerationJob, 0, len(values))
	for i := range values {
		result = append(result, poToEntity(&values[i]))
	}
	return result, nil
}

func (r *MediaJobRepo) DeleteMediaJob(ctx context.Context, id, userID string) error {
	return log.WrapError(r.data.DB(ctx).Model(&po.MediaGenerationJob{}).
		Where("ulid = ? AND user_id = ? AND deleted_at = 0", id, userID).
		Updates(map[string]any{"deleted_at": time.Now().UnixMilli()}).Error, "MediaJobRepo.Delete")
}

func (r *MediaJobRepo) FailInterruptedMediaJobs(ctx context.Context) error {
	now := time.Now().UnixMilli()
	return log.WrapError(r.data.DB(ctx).Model(&po.MediaGenerationJob{}).
		Where("status IN ? AND deleted_at = 0", []string{entity.MediaJobStatusQueued, entity.MediaJobStatusRunning}).
		Updates(map[string]any{"status": entity.MediaJobStatusFailed, "progress": 100, "finished_at": now, "error_message": "服务重启，生成任务已中断，请重新生成"}).Error, "MediaJobRepo.FailInterrupted")
}

func entityToPO(job *entity.MediaGenerationJob) *po.MediaGenerationJob {
	return &po.MediaGenerationJob{
		Ulid: job.Ulid, UserID: job.UserID, ModelID: job.ModelID, ModelName: job.ModelName,
		MediaType: job.MediaType, Prompt: job.Prompt, NegativePrompt: job.NegativePrompt,
		SourceURL: job.SourceURL, Size: job.Size, Quality: job.Quality, DurationSeconds: job.DurationSeconds,
		Status: job.Status, Progress: job.Progress, TraceID: job.TraceID,
	}
}

func poToEntity(job *po.MediaGenerationJob) *entity.MediaGenerationJob {
	return &entity.MediaGenerationJob{
		Ulid: job.Ulid, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, UserID: job.UserID,
		ModelID: job.ModelID, ModelName: job.ModelName, MediaType: job.MediaType, Prompt: job.Prompt,
		NegativePrompt: job.NegativePrompt, SourceURL: job.SourceURL, Size: job.Size, Quality: job.Quality,
		DurationSeconds: job.DurationSeconds, Status: job.Status, Progress: job.Progress,
		MediaURL: job.MediaURL, MimeType: job.MimeType, ProviderJobID: job.ProviderJobID,
		ErrorMessage: job.ErrorMessage, TraceID: job.TraceID, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
	}
}
