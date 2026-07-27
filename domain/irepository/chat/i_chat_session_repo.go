package chat

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/chat"
	"github.com/good-fish-man/agent-runtime-client/pkg/query"
)

type IChatSessionRepo interface {
	Create(ctx context.Context, session *entity.ChatSession) (ulid string, err error)
	CreateWithId(ctx context.Context, session *entity.ChatSession, ulid string) error
	Delete(ctx context.Context, ulid string) error
	Update(ctx context.Context, session *entity.ChatSession) error
	UpdateStatus(ctx context.Context, ulid string, status string) error
	FindById(ctx context.Context, ulid string) (*entity.ChatSession, error)
	FindByUserId(ctx context.Context, userId string, status string) ([]*entity.ChatSession, error)
	FindPage(ctx context.Context, queries []*query.Query, reqPage *query.PageData, reqSort *query.SortData) ([]*entity.ChatSession, *query.PageData, error)
	FindRecent(ctx context.Context, limit int) ([]*entity.ChatSession, error)
	CountByChannel(ctx context.Context) (map[string]int, error)
	FindByUserIdAndChannel(ctx context.Context, userId, channel string) (*entity.ChatSession, error)
}

type IChatMessageRepo interface {
	Create(ctx context.Context, message *entity.ChatMessage) (ulid string, err error)
	Update(ctx context.Context, message *entity.ChatMessage) error
	UpdateStatus(ctx context.Context, ulid string, status string) error
	FindById(ctx context.Context, ulid string) (*entity.ChatMessage, error)
	FindBySessionId(ctx context.Context, sessionId string) ([]*entity.ChatMessage, error)
	DeleteBySessionId(ctx context.Context, sessionId string) error
}

type IChatApprovalRepo interface {
	Create(ctx context.Context, approval *entity.ChatApproval) (ulid string, err error)
	Update(ctx context.Context, approval *entity.ChatApproval) error
	UpdateStatus(ctx context.Context, ulid string, status, approvedBy string, reason string) error
	FindById(ctx context.Context, ulid string) (*entity.ChatApproval, error)
	FindByMessageId(ctx context.Context, messageId string) (*entity.ChatApproval, error)
	FindPending(ctx context.Context) ([]*entity.ChatApproval, error)
	FindByUserId(ctx context.Context, userId string) ([]*entity.ChatApproval, error)
}

type IChatTokenStatsRepo interface {
	Create(ctx context.Context, stats *entity.ChatTokenStats) (ulid string, err error)
	Update(ctx context.Context, stats *entity.ChatTokenStats) error
	FindOrCreate(ctx context.Context, agentId, userId, date, model string) (*entity.ChatTokenStats, error)
	AddTokens(ctx context.Context, agentId, userId, date, model string, input, output int) error
	GetTotalTokens(ctx context.Context) (int, error)
	GetTokenRanking(ctx context.Context, limit int) ([]*TokenRankingItem, error)
}

type TokenRankingItem struct {
	AgentId     string
	AgentName   string
	TotalTokens int
}
