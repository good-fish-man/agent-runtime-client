package dashboard

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	agentpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/agent"
	channelpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/channel"
	chatpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/chat"
	jobpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/job"
	kbpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

func (h *Handler) Overview(c *gin.Context) {
	ctx := c.Request.Context()
	db := h.data.DB(ctx)

	var activeAgents int64
	var periodicAgents int64
	var completedTasks int64
	var activeKnowledgeSources int64
	var totalTokens int64

	_ = db.Model(&agentpo.SysAgent{}).Where("deleted_at = 0 AND enabled = ?", true).Count(&activeAgents).Error
	_ = db.Model(&agentpo.SysAgent{}).Where("deleted_at = 0 AND enabled = ? AND is_periodic = ?", true, true).Count(&periodicAgents).Error
	_ = db.Model(&jobpo.JobExecutionPO{}).Where("deleted_at = 0 AND status = ?", jobpo.JobStatusSuccess).Count(&completedTasks).Error
	_ = db.Model(&kbpo.SysKnowledgeBase{}).Where("deleted_at = 0 AND enabled = ?", true).Count(&activeKnowledgeSources).Error
	_ = db.Model(&chatpo.ChatTokenStats{}).Select("COALESCE(SUM(total_tokens), 0)").Scan(&totalTokens).Error

	response.Ok(c, gin.H{
		"active_agents":            activeAgents,
		"periodic_agents":          periodicAgents,
		"tasks_completed":          completedTasks,
		"total_tokens":             totalTokens,
		"active_knowledge_sources": activeKnowledgeSources,
	})
}

func (h *Handler) TokenRanking(c *gin.Context) {
	limit := parseLimit(c, 10)
	type row struct {
		AgentID     string `json:"agent_id"`
		AgentName   string `json:"agent_name"`
		TotalTokens int64  `json:"total_tokens"`
	}
	rows := make([]row, 0)
	err := h.data.DB(c.Request.Context()).
		Table("chat_token_stats AS s").
		Select("s.agent_id, COALESCE(a.name, s.agent_id) AS agent_name, SUM(s.total_tokens) AS total_tokens").
		Joins("LEFT JOIN sys_agent AS a ON a.ulid = s.agent_id").
		Group("s.agent_id, a.name").
		Order("total_tokens DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": []row{}})
		return
	}
	response.Ok(c, gin.H{"rankings": rows})
}

func (h *Handler) ChannelActivity(c *gin.Context) {
	type row struct {
		ChannelID    string `json:"channel_id"`
		ChannelName  string `json:"channel_name"`
		Status       string `json:"status"`
		MessageCount int64  `json:"message_count"`
	}
	rows := make([]row, 0)
	err := h.data.DB(c.Request.Context()).
		Model(&channelpo.SysChannel{}).
		Select("ulid AS channel_id, name AS channel_name, CASE WHEN enabled THEN 'active' ELSE 'inactive' END AS status, 0 AS message_count").
		Where("deleted_at = 0").
		Order("sort ASC, created_at DESC").
		Scan(&rows).Error
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"channels": []row{}}})
		return
	}
	response.Ok(c, gin.H{"channels": rows})
}

func (h *Handler) RecentSessions(c *gin.Context) {
	limit := parseLimit(c, 10)
	rows := make([]chatpo.ChatSession, 0)
	err := h.data.DB(c.Request.Context()).
		Model(&chatpo.ChatSession{}).
		Where("deleted_at = 0").
		Order("updated_at DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"sessions": []chatpo.ChatSession{}}})
		return
	}
	response.Ok(c, gin.H{"sessions": rows})
}

func parseLimit(c *gin.Context, fallback int) int {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(fallback)))
	if err != nil || limit <= 0 {
		return fallback
	}
	if limit > 100 {
		return 100
	}
	return limit
}
