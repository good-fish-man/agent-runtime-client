package migration

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	controlpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/control"
	protocolv4 "github.com/good-fish-man/athena-protocol/protocol/v4"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// migrateControlV3 copies legacy control records into the v0.2 kernel. The old
// tables remain untouched so operators can verify the migration before removal.
func migrateControlV3(ctx context.Context, db *gorm.DB) error {
	if !db.Migrator().HasTable(&controlpo.LegacyV3Task{}) {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := migrateV3Devices(tx); err != nil {
			return err
		}
		if err := migrateV3Tasks(tx); err != nil {
			return err
		}
		actions, err := migrateV3Actions(tx)
		if err != nil {
			return err
		}
		return migrateV3Observations(tx, actions)
	})
}

func migrateV3Devices(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&controlpo.LegacyV3Device{}) {
		return nil
	}
	var values []controlpo.LegacyV3Device
	if err := tx.Find(&values).Error; err != nil {
		return err
	}
	for _, value := range values {
		lease := value.LastSeenAt + int64((45*time.Second)/time.Millisecond)
		migrated := &controlpo.Device{
			DeviceID: value.DeviceID, UserID: value.UserID, Protocol: protocolv4.Protocol,
			Name: value.Name, Platform: value.Platform, Architecture: value.Architecture,
			Capabilities: value.Capabilities, CapabilityInstances: "[]", Online: value.Online,
			Revision: 1, ConnectedAt: value.ConnectedAt, LastSeenAt: value.LastSeenAt,
			LeaseExpiresAt: lease, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(migrated).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateV3Tasks(tx *gorm.DB) error {
	var values []controlpo.LegacyV3Task
	if err := tx.Find(&values).Error; err != nil {
		return err
	}
	for _, value := range values {
		migrated := &controlpo.Task{
			TaskID: value.TaskID, ConversationID: value.ConversationID, UserID: value.UserID,
			DeviceID: value.DeviceID, Status: migrateV3TaskStatus(value.Status), Sequence: value.Sequence,
			Revision: 1, ActiveSessions: jsonOr(value.ActiveSessions, "{}"), Metadata: jsonOr(value.Metadata, "{}"),
			Result: "{}", ErrorDetail: "null", CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(migrated).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateV3Actions(tx *gorm.DB) (map[string]controlpo.Action, error) {
	result := make(map[string]controlpo.Action)
	if !tx.Migrator().HasTable(&controlpo.LegacyV3Action{}) {
		return result, nil
	}
	var values []controlpo.LegacyV3Action
	if err := tx.Order("task_id ASC, sequence ASC").Find(&values).Error; err != nil {
		return nil, err
	}
	for _, value := range values {
		stepID := "step-v3-" + value.ActionID
		issuedAt := value.CreatedAt
		if issuedAt == 0 {
			issuedAt = value.Deadline - int64((2*time.Minute)/time.Millisecond)
		}
		risk := migrateV3Risk(value.Risk)
		policy, _ := json.Marshal(protocolv4.Policy{Risk: risk, Decision: value.Decision})
		expected, _ := json.Marshal(protocolv4.ExpectedObservation{Kind: value.Capability, TimeoutMS: int64((2 * time.Minute) / time.Millisecond)})
		migrated := controlpo.Action{
			ActionID: value.ActionID, TaskID: value.TaskID, StepID: stepID, DeviceID: value.DeviceID,
			CapabilityInstanceID: value.DeviceID + ":" + strings.ReplaceAll(value.Capability, ".", "-"),
			UserID:               value.UserID, SessionID: value.SessionID, Protocol: protocolv4.Protocol,
			Sequence: value.Sequence, Revision: 1, IdempotencyKey: value.IdempotencyKey,
			IssuedAt: issuedAt, Deadline: value.Deadline, Capability: value.Capability,
			Operation: capabilityOperation(value.Capability), Target: "{}", Arguments: jsonOr(value.Arguments, "{}"),
			Risk: risk, Decision: value.Decision, Policy: string(policy), ExpectedObservation: string(expected),
			CreatedAt: value.CreatedAt,
		}
		step := controlpo.Step{
			StepID: stepID, TaskID: value.TaskID, Ordinal: int(value.Sequence), Status: protocolv4.StepStatusCompleted,
			Capability: value.Capability, Operation: migrated.Operation, Target: "{}", Input: migrated.Arguments,
			ExpectedObservation: migrated.ExpectedObservation, RetryPolicy: "{}", CreatedAt: issuedAt, UpdatedAt: issuedAt,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&step).Error; err != nil {
			return nil, err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&migrated).Error; err != nil {
			return nil, err
		}
		result[value.ActionID] = migrated
	}
	return result, nil
}

func migrateV3Observations(tx *gorm.DB, actions map[string]controlpo.Action) error {
	if !tx.Migrator().HasTable(&controlpo.LegacyV3Observation{}) {
		return nil
	}
	var values []controlpo.LegacyV3Observation
	if err := tx.Find(&values).Error; err != nil {
		return err
	}
	for _, value := range values {
		action, ok := actions[value.ActionID]
		if !ok {
			if err := tx.Where("action_id = ?", value.ActionID).Limit(1).Find(&action).Error; err != nil {
				return err
			}
			if action.ActionID == "" {
				continue
			}
		}
		migrated := &controlpo.Observation{
			ObservationID: "observation-v3-" + value.ActionID, ActionID: value.ActionID, TaskID: value.TaskID,
			StepID: action.StepID, DeviceID: action.DeviceID, IdempotencyKey: value.IdempotencyKey,
			SessionID: value.SessionID, Protocol: protocolv4.Protocol, Sequence: value.Sequence, Revision: 1,
			Status: value.Status, FinishedAt: value.ObservedAt, ObservedAt: value.ObservedAt,
			State: jsonOr(value.State, "{}"), Evidence: "[]", WorldPatch: "null", Error: value.Error,
			ErrorDetail: "null", CreatedAt: value.CreatedAt,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(migrated).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateV3TaskStatus(status string) string {
	switch status {
	case "UNDERSTANDING":
		return protocolv4.TaskStatusCreated
	case "PLANNING":
		return protocolv4.TaskStatusPlanning
	case "WAITING_ACTION", "OBSERVING":
		return protocolv4.TaskStatusWaitingObservation
	case "WAITING_APPROVAL":
		return protocolv4.TaskStatusWaitingApproval
	case "EXECUTING":
		return protocolv4.TaskStatusRunning
	case "EVALUATING":
		return protocolv4.TaskStatusVerifying
	case "WAITING_USER", "COMPLETED", "FAILED", "CANCELLED":
		return status
	default:
		return protocolv4.TaskStatusFailed
	}
}

func migrateV3Risk(risk string) string {
	switch risk {
	case "LOW":
		return protocolv4.RiskReadOnly
	case "MEDIUM":
		return protocolv4.RiskReversible
	case "HIGH":
		return protocolv4.RiskExternalWrite
	default:
		return protocolv4.RiskSensitive
	}
}

func capabilityOperation(capability string) string {
	if index := strings.LastIndex(capability, "."); index >= 0 && index+1 < len(capability) {
		return capability[index+1:]
	}
	return capability
}

func jsonOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" || !json.Valid([]byte(value)) {
		return fallback
	}
	return value
}
