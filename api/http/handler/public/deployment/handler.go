package deployment

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	service "github.com/good-fish-man/agent-runtime-client/application/service/deployment"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/deployment"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type Handler struct{ service *service.Service }

func NewHandler(value *service.Service) *Handler { return &Handler{service: value} }

func (h *Handler) CreateBuild(c *gin.Context) {
	var request service.CreateBuildRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreateBuild(c.Request.Context(), userID(c), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListBuilds(c *gin.Context) {
	filter := entity.BuildFilter{AgentID: c.Query("agent_id"), Limit: queryInt(c, "limit", 50), Offset: queryInt(c, "offset", 0)}
	items, total, err := h.service.ListBuilds(c.Request.Context(), userID(c), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items, "total": total})
}

func (h *Handler) FindBuild(c *gin.Context) {
	value, err := h.service.FindBuild(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if value == nil {
		_ = c.Error(apierror.ErrNotFound.WithMessage("agent build not found"))
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ProposePromotion(c *gin.Context) {
	var request service.CreatePromotionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.ProposePromotion(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListPromotions(c *gin.Context) {
	filter := entity.PromotionFilter{AgentID: c.Query("agent_id"), Status: c.Query("status"), Limit: queryInt(c, "limit", 50), Offset: queryInt(c, "offset", 0)}
	items, total, err := h.service.ListPromotions(c.Request.Context(), userID(c), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items, "total": total})
}

func (h *Handler) TransitionPromotion(c *gin.Context) {
	var request service.TransitionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.TransitionPromotion(c.Request.Context(), userID(c), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) RecordShadow(c *gin.Context) {
	var request service.ShadowRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.RecordShadow(c.Request.Context(), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListShadow(c *gin.Context) {
	items, err := h.service.ListShadowResults(c.Request.Context(), userID(c), c.Param("id"), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) RecordMetric(c *gin.Context) {
	var request service.CanaryMetricRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	metric, promotion, err := h.service.RecordCanaryMetric(c.Request.Context(), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, gin.H{"metric": metric, "promotion": promotion})
}

func (h *Handler) ListMetrics(c *gin.Context) {
	items, err := h.service.ListCanaryMetrics(c.Request.Context(), userID(c), c.Param("id"), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) Rollback(c *gin.Context) {
	var request service.RollbackRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	rollback, compensations, err := h.service.Rollback(c.Request.Context(), userID(c), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"rollback": rollback, "compensations": compensations})
}

func (h *Handler) SetOptOut(c *gin.Context) {
	var request struct {
		AgentID  string `json:"agent_id"`
		OptedOut bool   `json:"opted_out"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.AgentID == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("agent_id is required"))
		return
	}
	if err := h.service.SetExperimentOptOut(c.Request.Context(), userID(c), request.AgentID, request.OptedOut); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"agent_id": request.AgentID, "opted_out": request.OptedOut})
}

func (h *Handler) GetExperiment(c *gin.Context) {
	agentID := c.Query("agent_id")
	if agentID == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("agent_id is required"))
		return
	}
	exposure, err := h.service.Exposure(c.Request.Context(), userID(c), agentID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"exposure": exposure})
}

func (h *Handler) ListManifests(c *gin.Context) {
	items, err := h.service.ListRunManifests(c.Request.Context(), userID(c), c.Query("agent_id"), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) ListRollbacks(c *gin.Context) {
	items, err := h.service.ListRollbacks(c.Request.Context(), userID(c), c.Query("agent_id"), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func userID(c *gin.Context) string { return c.GetString(consts.CtxKeyUserID) }

func queryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.DefaultQuery(key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}
