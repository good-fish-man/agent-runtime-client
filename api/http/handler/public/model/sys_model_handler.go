package model

import (
	"net/http"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/model"
	modelentity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// CreateSysModel handles POST /model.
func (h *Handler) CreateSysModel(c *gin.Context) {
	var req dto.CreateSysModelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.CreatedBy = c.GetString("user_id")

	res, err := h.svc.CreateSysModel(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}

// DeleteSysModel handles DELETE /model/:ulid.
func (h *Handler) DeleteSysModel(c *gin.Context) {
	var req dto.DelSysModelReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString("user_id")
	if err := h.svc.DeleteSysModel(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// UpdateSysModel handles PUT /model/:ulid.
func (h *Handler) UpdateSysModel(c *gin.Context) {
	var req dto.UpdateSysModelReq
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

	if err := h.svc.UpdateSysModel(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

func (h *Handler) UpdateSysModelEnabled(c *gin.Context) {
	if c.GetUint("admin_level") == 0 {
		_ = c.Error(apierror.ErrForbidden.WithMessage("仅管理员可以启用或停用模型"))
		return
	}
	var req dto.UpdateSysModelEnabledReq
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
	if err := h.svc.UpdateSysModelEnabled(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	model, err := h.svc.FindSysModelAdminByID(c.Request.Context(), req.Ulid)
	if err == nil && model != nil {
		mode := model.RuntimeMode
		if !*req.Enabled {
			mode = modelentity.RuntimeModeOff
		} else if mode == "" {
			mode = modelentity.RuntimeModeOnDemand
		}
		h.applyRuntimeMode(model.Provider, model.Name, mode)
	}
	response.Ok(c, nil)
}

func (h *Handler) UpdateSysModelRuntimeMode(c *gin.Context) {
	if c.GetUint("admin_level") == 0 {
		_ = c.Error(apierror.ErrForbidden.WithMessage("仅管理员可以设置本地模型运行模式"))
		return
	}
	var req dto.UpdateSysModelRuntimeModeReq
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
	if err := h.svc.UpdateSysModelRuntimeMode(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	model, err := h.svc.FindSysModelAdminByID(c.Request.Context(), req.Ulid)
	if err == nil && model != nil {
		h.applyRuntimeMode(model.Provider, model.Name, req.RuntimeMode)
	}
	response.Ok(c, nil)
}

func (h *Handler) FindSysModelAdminAll(c *gin.Context) {
	if c.GetUint("admin_level") == 0 {
		_ = c.Error(apierror.ErrForbidden.WithMessage("仅管理员可以查看全部模型"))
		return
	}
	res, err := h.svc.FindSysModelAdminAll(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysModelById handles GET /model/:ulid.
func (h *Handler) FindSysModelById(c *gin.Context) {
	var req dto.FindSysModelByIdReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString("user_id")
	res, err := h.svc.FindSysModelById(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysModelAll handles POST /model/all.
func (h *Handler) FindSysModelAll(c *gin.Context) {
	var req dto.FindSysModelAllReq
	_ = c.ShouldBindJSON(&req) // body optional
	req.UserID = c.GetString("user_id")

	res, err := h.svc.FindSysModelAll(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindModelCatalog handles GET /model/catalog.
func (h *Handler) FindModelCatalog(c *gin.Context) {
	var req dto.FindModelCatalogReq
	if err := c.ShouldBindQuery(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}

	res, err := h.svc.FindModelCatalog(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysModelPage handles POST /model/page.
func (h *Handler) FindSysModelPage(c *gin.Context) {
	var req dto.FindSysModelPageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.UserID = c.GetString("user_id")
	res, err := h.svc.FindSysModelPage(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}
