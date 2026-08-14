package pluginregistry

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	service "github.com/good-fish-man/agent-runtime-client/application/service/pluginregistry"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type Handler struct{ service *service.Service }

func NewHandler(value *service.Service) *Handler { return &Handler{service: value} }

func (h *Handler) List(c *gin.Context) {
	items, err := h.service.List(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) Install(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 12<<20)
	var request service.InstallRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.Install(c.Request.Context(), c.GetString(consts.CtxKeyUserID), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) Transition(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var request service.StatusRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.Transition(c.Request.Context(), c.GetString(consts.CtxKeyUserID), c.Param("provider"), c.Param("version"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) Review(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var request service.ReviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.Review(c.Request.Context(), c.GetString(consts.CtxKeyUserID), c.Param("provider"), c.Param("version"), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) Audit(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.service.Audit(c.Request.Context(), limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) Reload(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	value, err := h.service.Reload(c.Request.Context())
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
	_ = c.Error(apierror.ErrForbidden.WithMessage("administrator access is required"))
	return false
}
