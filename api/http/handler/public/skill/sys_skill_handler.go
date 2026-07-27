package skill

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/skill"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// CreateSysSkill handles POST /skill.
func (h *Handler) CreateSysSkill(c *gin.Context) {
	var req dto.CreateSysSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.CreatedBy = c.GetString("user_id")

	res, err := h.svc.CreateSysSkill(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}

// DeleteSysSkill handles DELETE /skill/:ulid.
func (h *Handler) DeleteSysSkill(c *gin.Context) {
	var req dto.DelSysSkillReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := h.svc.DeleteSysSkill(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// UpdateSysSkill handles PUT /skill/:ulid.
func (h *Handler) UpdateSysSkill(c *gin.Context) {
	var req dto.UpdateSysSkillReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UpdatedBy = c.GetString("user_id")

	if err := h.svc.UpdateSysSkill(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// FindSysSkillById handles GET /skill/:ulid.
func (h *Handler) FindSysSkillById(c *gin.Context) {
	var req dto.FindSysSkillByIdReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysSkillById(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysSkillAll handles POST /skill/all.
func (h *Handler) FindSysSkillAll(c *gin.Context) {
	var req dto.FindSysSkillAllReq
	_ = c.ShouldBindJSON(&req)

	res, err := h.svc.FindSysSkillAll(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysSkillPage handles POST /skill/page.
func (h *Handler) FindSysSkillPage(c *gin.Context) {
	var req dto.FindSysSkillPageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysSkillPage(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// CheckSkillName handles POST /skill/check-name.
func (h *Handler) CheckSkillName(c *gin.Context) {
	var req dto.CheckSkillNameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.CheckSkillName(c.Request.Context(), req.Name)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// UploadSysSkill handles POST /skill/upload.
func (h *Handler) UploadSysSkill(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("failed to get uploaded file"))
		return
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("failed to read uploaded file"))
		return
	}

	res, err := h.svc.UploadSysSkill(c.Request.Context(), fileData, header.Filename, c.GetString("user_id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}
