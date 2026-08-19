package delegation

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/delegation"
	repository "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/delegation"
	log "github.com/good-fish-man/logx"
)

var _ repository.ParallelStore = (*Store)(nil)

func (s *Store) CreateParallelPlan(ctx context.Context, value entity.ParallelPlanBundle) error {
	if strings.TrimSpace(value.Plan.PlanID) == "" || strings.TrimSpace(value.Plan.OwnerID) == "" || strings.TrimSpace(value.Plan.DefinitionHash) == "" || strings.TrimSpace(value.Plan.Content) == "" || value.Plan.Revision <= 0 || value.Plan.CreatedAt.IsZero() || len(value.Nodes) == 0 {
		return fmt.Errorf("parallel plan requires identity, owner, definition, revision, timestamp, and nodes")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing po.ParallelPlan
		found := tx.Where("plan_id = ?", value.Plan.PlanID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.OwnerID == value.Plan.OwnerID && existing.DefinitionHash == value.Plan.DefinitionHash {
				return nil
			}
			return ErrIdempotencyConflict
		}
		plan := parallelPlanRow(value.Plan)
		if err := tx.Create(&plan).Error; err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(value.Nodes))
		for _, node := range value.Nodes {
			if node.PlanID != value.Plan.PlanID || node.OwnerID != value.Plan.OwnerID || strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.Content) == "" || node.Revision <= 0 {
				return fmt.Errorf("parallel node does not belong to its plan or is incomplete")
			}
			if _, duplicate := seen[node.NodeID]; duplicate {
				return fmt.Errorf("parallel node %q is duplicated", node.NodeID)
			}
			seen[node.NodeID] = struct{}{}
			row := parallelNodeRow(node)
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return createEvent(tx, value.Event)
	}), "DelegationStore.CreateParallelPlan")
}

func (s *Store) TransitionParallelNode(ctx context.Context, value entity.ParallelNodeTransition) error {
	if strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.PlanID) == "" || strings.TrimSpace(value.NodeID) == "" || strings.TrimSpace(value.Status) == "" || value.ExpectedRevision <= 0 || value.UpdatedAt.IsZero() {
		return fmt.Errorf("parallel node transition requires owner, plan, node, status, revision, and timestamp")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var current po.ParallelNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("plan_id = ? AND node_id = ? AND owner_id = ?", value.PlanID, value.NodeID, value.OwnerID).Take(&current).Error; err != nil {
			return err
		}
		if current.Revision != value.ExpectedRevision {
			if current.Revision == value.ExpectedRevision+1 && parallelNodeTransitionMatches(current, value) {
				return nil
			}
			return ErrRevisionConflict
		}
		updates := map[string]any{
			"role": value.Role, "status": value.Status, "attempt": value.Attempt,
			"result_id": value.ResultID, "error_chain": value.ErrorChain, "content": value.Content,
			"revision": value.ExpectedRevision + 1, "updated_at": millis(value.UpdatedAt),
		}
		updated := tx.Model(&po.ParallelNode{}).
			Where("plan_id = ? AND node_id = ? AND owner_id = ? AND revision = ?", value.PlanID, value.NodeID, value.OwnerID, value.ExpectedRevision).
			Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, value.Event)
	}), "DelegationStore.TransitionParallelNode")
}

func (s *Store) CompleteParallelPlan(ctx context.Context, value entity.ParallelPlanCompletion) error {
	if strings.TrimSpace(value.OwnerID) == "" || strings.TrimSpace(value.PlanID) == "" || strings.TrimSpace(value.Status) == "" || value.ExpectedRevision <= 0 || value.CompletedAt.IsZero() || value.Aggregate.PlanID != value.PlanID || value.Aggregate.OwnerID != value.OwnerID || strings.TrimSpace(value.Aggregate.AggregateID) == "" || strings.TrimSpace(value.Aggregate.Content) == "" {
		return fmt.Errorf("parallel plan completion is incomplete or crosses an owner boundary")
	}
	return log.WrapError(s.data.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var plan po.ParallelPlan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("plan_id = ? AND owner_id = ?", value.PlanID, value.OwnerID).Take(&plan).Error; err != nil {
			return err
		}
		if plan.Revision != value.ExpectedRevision {
			var existing po.ParallelAggregate
			found := tx.Where("plan_id = ?", value.PlanID).Limit(1).Find(&existing)
			if found.Error != nil {
				return found.Error
			}
			if found.RowsAffected > 0 && existing.AggregateID == value.Aggregate.AggregateID && existing.OwnerID == value.OwnerID && plan.Status == value.Status {
				return nil
			}
			return ErrRevisionConflict
		}
		var existing po.ParallelAggregate
		found := tx.Where("plan_id = ?", value.PlanID).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.AggregateID == value.Aggregate.AggregateID && existing.OwnerID == value.OwnerID {
				return nil
			}
			return ErrIdempotencyConflict
		}
		aggregate := parallelAggregateRow(value.Aggregate)
		if err := tx.Create(&aggregate).Error; err != nil {
			return err
		}
		updated := tx.Model(&po.ParallelPlan{}).
			Where("plan_id = ? AND owner_id = ? AND revision = ?", value.PlanID, value.OwnerID, value.ExpectedRevision).
			Updates(map[string]any{"status": value.Status, "revision": value.ExpectedRevision + 1, "updated_at": millis(value.CompletedAt)})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		return createEvent(tx, value.Event)
	}), "DelegationStore.CompleteParallelPlan")
}

func (s *Store) FindParallelPlan(ctx context.Context, ownerID, planID string) (*entity.ParallelPlan, []entity.ParallelNode, *entity.ParallelAggregate, error) {
	var planRow po.ParallelPlan
	result := s.data.DB(ctx).Where("owner_id = ? AND plan_id = ?", ownerID, planID).Limit(1).Find(&planRow)
	if result.Error != nil {
		return nil, nil, nil, log.WrapError(result.Error, "DelegationStore.FindParallelPlan.plan")
	}
	if result.RowsAffected == 0 {
		return nil, nil, nil, nil
	}
	var nodeRows []po.ParallelNode
	if err := s.data.DB(ctx).Where("owner_id = ? AND plan_id = ?", ownerID, planID).Order("node_id ASC").Find(&nodeRows).Error; err != nil {
		return nil, nil, nil, log.WrapError(err, "DelegationStore.FindParallelPlan.nodes")
	}
	nodes := make([]entity.ParallelNode, 0, len(nodeRows))
	for _, row := range nodeRows {
		nodes = append(nodes, parallelNodeEntity(row))
	}
	var aggregateRow po.ParallelAggregate
	aggregateResult := s.data.DB(ctx).Where("owner_id = ? AND plan_id = ?", ownerID, planID).Limit(1).Find(&aggregateRow)
	if aggregateResult.Error != nil {
		return nil, nil, nil, log.WrapError(aggregateResult.Error, "DelegationStore.FindParallelPlan.aggregate")
	}
	var aggregate *entity.ParallelAggregate
	if aggregateResult.RowsAffected > 0 {
		value := parallelAggregateEntity(aggregateRow)
		aggregate = &value
	}
	plan := parallelPlanEntity(planRow)
	return &plan, nodes, aggregate, nil
}

func parallelPlanRow(value entity.ParallelPlan) po.ParallelPlan {
	return po.ParallelPlan{PlanID: value.PlanID, OwnerID: value.OwnerID, GoalID: value.GoalID, TaskStepID: value.TaskStepID, Status: value.Status, DefinitionHash: value.DefinitionHash, Content: value.Content, Revision: value.Revision, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
}

func parallelPlanEntity(value po.ParallelPlan) entity.ParallelPlan {
	return entity.ParallelPlan{PlanID: value.PlanID, OwnerID: value.OwnerID, GoalID: value.GoalID, TaskStepID: value.TaskStepID, Status: value.Status, DefinitionHash: value.DefinitionHash, Content: value.Content, Revision: value.Revision, CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt)}
}

func parallelNodeRow(value entity.ParallelNode) po.ParallelNode {
	return po.ParallelNode{PlanID: value.PlanID, NodeID: value.NodeID, OwnerID: value.OwnerID, Role: value.Role, Status: value.Status, Attempt: value.Attempt, ResultID: value.ResultID, ErrorChain: value.ErrorChain, Content: value.Content, Revision: value.Revision, CreatedAt: millis(value.CreatedAt), UpdatedAt: millis(value.UpdatedAt)}
}

func parallelNodeEntity(value po.ParallelNode) entity.ParallelNode {
	return entity.ParallelNode{PlanID: value.PlanID, NodeID: value.NodeID, OwnerID: value.OwnerID, Role: value.Role, Status: value.Status, Attempt: value.Attempt, ResultID: value.ResultID, ErrorChain: value.ErrorChain, Content: value.Content, Revision: value.Revision, CreatedAt: fromMillis(value.CreatedAt), UpdatedAt: fromMillis(value.UpdatedAt)}
}

func parallelAggregateRow(value entity.ParallelAggregate) po.ParallelAggregate {
	return po.ParallelAggregate{AggregateID: value.AggregateID, PlanID: value.PlanID, OwnerID: value.OwnerID, Status: value.Status, Content: value.Content, CreatedAt: millis(value.CreatedAt)}
}

func parallelAggregateEntity(value po.ParallelAggregate) entity.ParallelAggregate {
	return entity.ParallelAggregate{AggregateID: value.AggregateID, PlanID: value.PlanID, OwnerID: value.OwnerID, Status: value.Status, Content: value.Content, CreatedAt: fromMillis(value.CreatedAt)}
}

func parallelNodeTransitionMatches(current po.ParallelNode, value entity.ParallelNodeTransition) bool {
	return current.Role == value.Role && current.Status == value.Status && current.Attempt == value.Attempt && current.ResultID == value.ResultID && current.ErrorChain == value.ErrorChain && current.Content == value.Content
}
