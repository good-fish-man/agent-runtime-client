package memory

import (
	"context"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime-client/infra/data"
	po "github.com/good-fish-man/agent-runtime-client/infra/repository/po/memory"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

type Service struct{ data *data.Data }

type CreateReq struct {
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MemoryType  string `json:"memory_type"`
	Content     string `json:"content"`
	Importance  int32  `json:"importance"`
}

type ListReq struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Limit     int    `json:"limit"`
}

type ExportBundle struct {
	Schema     string            `json:"schema"`
	OwnerID    string            `json:"owner_id"`
	ExportedAt time.Time         `json:"exported_at"`
	Items      []*po.AgentMemory `json:"items"`
}

func NewService(d *data.Data) *Service { return &Service{data: d} }

func (s *Service) Create(ctx context.Context, userID string, req CreateReq) (*po.AgentMemory, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("memory content is required")
	}
	if req.MemoryType == "" {
		req.MemoryType = "semantic"
	}
	if req.Importance <= 0 {
		req.Importance = 1
	}
	item := &po.AgentMemory{UserID: userID, AgentID: req.AgentID, SessionID: req.SessionID, Name: req.Name, Description: req.Description, MemoryType: req.MemoryType, Content: req.Content, Importance: req.Importance, Enabled: true}
	if err := s.data.DB(ctx).Create(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, userID string, req ListReq) ([]*po.AgentMemory, error) {
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items := make([]*po.AgentMemory, 0)
	db := s.data.DB(ctx).Where("user_id = ? AND deleted_at = 0 AND enabled = ?", userID, true)
	if req.AgentID != "" {
		db = db.Where("agent_id = ?", req.AgentID)
	}
	if req.SessionID != "" {
		db = db.Where("session_id = ?", req.SessionID)
	}
	err := db.Order("importance desc, updated_at desc").Limit(limit).Find(&items).Error
	return items, err
}

func (s *Service) Delete(ctx context.Context, userID, id string) error {
	result := s.data.DB(ctx).Model(&po.AgentMemory{}).Where("ulid = ? AND user_id = ? AND deleted_at = 0", id, userID).Updates(map[string]any{"deleted_at": time.Now().UnixMilli(), "enabled": false})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apierror.ErrNotFound.WithMessage("memory not found")
	}
	return nil
}

func (s *Service) Export(ctx context.Context, userID string) (*ExportBundle, error) {
	items := make([]*po.AgentMemory, 0)
	if err := s.data.DB(ctx).Where("user_id = ? AND deleted_at = 0", userID).Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return &ExportBundle{Schema: "athena.privacy-export.v1", OwnerID: userID, ExportedAt: time.Now().UTC(), Items: items}, nil
}

func (s *Service) DeleteAll(ctx context.Context, userID string) (int64, error) {
	result := s.data.DB(ctx).Model(&po.AgentMemory{}).
		Where("user_id = ? AND deleted_at = 0", userID).
		Updates(map[string]any{"deleted_at": time.Now().UnixMilli(), "enabled": false, "content": "", "name": "", "description": ""})
	return result.RowsAffected, result.Error
}

// ContextText returns compact user-owned memories for prompt context injection.
func (s *Service) ContextText(ctx context.Context, userID, agentID string, limit int) (string, error) {
	items, err := s.List(ctx, userID, ListReq{AgentID: agentID, Limit: limit})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, item := range items {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		if item.Name != "" {
			b.WriteString("- " + item.Name + ": ")
		} else {
			b.WriteString("- ")
		}
		b.WriteString(item.Content)
	}
	return b.String(), nil
}

func (s *Service) StoreExtracted(ctx context.Context, userID, agentID, sessionID string, entries []CreateReq) error {
	for _, entry := range entries {
		entry.AgentID = agentID
		entry.SessionID = sessionID
		if _, err := s.Create(ctx, userID, entry); err != nil {
			return err
		}
	}
	return nil
}
