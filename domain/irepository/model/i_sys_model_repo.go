package model

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

// ISysModelRepo is the persistence port for SysModel.
type ISysModelRepo interface {
	Create(ctx context.Context, en *entity.SysModel) (ulid string, err error)
	Delete(ctx context.Context, en *entity.SysModel) (err error)
	Update(ctx context.Context, en *entity.SysModel) (err error)
	UpdateEnabled(ctx context.Context, ulid, updatedBy string, enabled bool) (err error)
	UpdateRuntimeMode(ctx context.Context, ulid, updatedBy, runtimeMode string) (err error)
	FindById(ctx context.Context, ulid string, selectColumn ...string) (en *entity.SysModel, err error)
	FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) (entries []*entity.SysModel, err error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) (entries []*entity.SysModel, rspPage *query.PageData, err error)
	FindRecentUsageMetrics(ctx context.Context, since int64) ([]entity.ModelUsageMetric, error)
	CreateKey(ctx context.Context, key *entity.SysModelKey) (string, error)
	UpdateKey(ctx context.Context, key *entity.SysModelKey) error
	DeleteKey(ctx context.Context, keyID, userID string) error
	FindKeyByID(ctx context.Context, keyID string) (*entity.SysModelKey, error)
	FindKeysByUser(ctx context.Context, userID string) ([]*entity.SysModelKey, error)
	CountModelsByKey(ctx context.Context, keyID, userID string) (int64, error)
}
