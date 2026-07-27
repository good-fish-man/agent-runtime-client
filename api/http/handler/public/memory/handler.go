package memory

import (
	"net/http"

	"github.com/gin-gonic/gin"

	service "github.com/good-fish-man/agent-runtime-client/application/service/memory"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type Handler struct{ svc *service.Service }

func NewHandler(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Create(c *gin.Context) {
	var req service.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	item, err := h.svc.Create(c.Request.Context(), c.GetString(consts.CtxKeyUserID), req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, item)
}

func (h *Handler) List(c *gin.Context) {
	var req service.ListReq
	_ = c.ShouldBindJSON(&req)
	items, err := h.svc.List(c.Request.Context(), c.GetString(consts.CtxKeyUserID), req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, items)
}

func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.GetString(consts.CtxKeyUserID), c.Param("ulid")); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}
