package channel

import (
	"net/http"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/channel"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// CreateSysChannel handles POST /channel.
func (h *Handler) CreateSysChannel(c *gin.Context) {
	var req dto.CreateSysChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.CreatedBy = c.GetString("user_id")

	res, err := h.svc.CreateSysChannel(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}

// DeleteSysChannel handles DELETE /channel/:ulid.
func (h *Handler) DeleteSysChannel(c *gin.Context) {
	var req dto.DelSysChannelReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := h.svc.DeleteSysChannel(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// UpdateSysChannel handles PUT /channel/:ulid.
func (h *Handler) UpdateSysChannel(c *gin.Context) {
	var req dto.UpdateSysChannelReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UpdatedBy = c.GetString("user_id")

	if err := h.svc.UpdateSysChannel(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// FindSysChannelById handles GET /channel/:ulid.
func (h *Handler) FindSysChannelById(c *gin.Context) {
	var req dto.FindSysChannelByIdReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysChannelById(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysChannelAll handles POST /channel/all.
func (h *Handler) FindSysChannelAll(c *gin.Context) {
	var req dto.FindSysChannelAllReq
	_ = c.ShouldBindJSON(&req) // body optional

	res, err := h.svc.FindSysChannelAll(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysChannelPage handles POST /channel/page.
func (h *Handler) FindSysChannelPage(c *gin.Context) {
	var req dto.FindSysChannelPageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysChannelPage(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}
