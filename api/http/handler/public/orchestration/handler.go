package orchestration

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/api/http/middleware"
	service "github.com/good-fish-man/agent-runtime-client/application/service/orchestration"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type Handler struct{ service *service.Service }

func NewHandler(value *service.Service) *Handler { return &Handler{service: value} }

func (h *Handler) CreateInternal(c *gin.Context) {
	if !middleware.InternalTokenValid(c.GetHeader(consts.HeaderAthenaInternalToken)) {
		_ = c.Error(apierror.ErrUnauthorized)
		return
	}
	var request struct {
		UserID string `json:"user_id"`
		service.CreatePlannedGoalRequest
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreatePlannedGoal(c.Request.Context(), strings.TrimSpace(request.UserID), request.CreatePlannedGoalRequest)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) CreateGoal(c *gin.Context) {
	var request service.CreateGoalRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreateGoal(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListGoals(c *gin.Context) {
	statuses := split(c.Query("status"))
	items, err := h.service.ListGoals(c.Request.Context(), userID(c), statuses, queryInt(c, "limit", 100))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) ListScheduleTriggers(c *gin.Context) {
	items, err := h.service.ListScheduleTriggers(c.Request.Context(), userID(c), strings.TrimSpace(c.Query("schedule_id")), queryInt(c, "limit", 100))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) GetGoal(c *gin.Context) {
	value, err := h.service.GetGoal(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) PlanGoal(c *gin.Context) {
	var request service.PlanGoalRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.PlanGoal(c.Request.Context(), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) StartTask(c *gin.Context) {
	var request service.StartTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	route, state, err := h.service.StartTask(c.Request.Context(), userID(c), c.Param("id"), c.Param("taskID"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"route": route, "state": state})
}

func (h *Handler) RecordResult(c *gin.Context) {
	var request service.RecordResultRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.RecordResult(c.Request.Context(), userID(c), c.Param("id"), c.Param("taskID"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) SaveCheckpoint(c *gin.Context) {
	var request service.CheckpointRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.SaveCheckpoint(c.Request.Context(), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) Pause(c *gin.Context) {
	var request struct {
		Reason           string `json:"reason"`
		ExpectedRevision int64  `json:"expected_revision"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.Pause(c.Request.Context(), userID(c), c.Param("id"), request.Reason, request.ExpectedRevision)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) Resume(c *gin.Context) {
	var request struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.Resume(c.Request.Context(), userID(c), c.Param("id"), request.ExpectedRevision)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ListCheckpoints(c *gin.Context) {
	items, err := h.service.ListCheckpoints(c.Request.Context(), userID(c), c.Param("id"), queryInt(c, "limit", 100))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) WorldSlice(c *gin.Context) {
	value, err := h.service.WorldSlice(c.Request.Context(), userID(c), c.Param("id"), c.Param("taskID"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"world_slices_by_device": value})
}

func userID(c *gin.Context) string { return c.GetString(consts.CtxKeyUserID) }
func split(value string) []string {
	values := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}
func queryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.DefaultQuery(key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}
