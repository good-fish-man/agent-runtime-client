package control

import protocolv4 "github.com/good-fish-man/athena-protocol/protocol/v4"

const (
	Protocol                  = protocolv4.Protocol
	AttachmentEncodingBase64  = protocolv4.AttachmentEncodingBase64
	MaxAttachmentBytes        = protocolv4.MaxAttachmentBytes
	MaxObservationAttachments = protocolv4.MaxObservationAttachments

	TypeHello        = protocolv4.TypeHello
	TypeWelcome      = protocolv4.TypeWelcome
	TypeHeartbeat    = protocolv4.TypeHeartbeat
	TypeHeartbeatAck = protocolv4.TypeHeartbeatAck
	TypeAction       = protocolv4.TypeAction
	TypeObservation  = protocolv4.TypeObservation
	TypeProgress     = protocolv4.TypeProgress
	TypeCancel       = protocolv4.TypeCancel
	TypeError        = protocolv4.TypeError
	TypeTaskEvent    = protocolv4.TypeTaskEvent

	RiskReadOnly      = protocolv4.RiskReadOnly
	RiskReversible    = protocolv4.RiskReversible
	RiskExternalWrite = protocolv4.RiskExternalWrite
	RiskSensitive     = protocolv4.RiskSensitive
	RiskLow           = protocolv4.RiskLow
	RiskMedium        = protocolv4.RiskMedium
	RiskHigh          = protocolv4.RiskHigh
	Allow             = protocolv4.Allow
	AskUser           = protocolv4.AskUser
	Block             = protocolv4.Block
	ApprovalPending   = protocolv4.ApprovalPending
	ApprovalApproved  = protocolv4.ApprovalApproved
	ApprovalRejected  = protocolv4.ApprovalRejected
	ApprovalExpired   = protocolv4.ApprovalExpired

	StatusUnderstanding   = protocolv4.StatusUnderstanding
	StatusPlanning        = protocolv4.StatusPlanning
	StatusWaitingAction   = protocolv4.StatusWaitingAction
	StatusWaitingApproval = protocolv4.StatusWaitingApproval
	StatusExecuting       = protocolv4.StatusExecuting
	StatusObserving       = protocolv4.StatusObserving
	StatusEvaluating      = protocolv4.StatusEvaluating
	StatusWaitingUser     = protocolv4.StatusWaitingUser
	StatusCompleted       = protocolv4.StatusCompleted
	StatusFailed          = protocolv4.StatusFailed
	StatusCancelled       = protocolv4.StatusCancelled

	TaskStatusCreated            = protocolv4.TaskStatusCreated
	TaskStatusPlanning           = protocolv4.TaskStatusPlanning
	TaskStatusRunning            = protocolv4.TaskStatusRunning
	TaskStatusWaitingObservation = protocolv4.TaskStatusWaitingObservation
	TaskStatusWaitingApproval    = protocolv4.TaskStatusWaitingApproval
	TaskStatusWaitingUser        = protocolv4.TaskStatusWaitingUser
	TaskStatusVerifying          = protocolv4.TaskStatusVerifying
	TaskStatusRetryWait          = protocolv4.TaskStatusRetryWait
	TaskStatusPaused             = protocolv4.TaskStatusPaused
	TaskStatusCancelling         = protocolv4.TaskStatusCancelling
	TaskStatusCompleted          = protocolv4.TaskStatusCompleted
	TaskStatusFailed             = protocolv4.TaskStatusFailed
	TaskStatusCancelled          = protocolv4.TaskStatusCancelled

	StepStatusPending            = protocolv4.StepStatusPending
	StepStatusReady              = protocolv4.StepStatusReady
	StepStatusRunning            = protocolv4.StepStatusRunning
	StepStatusWaitingObservation = protocolv4.StepStatusWaitingObservation
	StepStatusWaitingApproval    = protocolv4.StepStatusWaitingApproval
	StepStatusWaitingUser        = protocolv4.StepStatusWaitingUser
	StepStatusVerifying          = protocolv4.StepStatusVerifying
	StepStatusPaused             = protocolv4.StepStatusPaused
	StepStatusCompleted          = protocolv4.StepStatusCompleted
	StepStatusFailed             = protocolv4.StepStatusFailed
	StepStatusCancelled          = protocolv4.StepStatusCancelled

	EventTaskCreated         = protocolv4.EventTaskCreated
	EventTaskStatusChanged   = protocolv4.EventTaskStatusChanged
	EventStepCreated         = protocolv4.EventStepCreated
	EventStepStatusChanged   = protocolv4.EventStepStatusChanged
	EventActionRequested     = protocolv4.EventActionRequested
	EventActionDispatched    = protocolv4.EventActionDispatched
	EventActionProgressed    = protocolv4.EventActionProgressed
	EventApprovalRequested   = protocolv4.EventApprovalRequested
	EventApprovalDecided     = protocolv4.EventApprovalDecided
	EventObservationReceived = protocolv4.EventObservationReceived
	EventWorldPatched        = protocolv4.EventWorldPatched
	EventTaskCancelled       = protocolv4.EventTaskCancelled

	ObservationSucceeded       = protocolv4.ObservationSucceeded
	ObservationFailed          = protocolv4.ObservationFailed
	ObservationCancelled       = protocolv4.ObservationCancelled
	ObservationExpired         = protocolv4.ObservationExpired
	ObservationBlocked         = protocolv4.ObservationBlocked
	ObservationWaitingApproval = protocolv4.ObservationWaitingApproval
	ObservationWaitingUser     = protocolv4.ObservationWaitingUser
)

type Policy = protocolv4.Policy
type Attachment = protocolv4.Attachment
type EvidenceRef = protocolv4.EvidenceRef
type ErrorDetail = protocolv4.ErrorDetail
type ExpectedObservation = protocolv4.ExpectedObservation
type WorldMutation = protocolv4.WorldMutation
type WorldPatch = protocolv4.WorldPatch
type WorldState = protocolv4.WorldState
type Action = protocolv4.Action
type Observation = protocolv4.Observation
type Progress = protocolv4.Progress
type Cancel = protocolv4.Cancel
type CapabilityInstance = protocolv4.CapabilityInstance
type DeviceMessage = protocolv4.DeviceMessage
type RegisteredDevice = protocolv4.RegisteredDevice
type RetryPolicy = protocolv4.RetryPolicy
type TaskStep = protocolv4.TaskStep
type TaskSession = protocolv4.TaskSession
type Approval = protocolv4.Approval
type Artifact = protocolv4.Artifact
type EventEnvelope = protocolv4.EventEnvelope

var (
	ValidTaskStatus         = protocolv4.ValidTaskStatus
	ValidStepStatus         = protocolv4.ValidStepStatus
	TerminalTaskStatus      = protocolv4.TerminalTaskStatus
	CanTransitionTaskStatus = protocolv4.CanTransitionTaskStatus
	NewCancel               = protocolv4.NewCancel
	ActionFromMap           = protocolv4.ActionFromMap
	DecodeStrict            = protocolv4.DecodeStrict
	NewID                   = protocolv4.NewID
)
