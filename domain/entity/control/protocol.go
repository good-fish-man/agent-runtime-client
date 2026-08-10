package control

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const Protocol = "athena.agent.v3"

const (
	AttachmentEncodingBase64  = "base64"
	MaxAttachmentBytes        = 4 << 20
	MaxObservationAttachments = 2
)

const (
	TypeHello        = "HELLO"
	TypeWelcome      = "WELCOME"
	TypeHeartbeat    = "HEARTBEAT"
	TypeHeartbeatAck = "HEARTBEAT_ACK"
	TypeAction       = "ACTION"
	TypeObservation  = "OBSERVATION"
	TypeProgress     = "PROGRESS"
	TypeCancel       = "CANCEL"
	TypeError        = "ERROR"
)

const (
	RiskLow    = "LOW"
	RiskMedium = "MEDIUM"
	RiskHigh   = "HIGH"
	Allow      = "ALLOW"
	AskUser    = "ASK_USER"
	Block      = "BLOCK"

	StatusUnderstanding   = "UNDERSTANDING"
	StatusPlanning        = "PLANNING"
	StatusWaitingAction   = "WAITING_ACTION"
	StatusWaitingApproval = "WAITING_APPROVAL"
	StatusExecuting       = "EXECUTING"
	StatusObserving       = "OBSERVING"
	StatusEvaluating      = "EVALUATING"
	StatusWaitingUser     = "WAITING_USER"
	StatusCompleted       = "COMPLETED"
	StatusFailed          = "FAILED"
	StatusCancelled       = "CANCELLED"

	ObservationSucceeded       = "SUCCEEDED"
	ObservationFailed          = "FAILED"
	ObservationCancelled       = "CANCELLED"
	ObservationExpired         = "EXPIRED"
	ObservationBlocked         = "BLOCKED"
	ObservationWaitingApproval = "WAITING_APPROVAL"
	ObservationWaitingUser     = "WAITING_USER"
)

type Policy struct {
	Risk     string `json:"risk"`
	Decision string `json:"decision"`
}

// Attachment is transient device evidence. Data is consumed by the current
// model turn and is intentionally excluded from persistence and UI events.
type Attachment struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	MIMEType string `json:"mime_type"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Encoding string `json:"encoding"`
	Data     string `json:"data,omitempty"`
	Purpose  string `json:"purpose,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type Action struct {
	Protocol       string         `json:"protocol"`
	Type           string         `json:"type"`
	TaskID         string         `json:"task_id"`
	ActionID       string         `json:"action_id"`
	SessionID      string         `json:"session_id,omitempty"`
	Sequence       int64          `json:"sequence"`
	IdempotencyKey string         `json:"idempotency_key"`
	Deadline       time.Time      `json:"deadline"`
	Capability     string         `json:"capability"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	Policy         Policy         `json:"policy"`
}

type Observation struct {
	Protocol    string         `json:"protocol"`
	Type        string         `json:"type"`
	TaskID      string         `json:"task_id"`
	ActionID    string         `json:"action_id"`
	SessionID   string         `json:"session_id,omitempty"`
	Sequence    int64          `json:"sequence"`
	Status      string         `json:"status"`
	ObservedAt  time.Time      `json:"observed_at"`
	State       map[string]any `json:"state,omitempty"`
	Attachments []Attachment   `json:"attachments,omitempty"`
	Error       string         `json:"error,omitempty"`
}

type Progress struct {
	Protocol   string         `json:"protocol"`
	Type       string         `json:"type"`
	TaskID     string         `json:"task_id"`
	ActionID   string         `json:"action_id"`
	SessionID  string         `json:"session_id,omitempty"`
	Sequence   int64          `json:"sequence"`
	Capability string         `json:"capability,omitempty"`
	Stage      string         `json:"stage,omitempty"`
	Message    string         `json:"message,omitempty"`
	Progress   int            `json:"progress,omitempty"`
	Bytes      int64          `json:"bytes,omitempty"`
	Total      int64          `json:"total,omitempty"`
	State      map[string]any `json:"state,omitempty"`
	SentAt     time.Time      `json:"sent_at"`
}

type Cancel struct {
	Protocol string `json:"protocol"`
	Type     string `json:"type"`
	TaskID   string `json:"task_id"`
	ActionID string `json:"action_id"`
	Sequence int64  `json:"sequence"`
	Reason   string `json:"reason,omitempty"`
}

type DeviceMessage struct {
	Protocol     string    `json:"protocol"`
	Type         string    `json:"type"`
	DeviceID     string    `json:"device_id,omitempty"`
	Name         string    `json:"name,omitempty"`
	Platform     string    `json:"platform,omitempty"`
	Architecture string    `json:"architecture,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	SentAt       time.Time `json:"sent_at,omitempty"`
}

type RegisteredDevice struct {
	DeviceID     string    `json:"device_id"`
	UserID       string    `json:"user_id,omitempty"`
	Name         string    `json:"name"`
	Platform     string    `json:"platform"`
	Architecture string    `json:"architecture"`
	Capabilities []string  `json:"capabilities"`
	Online       bool      `json:"online"`
	ConnectedAt  time.Time `json:"connected_at,omitempty"`
	LastSeenAt   time.Time `json:"last_seen_at,omitempty"`
}

type TaskSession struct {
	TaskID         string                 `json:"task_id"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	UserID         string                 `json:"user_id"`
	DeviceID       string                 `json:"device_id"`
	Status         string                 `json:"status"`
	Sequence       int64                  `json:"sequence"`
	ActiveSessions map[string]string      `json:"active_sessions,omitempty"`
	Actions        []Action               `json:"actions,omitempty"`
	Observations   []Observation          `json:"observations,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

func (a *Action) Normalize() {
	if a.Arguments == nil {
		a.Arguments = map[string]any{}
	}
}

func (a Action) Validate() error {
	if a.Protocol != Protocol || a.Type != TypeAction {
		return fmt.Errorf("invalid action protocol or type")
	}
	if a.TaskID == "" || a.ActionID == "" || a.IdempotencyKey == "" || a.Capability == "" || a.Sequence <= 0 {
		return fmt.Errorf("task_id, action_id, positive sequence, idempotency_key, and capability are required")
	}
	if a.Deadline.IsZero() {
		return fmt.Errorf("deadline is required")
	}
	if a.Policy.Risk != RiskLow && a.Policy.Risk != RiskMedium && a.Policy.Risk != RiskHigh {
		return fmt.Errorf("unsupported risk %q", a.Policy.Risk)
	}
	if a.Policy.Decision != Allow && a.Policy.Decision != AskUser && a.Policy.Decision != Block {
		return fmt.Errorf("unsupported policy decision %q", a.Policy.Decision)
	}
	return nil
}

func (o Observation) Validate() error {
	if o.Protocol != Protocol || o.Type != TypeObservation {
		return fmt.Errorf("invalid observation protocol or type")
	}
	if o.TaskID == "" || o.ActionID == "" || o.Sequence <= 0 || o.ObservedAt.IsZero() {
		return fmt.Errorf("task_id, action_id, positive sequence, and observed_at are required")
	}
	if len(o.Attachments) > MaxObservationAttachments {
		return fmt.Errorf("observation has too many attachments")
	}
	attachmentIDs := make(map[string]bool, len(o.Attachments))
	for _, attachment := range o.Attachments {
		if attachmentIDs[attachment.ID] {
			return fmt.Errorf("duplicate attachment id %q", attachment.ID)
		}
		attachmentIDs[attachment.ID] = true
		if err := attachment.Validate(); err != nil {
			return err
		}
	}
	switch o.Status {
	case ObservationSucceeded, ObservationFailed, ObservationCancelled, ObservationExpired, ObservationBlocked, ObservationWaitingApproval, ObservationWaitingUser:
		return nil
	default:
		return fmt.Errorf("unsupported observation status %q", o.Status)
	}
}

func (a Attachment) Validate() error {
	if a.ID == "" || a.Kind != "image" || a.SHA256 == "" {
		return fmt.Errorf("attachment id, image kind, and sha256 are required")
	}
	if a.MIMEType != "image/png" && a.MIMEType != "image/jpeg" && a.MIMEType != "image/webp" {
		return fmt.Errorf("unsupported attachment MIME type %q", a.MIMEType)
	}
	if a.Encoding != AttachmentEncodingBase64 {
		return fmt.Errorf("unsupported attachment encoding %q", a.Encoding)
	}
	if a.Size <= 0 || a.Size > MaxAttachmentBytes {
		return fmt.Errorf("attachment size must be between 1 and %d bytes", MaxAttachmentBytes)
	}
	if a.Data == "" {
		return fmt.Errorf("attachment data is required")
	}
	if len(a.Data) > base64.StdEncoding.EncodedLen(MaxAttachmentBytes) {
		return fmt.Errorf("attachment %s encoded payload is too large", a.ID)
	}
	decoded, err := base64.StdEncoding.DecodeString(a.Data)
	if err != nil {
		return fmt.Errorf("decode attachment %s: %w", a.ID, err)
	}
	if int64(len(decoded)) != a.Size {
		return fmt.Errorf("attachment %s size does not match payload", a.ID)
	}
	digest := sha256.Sum256(decoded)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), a.SHA256) {
		return fmt.Errorf("attachment %s sha256 does not match payload", a.ID)
	}
	return nil
}

// WithoutAttachmentData returns an observation safe for persistence, logs,
// task history, and frontend stream events.
func (o Observation) WithoutAttachmentData() Observation {
	copy := o
	copy.Attachments = make([]Attachment, len(o.Attachments))
	for i, attachment := range o.Attachments {
		attachment.Data = ""
		copy.Attachments[i] = attachment
	}
	return copy
}

func (p Progress) Validate() error {
	if p.Protocol != Protocol || p.Type != TypeProgress {
		return fmt.Errorf("invalid progress protocol or type")
	}
	if p.TaskID == "" || p.ActionID == "" || p.Sequence <= 0 || p.SentAt.IsZero() {
		return fmt.Errorf("task_id, action_id, positive sequence, and sent_at are required")
	}
	if p.Progress < 0 || p.Progress > 100 {
		return fmt.Errorf("progress must be between 0 and 100")
	}
	return nil
}

func ValidTaskStatus(status string) bool {
	switch status {
	case StatusUnderstanding, StatusPlanning, StatusWaitingAction, StatusWaitingApproval, StatusExecuting, StatusObserving, StatusEvaluating, StatusWaitingUser, StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func TerminalTaskStatus(status string) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTaskStatus keeps the first terminal outcome authoritative.
// Repeated writes of the same terminal state remain idempotent.
func CanTransitionTaskStatus(current, next string) bool {
	if !ValidTaskStatus(next) {
		return false
	}
	return current == "" || current == next || !TerminalTaskStatus(current)
}

func NewCancel(action Action, reason string) Cancel {
	return Cancel{Protocol: Protocol, Type: TypeCancel, TaskID: action.TaskID, ActionID: action.ActionID, Sequence: action.Sequence, Reason: reason}
}

func ActionFromMap(value map[string]any) (Action, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return Action{}, fmt.Errorf("encode action payload: %w", err)
	}
	var action Action
	if err := json.Unmarshal(data, &action); err != nil {
		return Action{}, fmt.Errorf("decode action payload: %w", err)
	}
	if action.Protocol != Protocol || action.Type != TypeAction {
		return Action{}, fmt.Errorf("unsupported action protocol")
	}
	if err := action.Validate(); err != nil {
		return Action{}, err
	}
	return action, nil
}
