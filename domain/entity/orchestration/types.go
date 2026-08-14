package orchestration

import orchestrationv1 "github.com/good-fish-man/athena-protocol/protocol/orchestration/v1"

const Schema = orchestrationv1.Schema

type PersistentGoal = orchestrationv1.PersistentGoal
type GoalBudget = orchestrationv1.GoalBudget
type BudgetUsage = orchestrationv1.BudgetUsage
type SuccessCriterion = orchestrationv1.SuccessCriterion
type ApprovalPolicy = orchestrationv1.ApprovalPolicy
type TaskBudget = orchestrationv1.TaskBudget
type SpecialistTask = orchestrationv1.SpecialistTask
type TaskGraph = orchestrationv1.TaskGraph
type Provenance = orchestrationv1.Provenance
type SpecialistResult = orchestrationv1.SpecialistResult
type GoalCheckpoint = orchestrationv1.GoalCheckpoint
type DeviceCandidate = orchestrationv1.DeviceCandidate
type RouteDecision = orchestrationv1.RouteDecision
type ScheduleTrigger = orchestrationv1.ScheduleTrigger

type GoalFilter struct {
	Statuses []string
	Limit    int
}
