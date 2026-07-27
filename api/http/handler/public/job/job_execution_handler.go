package job

import (
	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/job"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// FindJobExecutionById handles GET /job/execution/:ulid.
func (h *Handler) FindJobExecutionById(c *gin.Context) {
	var req dto.FindJobExecutionByIdReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindJobExecutionById(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindJobExecutionByAgentId handles GET /job/execution/byAgentId.
func (h *Handler) FindJobExecutionByAgentId(c *gin.Context) {
	var req dto.FindJobExecutionByAgentIdReq
	if err := c.ShouldBindQuery(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindJobExecutionByAgentId(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindJobExecutionPage handles POST /job/execution/page.
func (h *Handler) FindJobExecutionPage(c *gin.Context) {
	var req dto.FindJobExecutionPageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindJobExecutionPage(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}
