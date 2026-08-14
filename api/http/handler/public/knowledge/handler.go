package knowledge

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	service "github.com/good-fish-man/agent-runtime-client/application/service/knowledge"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type Handler struct{ service *service.Service }

func NewHandler(value *service.Service) *Handler { return &Handler{service: value} }

func (h *Handler) CreateUserEvidence(c *gin.Context) {
	var request service.CreateUserEvidenceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreateUserEvidence(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) CreateClaim(c *gin.Context) {
	var request service.CreateClaimRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	claim, contradictions, err := h.service.CreateClaim(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, gin.H{"claim": claim, "contradictions": contradictions})
}

func (h *Handler) ListClaims(c *gin.Context) {
	items, err := h.service.ListClaims(c.Request.Context(), userID(c), queryInt(c, "limit", 100))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) ListEvidence(c *gin.Context) {
	items, err := h.service.ListEvidence(c.Request.Context(), userID(c), queryInt(c, "limit", 100))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) Retrieve(c *gin.Context) {
	var query entity.RetrievalQuery
	if err := c.ShouldBindJSON(&query); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.Retrieve(c.Request.Context(), userID(c), query)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ListContradictions(c *gin.Context) {
	items, err := h.service.ListContradictions(c.Request.Context(), userID(c), c.DefaultQuery("unresolved", "true") != "false", queryInt(c, "limit", 100))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) ResolveContradiction(c *gin.Context) {
	var request service.ResolveContradictionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.ResolveContradiction(c.Request.Context(), userID(c), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ListSnapshots(c *gin.Context) {
	items, err := h.service.ListSnapshots(c.Request.Context(), userID(c), queryInt(c, "limit", 100))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) CreateOntologyPack(c *gin.Context) {
	var request service.CreateOntologyPackRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreateOntologyPack(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListOntologyPacks(c *gin.Context) {
	items, err := h.service.ListOntologyPacks(c.Request.Context(), userID(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) CreateOntologyCandidate(c *gin.Context) {
	var request service.CreateOntologyCandidateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreateOntologyCandidate(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListOntologyCandidates(c *gin.Context) {
	items, err := h.service.ListOntologyCandidates(c.Request.Context(), userID(c), queryInt(c, "limit", 100))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) ReviewOntologyCandidate(c *gin.Context) {
	var request service.ReviewOntologyCandidateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.ReviewOntologyCandidate(c.Request.Context(), userID(c), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) CreateOntologyMigration(c *gin.Context) {
	var request service.CreateOntologyMigrationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreateOntologyMigration(c.Request.Context(), userID(c), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ReviewOntologyMigration(c *gin.Context) {
	var request service.ReviewOntologyMigrationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.ReviewOntologyMigration(c.Request.Context(), userID(c), userID(c), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ExecuteOntologyMigration(c *gin.Context) {
	value, err := h.service.ExecuteOntologyMigration(c.Request.Context(), userID(c), c.Param("id"))
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
