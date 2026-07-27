package model

import (
	"net/http"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

func (h *Handler) CreateModelKey(c *gin.Context) {
	var req dto.CreateModelKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString(consts.CtxKeyUserID)
	result, err := h.svc.CreateModelKey(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, result)
}

func (h *Handler) UpdateModelKey(c *gin.Context) {
	var req dto.UpdateModelKeyReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString(consts.CtxKeyUserID)
	if err := h.svc.UpdateModelKey(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

func (h *Handler) DeleteModelKey(c *gin.Context) {
	if err := h.svc.DeleteModelKey(c.Request.Context(), c.Param("ulid"), c.GetString(consts.CtxKeyUserID)); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

func (h *Handler) FindModelKeys(c *gin.Context) {
	result, err := h.svc.FindModelKeys(c.Request.Context(), c.GetString(consts.CtxKeyUserID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, result)
}
