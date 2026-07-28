package job

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/job"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/job"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	converter "github.com/good-fish-man/agent-runtime-client/infra/repository/converter/job"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/job"
	"github.com/good-fish-man/agent-runtime-client/pkg/log"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

var _ irepo.IJobExecutionRepo = (*JobExecutionRepo)(nil)

// JobExecutionRepo is the gorm-backed implementation of IJobExecutionRepo.
type JobExecutionRepo struct {
	data *data.Data
}

// NewJobExecutionRepo constructs the repository with the shared data handle.
func NewJobExecutionRepo(d *data.Data) *JobExecutionRepo {
	return &JobExecutionRepo{data: d}
}

func (r *JobExecutionRepo) Create(ctx context.Context, en *entity.JobExecution) (string, error) {
	p := converter.E2PJobExecutionAdd(en)
	if err := r.data.DB(ctx).Create(p).Error; err != nil {
		return "", log.WrapError(err, "Repository")
	}
	return p.Ulid, nil
}

func (r *JobExecutionRepo) Delete(ctx context.Context, en *entity.JobExecution) error {
	patch := converter.E2PJobExecutionDel(en)
	return log.WrapError(r.data.DB(ctx).Model(&po.JobExecutionPO{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *JobExecutionRepo) Update(ctx context.Context, en *entity.JobExecution) error {
	patch := converter.E2PJobExecutionUpdate(en)
	return log.WrapError(r.data.DB(ctx).Model(&po.JobExecutionPO{}).Where("ulid = ?", en.Ulid).Updates(patch).Error, "Repository")
}

func (r *JobExecutionRepo) FindById(ctx context.Context, ulid string, selectColumn ...string) (*entity.JobExecution, error) {
	var p po.JobExecutionPO
	db := r.data.DB(ctx)
	if len(selectColumn) > 0 {
		db = db.Select(selectColumn)
	}
	if err := db.Limit(1).Find(&p, "ulid = ? AND deleted_at = 0", ulid).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.P2EJobExecution(&p), nil
}

func (r *JobExecutionRepo) FindByQuery(ctx context.Context, queries []*query.Query) (*entity.JobExecution, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	var p po.JobExecutionPO
	db := r.data.DB(ctx).Model(&po.JobExecutionPO{}).Where("deleted_at = ?", 0)
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Limit(1).Find(&p).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.P2EJobExecution(&p), nil
}

func (r *JobExecutionRepo) FindAll(ctx context.Context, queries []*query.Query, selectArgs ...[]string) ([]*entity.JobExecution, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	ps := make([]*po.JobExecutionPO, 0)
	db := r.data.DB(ctx).Model(&po.JobExecutionPO{}).Select(query.SelectFields(selectArgs...)).Where("deleted_at = ?", 0)
	if where != "" {
		db = db.Where(where, values...)
	}
	if err := db.Order("ulid desc").Find(&ps).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.P2EJobExecutions(ps), nil
}

func (r *JobExecutionRepo) FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData, selectArgs ...[]string) ([]*entity.JobExecution, *query.PageData, error) {
	where, values, err := query.BuildWhere(queries)
	if err != nil {
		return nil, nil, log.WrapError(err, "Repository")
	}
	if reqPage == nil {
		reqPage = &query.PageData{PageNum: 1, PageSize: 10}
	}

	ps := make([]*po.JobExecutionPO, 0)
	db := r.data.DB(ctx).Model(&po.JobExecutionPO{}).Where("deleted_at = ?", 0)
	if where != "" {
		db = db.Where(where, values...)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, nil, log.WrapError(err, "Repository")
	}
	rspPage := &query.PageData{
		PageNum:     reqPage.PageNum,
		PageSize:    reqPage.PageSize,
		TotalNumber: total,
		TotalPage:   query.CeilPageNum(total, reqPage.PageSize),
	}
	if total == 0 {
		return converter.P2EJobExecutions(ps), rspPage, nil
	}

	err = db.Select(query.SelectFields(selectArgs...)).
		Order(reqSort.OrderBy("ulid")).
		Scopes(query.Paginate(reqPage.PageNum, reqPage.PageSize)).
		Find(&ps).Error
	if err != nil {
		return nil, nil, log.WrapError(err, "Repository")
	}
	return converter.P2EJobExecutions(ps), rspPage, nil
}

func (r *JobExecutionRepo) FindByAgentId(ctx context.Context, agentId string, limit int) ([]*entity.JobExecution, error) {
	ps := make([]*po.JobExecutionPO, 0)
	if err := r.data.DB(ctx).Model(&po.JobExecutionPO{}).
		Where("agent_id = ? AND deleted_at = ?", agentId, 0).
		Order("trigger_time DESC").
		Limit(limit).
		Find(&ps).Error; err != nil {
		return nil, log.WrapError(err, "Repository")
	}
	return converter.P2EJobExecutions(ps), nil
}

func (r *JobExecutionRepo) DeleteOldByAgentId(ctx context.Context, agentId string, keepCount int) error {
	sql := `DELETE FROM job_execution_log WHERE agent_id = ? AND deleted_at = 0
		AND ulid NOT IN (
			SELECT ulid FROM (
				SELECT ulid FROM job_execution_log
				WHERE agent_id = ? AND deleted_at = 0
				ORDER BY trigger_time DESC LIMIT ?
			) t
		)`
	return log.WrapError(r.data.DB(ctx).Exec(sql, agentId, agentId, keepCount).Error, "Repository")
}

func (r *JobExecutionRepo) CountByAgentId(ctx context.Context, agentId string) (int, error) {
	var count int64
	err := r.data.DB(ctx).Model(&po.JobExecutionPO{}).
		Where("agent_id = ? AND deleted_at = ?", agentId, 0).
		Count(&count).Error
	return int(count), err
}

func (r *JobExecutionRepo) CountByStatus(ctx context.Context, status string) (int, error) {
	var count int64
	err := r.data.DB(ctx).Model(&po.JobExecutionPO{}).
		Where("status = ? AND deleted_at = ?", status, 0).
		Count(&count).Error
	return int(count), err
}
