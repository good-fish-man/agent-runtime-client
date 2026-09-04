package worldmodel

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	service "github.com/good-fish-man/agent-runtime-client/application/service/worldmodel"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type Handler struct{ service *service.Service }

func NewHandler(value *service.Service) *Handler { return &Handler{service: value} }

func (h *Handler) Query(c *gin.Context) {
	var query entity.WorldQuery
	if err := c.ShouldBindJSON(&query); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.Query(c.Request.Context(), authctx.UserID(c.Request.Context()), query)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) Conflicts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	status := strings.TrimSpace(c.DefaultQuery("status", "OPEN"))
	items, err := h.service.Conflicts(c.Request.Context(), authctx.UserID(c.Request.Context()), status, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) ResolveConflict(c *gin.Context) {
	var request struct {
		Resolution       string `json:"resolution"`
		ExpectedRevision int64  `json:"expected_revision"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.ResolveConflict(c.Request.Context(), authctx.UserID(c.Request.Context()), c.Param("id"), request.Resolution, request.ExpectedRevision)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusOK, value)
}

func (h *Handler) CreateProvider(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var request entity.WorldProviderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreateProvider(c.Request.Context(), authctx.UserID(c.Request.Context()), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) UpdateProvider(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var request entity.WorldProviderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.UpdateProvider(c.Request.Context(), authctx.UserID(c.Request.Context()), c.Param("id"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) ListProviders(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	items, err := h.service.ListProviders(c.Request.Context(), authctx.UserID(c.Request.Context()))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) DeleteProvider(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	if err := h.service.DeleteProvider(c.Request.Context(), authctx.UserID(c.Request.Context()), c.Param("id")); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) TestProvider(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	value, err := h.service.TestProvider(c.Request.Context(), authctx.UserID(c.Request.Context()), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) QueryProvider(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var query entity.WorldQuery
	if err := c.ShouldBindJSON(&query); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.QueryProvider(c.Request.Context(), authctx.UserID(c.Request.Context()), c.Param("id"), query)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) OntologyContext(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	value, err := h.service.OntologyContext(c.Request.Context(), authctx.UserID(c.Request.Context()))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func requireAdmin(c *gin.Context) bool {
	if c.GetInt(consts.CtxKeyAdminLevel) > 0 || c.GetUint(consts.CtxKeyAdminLevel) > 0 {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{"error": "administrator access is required"})
	return false
}
