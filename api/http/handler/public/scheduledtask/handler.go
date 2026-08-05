package scheduledtask

import (
	"github.com/gin-gonic/gin"
	service "github.com/good-fish-man/agent-runtime-client/application/service/scheduledtask"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
	"net/http"
)

type Handler struct{ svc *service.Service }

func NewHandler(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) CreateInternal(c *gin.Context) {
	if !service.InternalTokenValid(c.GetHeader(consts.HeaderAthenaInternalToken)) {
		_ = c.Error(apierror.ErrUnauthorized)
		return
	}
	var req service.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	item, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, item)
}
func (h *Handler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), c.GetString(consts.CtxKeyUserID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, items)
}
func (h *Handler) Update(c *gin.Context) {
	var req service.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.GetString(consts.CtxKeyUserID), c.Param("ulid"), req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.GetString(consts.CtxKeyUserID), c.Param("ulid")); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}
func (h *Handler) ListApprovals(c *gin.Context) {
	items, err := h.svc.ListApprovals(c.Request.Context(), c.GetString(consts.CtxKeyUserID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, items)
}
func (h *Handler) DecideApproval(c *gin.Context) {
	var req service.ApprovalDecision
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := h.svc.DecideApproval(c.Request.Context(), c.GetString(consts.CtxKeyUserID), c.Param("ulid"), req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"interactive_completion_required": req.Approved})
}
