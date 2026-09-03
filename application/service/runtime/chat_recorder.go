package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	controlentity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	log "github.com/good-fish-man/logx"
)

type streamCapture struct {
	content   strings.Builder
	done      *entity.DoneEvent
	errEvent  *entity.ErrorEvent
	approvals []entity.PendingApproval
	research  map[string]any
}

func newStreamCapture() *streamCapture { return &streamCapture{} }

func (c *streamCapture) Wrap(next StreamFunc) StreamFunc {
	return func(event *entity.StreamEvent) error {
		c.capture(event)
		return next(event)
	}
}

func (c *streamCapture) capture(event *entity.StreamEvent) {
	if event == nil {
		return
	}
	switch event.Type {
	case entity.StreamTypeDelta:
		if event.Delta != nil && !event.Delta.Reasoning {
			c.content.WriteString(event.Delta.Text)
		}
	case entity.StreamTypeInterrupted:
		if event.Interrupted != nil {
			c.approvals = append([]entity.PendingApproval(nil), event.Interrupted.PendingApprovals...)
		}
	case entity.StreamTypeProgress:
		if snapshot := researchSnapshotFromProgress(event.Progress); snapshot != nil {
			c.research = snapshot
		}
	case entity.StreamTypeError:
		c.errEvent = event.Error
	case entity.StreamTypeDone:
		c.done = event.Done
		if event.Done != nil && strings.TrimSpace(event.Done.Content) != "" {
			c.content.Reset()
			c.content.WriteString(event.Done.Content)
		}
	}
}

type chatRecorder struct {
	data *data.Data
}

type chatRecordInput struct {
	UserID        string
	AgentID       string
	SessionID     string
	Channel       string
	Prompt        string
	AssistantText string
	ModelID       string
	Model         string
	PromptTokens  int
	OutputTokens  int
	TotalTokens   int
	LatencyMs     int
	TraceID       string
	Status        string
	ErrorMessage  string
	Metadata      map[string]any
	Approvals     []entity.PendingApproval
	ModelUsage    []entity.ModelUsageMetadata
}

func newChatRecorder(data *data.Data) *chatRecorder {
	if data == nil {
		return nil
	}
	return &chatRecorder{data: data}
}

func (r *chatRecorder) Record(ctx context.Context, input chatRecordInput) {
	if r == nil || r.data == nil {
		return
	}
	input.UserID = firstNonEmpty(input.UserID, authctx.UserID(ctx))
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.UserID == "" || input.SessionID == "" {
		return
	}
	if input.Channel == "" {
		input.Channel = "web"
	}
	if input.Status == "" {
		input.Status = "success"
	}
	if err := r.record(ctx, input); err != nil {
		log.Warnw(ctx, "record chat history failed", "session_id", input.SessionID, "error", err)
	}
}

func (s *RuntimeService) recordCompletion(ctx context.Context, values map[string]any, models map[string]entity.ModelConfig, prompt, content string, metadata *entity.ResponseMetadata, approvals []entity.PendingApproval, runErr error) {
	if s == nil || s.chat == nil {
		return
	}
	status := "success"
	errMessage := ""
	if runErr != nil {
		status = "failed"
		errMessage = runErr.Error()
	}
	if len(approvals) > 0 && status == "success" {
		status = "pending_approval"
	}
	s.chat.Record(context.WithoutCancel(ctx), chatRecordInput{
		UserID: authctx.UserID(ctx), AgentID: contextString(values, "agent_id"), SessionID: contextString(values, "session_id"), Channel: contextString(values, "channel"),
		Prompt: prompt, AssistantText: content, TraceID: traceID(ctx), Status: status, ErrorMessage: errMessage, Approvals: approvals,
		ModelID: recordingModelID(models), Model: recordingModelName(metadata, models), PromptTokens: metadataPromptTokens(metadata), OutputTokens: metadataCompletionTokens(metadata), TotalTokens: metadataTotalTokens(metadata), LatencyMs: metadataLatency(metadata),
		Metadata: metadataMap(metadata), ModelUsage: metadataModelUsage(metadata),
	})
}

func (s *RuntimeService) recordStream(ctx context.Context, values map[string]any, models map[string]entity.ModelConfig, prompt string, capture *streamCapture, runErr error) {
	if s == nil || capture == nil {
		return
	}
	if s.knowledge != nil && len(capture.research) > 0 {
		taskID := firstNonEmpty(contextString(values, "task_id"), firstNonEmpty(contextString(values, "request_id"), contextString(values, "run_manifest_id")))
		if err := s.knowledge.CaptureResearchEvidence(context.WithoutCancel(ctx), authctx.UserID(ctx), taskID, traceID(ctx), capture.research); err != nil {
			log.Warnw(ctx, "persist research evidence failed", "error", err)
		}
	}
	if s.chat == nil {
		return
	}
	status := "success"
	errMessage := ""
	if runErr != nil {
		status = "failed"
		errMessage = runErr.Error()
	}
	if capture.errEvent != nil {
		status = "failed"
		errMessage = strings.TrimSpace(capture.errEvent.Message)
	}
	if len(capture.approvals) > 0 && status == "success" {
		status = "pending_approval"
	}
	var metadata *entity.ResponseMetadata
	if capture.done != nil {
		metadata = capture.done.Metadata
	}
	if metadata == nil && capture.done != nil {
		metadata = &entity.ResponseMetadata{PromptTokens: capture.done.PromptTokens, CompletionTokens: capture.done.CompletionTokens, TokensUsed: capture.done.TotalTokens}
	}
	recordMetadata := metadataMap(metadata)
	if len(capture.research) > 0 {
		if recordMetadata == nil {
			recordMetadata = make(map[string]any)
		}
		recordMetadata["research"] = capture.research
	}
	s.chat.Record(context.WithoutCancel(ctx), chatRecordInput{
		UserID: authctx.UserID(ctx), AgentID: contextString(values, "agent_id"), SessionID: contextString(values, "session_id"), Channel: contextString(values, "channel"),
		Prompt: prompt, AssistantText: capture.content.String(), TraceID: traceID(ctx), Status: status, ErrorMessage: errMessage, Approvals: capture.approvals,
		ModelID: recordingModelID(models), Model: recordingModelName(metadata, models), PromptTokens: metadataPromptTokens(metadata), OutputTokens: metadataCompletionTokens(metadata), TotalTokens: metadataTotalTokens(metadata), LatencyMs: metadataLatency(metadata),
		Metadata: recordMetadata, ModelUsage: metadataModelUsage(metadata),
	})
}

func (r *chatRecorder) record(ctx context.Context, input chatRecordInput) error {
	db := r.data.DB(ctx)
	session := chatpo.ChatSession{}
	lookup := db.Where("ulid = ?", input.SessionID).Limit(1).Find(&session)
	if lookup.Error != nil {
		return lookup.Error
	}
	if lookup.RowsAffected == 0 {
		session = chatpo.ChatSession{Ulid: input.SessionID, UserId: input.UserID, AgentId: input.AgentID, Title: titleFromPrompt(input.Prompt), Status: "active", Channel: input.Channel}
		if err := db.Create(&session).Error; err != nil {
			return err
		}
	} else {
		updates := map[string]any{"updated_at": time.Now().UnixMilli()}
		if session.AgentId == "" && input.AgentID != "" {
			updates["agent_id"] = input.AgentID
		}
		if session.Title == "" && input.Prompt != "" {
			updates["title"] = titleFromPrompt(input.Prompt)
		}
		if session.Channel == "" && input.Channel != "" {
			updates["channel"] = input.Channel
		}
		if len(updates) > 1 {
			if err := db.Model(&chatpo.ChatSession{}).Where("ulid = ?", input.SessionID).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	now := time.Now().UnixMilli()
	if strings.TrimSpace(input.Prompt) != "" {
		if err := db.Create(&chatpo.ChatMessage{
			SessionId: input.SessionID, CreatedAt: now, Role: "user", Content: input.Prompt, Status: "success",
			Trace: jsonField(map[string]any{"trace_id": input.TraceID}),
		}).Error; err != nil {
			return err
		}
	}
	assistantStatus := input.Status
	if len(input.Approvals) > 0 && assistantStatus == "success" {
		assistantStatus = "pending_approval"
	}
	assistant := &chatpo.ChatMessage{
		SessionId: input.SessionID, CreatedAt: now + 1, Role: "assistant", Content: input.AssistantText, ModelID: input.ModelID, Model: input.Model,
		InputTokens: input.PromptTokens, OutputTokens: input.OutputTokens, TotalTokens: input.TotalTokens, LatencyMs: input.LatencyMs,
		Trace: jsonField(map[string]any{"trace_id": input.TraceID}), Status: assistantStatus, ErrorMsg: input.ErrorMessage, Metadata: jsonField(input.Metadata),
	}
	if err := db.Create(assistant).Error; err != nil {
		return err
	}
	for _, approval := range input.Approvals {
		params := approval.ArgumentsJSON
		if strings.TrimSpace(params) == "" {
			params = "{}"
		}
		item := chatpo.ChatApproval{UserId: input.UserID, AgentId: input.AgentID, MessageId: assistant.Ulid, ToolName: approval.ToolName, RiskLevel: approval.RiskLevel, Parameters: params, Status: "pending"}
		if err := db.Create(&item).Error; err != nil {
			return err
		}
	}
	for _, usage := range normalizedModelUsage(input) {
		stats := chatpo.ChatTokenStats{
			SessionId: input.SessionID, AgentId: input.AgentID, UserId: input.UserID, Date: chatpo.GetDateKey(),
			ModelID: usage.ModelID, Model: usage.Model, InputTokens: int(usage.PromptTokens),
			OutputTokens: int(usage.CompletionTokens), TotalTokens: int(usage.TotalTokens), RequestCount: int(usage.RequestCount),
		}
		if err := db.Create(&stats).Error; err != nil {
			return err
		}
	}
	return nil
}

func normalizedModelUsage(input chatRecordInput) []entity.ModelUsageMetadata {
	usageRecords := append([]entity.ModelUsageMetadata(nil), input.ModelUsage...)
	if len(usageRecords) == 0 && (input.TotalTokens > 0 || input.PromptTokens > 0 || input.OutputTokens > 0) {
		usageRecords = []entity.ModelUsageMetadata{{
			ModelID: input.ModelID, Model: input.Model, PromptTokens: int32(input.PromptTokens),
			CompletionTokens: int32(input.OutputTokens), TotalTokens: int32(input.TotalTokens), RequestCount: 1,
		}}
	}
	for i := range usageRecords {
		usageRecords[i].ModelID = strings.TrimSpace(usageRecords[i].ModelID)
		usageRecords[i].Model = strings.TrimSpace(usageRecords[i].Model)
		if usageRecords[i].ModelID == "" && len(usageRecords) == 1 {
			usageRecords[i].ModelID = strings.TrimSpace(input.ModelID)
		}
		if usageRecords[i].Model == "" && len(usageRecords) == 1 {
			usageRecords[i].Model = strings.TrimSpace(input.Model)
		}
		if usageRecords[i].RequestCount <= 0 {
			usageRecords[i].RequestCount = 1
		}
		if usageRecords[i].TotalTokens <= 0 {
			usageRecords[i].TotalTokens = usageRecords[i].PromptTokens + usageRecords[i].CompletionTokens
		}
	}
	return usageRecords
}

func researchSnapshotFromProgress(progress *controlentity.Progress) map[string]any {
	if progress == nil || progress.Capability != "research.execute" || len(progress.State) == 0 {
		return nil
	}
	pages, ok := progress.State["valuable_pages"].([]any)
	if !ok || len(pages) == 0 {
		return nil
	}
	return map[string]any{
		"queryTexts":  progress.State["query_texts"],
		"pages":       pages,
		"confidence":  progress.State["confidence"],
		"queryCount":  progress.State["queries"],
		"sourceCount": progress.State["sources"],
	}
}

func titleFromPrompt(prompt string) string {
	title := strings.TrimSpace(prompt)
	if title == "" {
		return "New conversation"
	}
	runes := []rune(title)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return title
}

func jsonField(value any) chatpo.StringJSON {
	if value == nil {
		return chatpo.StringJSON{}
	}
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return chatpo.StringJSON{}
	}
	return chatpo.StringJSON{Val: string(data)}
}

func contextString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func metadataModel(metadata *entity.ResponseMetadata) string {
	if metadata == nil {
		return ""
	}
	return metadata.Model
}

func recordingModelID(models map[string]entity.ModelConfig) string {
	return modelID(models["default"])
}

func recordingModelName(metadata *entity.ResponseMetadata, models map[string]entity.ModelConfig) string {
	if name := metadataModel(metadata); name != "" {
		return name
	}
	return strings.TrimSpace(models["default"].Name)
}

func metadataPromptTokens(metadata *entity.ResponseMetadata) int {
	if metadata == nil {
		return 0
	}
	return int(metadata.PromptTokens)
}

func metadataCompletionTokens(metadata *entity.ResponseMetadata) int {
	if metadata == nil {
		return 0
	}
	return int(metadata.CompletionTokens)
}

func metadataTotalTokens(metadata *entity.ResponseMetadata) int {
	if metadata == nil {
		return 0
	}
	if metadata.TokensUsed > 0 {
		return int(metadata.TokensUsed)
	}
	return int(metadata.PromptTokens + metadata.CompletionTokens)
}

func metadataLatency(metadata *entity.ResponseMetadata) int {
	if metadata == nil {
		return 0
	}
	return int(metadata.LatencyMs)
}

func metadataModelUsage(metadata *entity.ResponseMetadata) []entity.ModelUsageMetadata {
	if metadata == nil || len(metadata.ModelUsage) == 0 {
		return nil
	}
	return append([]entity.ModelUsageMetadata(nil), metadata.ModelUsage...)
}

func metadataMap(metadata *entity.ResponseMetadata) map[string]any {
	if metadata == nil {
		return nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("marshal metadata: %v", err)}
	}
	out := make(map[string]any)
	_ = json.Unmarshal(data, &out)
	return out
}
