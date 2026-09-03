package orchestration

import orchestrationv2 "github.com/good-fish-man/athena-protocol/protocol/orchestration/v2"

const Schema = orchestrationv2.Schema

type PersistentGoal = orchestrationv2.PersistentGoal
type GoalBudget = orchestrationv2.GoalBudget
type BudgetUsage = orchestrationv2.BudgetUsage
type SuccessCriterion = orchestrationv2.SuccessCriterion
type GoalInput = orchestrationv2.GoalInput
type ApprovalPolicy = orchestrationv2.ApprovalPolicy
type GoalTrigger = orchestrationv2.GoalTrigger
type TaskBudget = orchestrationv2.TaskBudget
type SpecialistTask = orchestrationv2.SpecialistTask
type TaskGraph = orchestrationv2.TaskGraph
type Provenance = orchestrationv2.Provenance
type SpecialistResult = orchestrationv2.SpecialistResult
type GoalCheckpoint = orchestrationv2.GoalCheckpoint
type DeviceCandidate = orchestrationv2.DeviceCandidate
type RouteDecision = orchestrationv2.RouteDecision
type ScheduleTrigger = orchestrationv2.ScheduleTrigger

type GoalFilter struct {
	Statuses []string
	Limit    int
}
