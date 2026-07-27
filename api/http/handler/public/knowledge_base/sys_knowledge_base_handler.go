package knowledge_base

import (
	"net/http"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// CreateSysKnowledgeBase handles POST /knowledge_base.
func (h *Handler) CreateSysKnowledgeBase(c *gin.Context) {
	var req dto.CreateSysKnowledgeBaseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.CreatedBy = c.GetString("user_id")

	res, err := h.svc.CreateSysKnowledgeBase(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}

// DeleteSysKnowledgeBase handles DELETE /knowledge_base/:ulid.
func (h *Handler) DeleteSysKnowledgeBase(c *gin.Context) {
	var req dto.DelSysKnowledgeBaseReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := h.svc.DeleteSysKnowledgeBase(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// UpdateSysKnowledgeBase handles PUT /knowledge_base/:ulid.
func (h *Handler) UpdateSysKnowledgeBase(c *gin.Context) {
	var req dto.UpdateSysKnowledgeBaseReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UpdatedBy = c.GetString("user_id")

	if err := h.svc.UpdateSysKnowledgeBase(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// FindSysKnowledgeBaseById handles GET /knowledge_base/:ulid.
func (h *Handler) FindSysKnowledgeBaseById(c *gin.Context) {
	var req dto.FindSysKnowledgeBaseByIdReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysKnowledgeBaseById(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysKnowledgeBaseAll handles POST /knowledge_base/all.
func (h *Handler) FindSysKnowledgeBaseAll(c *gin.Context) {
	var req dto.FindSysKnowledgeBaseAllReq
	_ = c.ShouldBindJSON(&req)

	res, err := h.svc.FindSysKnowledgeBaseAll(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysKnowledgeBasePage handles POST /knowledge_base/page.
func (h *Handler) FindSysKnowledgeBasePage(c *gin.Context) {
	var req dto.FindSysKnowledgeBasePageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysKnowledgeBasePage(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// RecallTest handles POST /knowledge_base/:ulid/recall.
func (h *Handler) RecallTest(c *gin.Context) {
	var uriReq dto.FindSysKnowledgeBaseByIdReq
	if err := c.ShouldBindUri(&uriReq); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	var req dto.RecallTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}

	res, err := h.svc.RecallTest(c.Request.Context(), uriReq.Ulid, &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}
