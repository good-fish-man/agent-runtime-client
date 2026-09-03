package learning

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	service "github.com/good-fish-man/agent-runtime-client/application/service/learning"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/learning"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type Handler struct {
	service   *service.Service
	evolution *service.EvolutionOrchestrator
}

func NewHandler(value *service.Service) *Handler { return &Handler{service: value} }

func (h *Handler) WithEvolution(value *service.EvolutionOrchestrator) *Handler {
	h.evolution = value
	return h
}

func (h *Handler) EvolutionStatus(c *gin.Context) {
	if h.evolution == nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("evolution orchestrator is not configured"))
		return
	}
	response.Ok(c, h.evolution.Snapshot())
}

func (h *Handler) ScanEvolution(c *gin.Context) {
	if h.evolution == nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("evolution orchestrator is not configured"))
		return
	}
	result, err := h.evolution.ScanOwner(c.Request.Context(), userID(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, result)
}

func (h *Handler) GenerateCandidate(c *gin.Context) {
	var request service.GenerateCandidateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.GenerateCandidate(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListCandidates(c *gin.Context) {
	filter := entity.CandidateFilter{
		Kind: c.Query("kind"), Status: c.Query("status"),
		Limit: queryInt(c, "limit", 50), Offset: queryInt(c, "offset", 0),
	}
	items, total, err := h.service.ListCandidates(c.Request.Context(), userID(c), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items, "total": total, "limit": filter.Limit, "offset": filter.Offset})
}

func (h *Handler) FindCandidate(c *gin.Context) {
	candidate, evidence, evaluations, err := h.service.FindCandidate(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if candidate == nil {
		_ = c.Error(apierror.ErrNotFound.WithMessage("learning candidate not found"))
		return
	}
	response.Ok(c, gin.H{"candidate": candidate, "evidence": evidence, "evaluations": evaluations})
}

func (h *Handler) ReviewCandidate(c *gin.Context) {
	var request service.ReviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.ReviewCandidate(c.Request.Context(), userID(c), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) UpdateCandidate(c *gin.Context) {
	var request service.UpdateCandidateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.UpdateCandidate(c.Request.Context(), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ReevaluateCandidate(c *gin.Context) {
	var request service.ReevaluateCandidateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.ReevaluateCandidate(c.Request.Context(), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ListSkills(c *gin.Context) {
	items, err := h.service.ListSkills(c.Request.Context(), userID(c), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items, "activation": "manual_only"})
}

func (h *Handler) ListStrategies(c *gin.Context) {
	items, err := h.service.ListStrategies(c.Request.Context(), userID(c), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items, "activation": "manual_only"})
}

func (h *Handler) StartDemonstration(c *gin.Context) {
	var request service.StartDemonstrationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.StartDemonstration(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListDemonstrations(c *gin.Context) {
	items, err := h.service.ListDemonstrations(c.Request.Context(), userID(c), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) RecordDemonstrationStep(c *gin.Context) {
	var request service.RecordDemonstrationStepRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.RecordDemonstrationStep(c.Request.Context(), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ResumeDemonstration(c *gin.Context) {
	h.transitionDemonstration(c, h.service.ResumeDemonstration)
}
func (h *Handler) PreviewDemonstration(c *gin.Context) {
	h.transitionDemonstration(c, h.service.PreviewDemonstration)
}
func (h *Handler) DiscardDemonstration(c *gin.Context) {
	h.transitionDemonstration(c, h.service.DiscardDemonstration)
}

func (h *Handler) EditDemonstration(c *gin.Context) {
	var request service.EditDemonstrationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.EditDemonstration(c.Request.Context(), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ConfirmDemonstration(c *gin.Context) {
	value, err := h.service.ConfirmDemonstration(c.Request.Context(), userID(c), userID(c), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) transitionDemonstration(c *gin.Context, transition func(context.Context, string, string) (*entity.Demonstration, error)) {
	value, err := transition(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func userID(c *gin.Context) string { return c.GetString(consts.CtxKeyUserID) }

func queryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.DefaultQuery(key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}
