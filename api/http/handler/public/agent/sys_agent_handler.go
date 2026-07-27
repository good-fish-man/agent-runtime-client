package agent

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/agent"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// CreateSysAgent handles POST /agent.
func (h *Handler) CreateSysAgent(c *gin.Context) {
	var req dto.CreateSysAgentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.CreatedBy = c.GetString("user_id")

	res, err := h.svc.CreateSysAgent(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}

// DeleteSysAgent handles DELETE /agent/:ulid.
func (h *Handler) DeleteSysAgent(c *gin.Context) {
	var req dto.DelSysAgentReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString("user_id")
	if err := h.svc.DeleteSysAgent(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// UpdateSysAgent handles PUT /agent/:ulid.
func (h *Handler) UpdateSysAgent(c *gin.Context) {
	var req dto.UpdateSysAgentReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UpdatedBy = c.GetString("user_id")
	req.UserID = req.UpdatedBy

	if err := h.svc.UpdateSysAgent(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// FindSysAgentById handles GET /agent/:ulid.
func (h *Handler) FindSysAgentById(c *gin.Context) {
	var req dto.FindSysAgentByIdReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString("user_id")
	res, err := h.svc.FindSysAgentById(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysAgentAll handles POST /agent/all.
func (h *Handler) FindSysAgentAll(c *gin.Context) {
	var req dto.FindSysAgentAllReq
	_ = c.ShouldBindJSON(&req)
	req.UserID = c.GetString("user_id")

	res, err := h.svc.FindSysAgentAll(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysAgentPage handles POST /agent/page.
func (h *Handler) FindSysAgentPage(c *gin.Context) {
	var req dto.FindSysAgentPageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString("user_id")
	res, err := h.svc.FindSysAgentPage(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// UploadSysAgent handles POST /agent/upload.
func (h *Handler) UploadSysAgent(c *gin.Context) {
	var req dto.UploadSysAgentReq
	if fileHeader, err := c.FormFile("file"); err == nil {
		file, err := fileHeader.Open()
		if err != nil {
			_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
			return
		}
		defer file.Close()
		if err := json.NewDecoder(file).Decode(&req); err != nil {
			_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
			return
		}
	} else if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.CreatedBy = c.GetString("user_id")

	res, err := h.svc.UploadSysAgent(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}

// UpdateSysAgentEnabled handles PUT /agent/:ulid/enabled.
func (h *Handler) UpdateSysAgentEnabled(c *gin.Context) {
	var req dto.UpdateSysAgentEnabledReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString("user_id")
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := h.svc.UpdateSysAgentEnabled(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}
