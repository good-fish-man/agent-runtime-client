package user

import (
	"net/http"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

func (h *Handler) Register(c *gin.Context) {
	var req dto.RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.Register(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}

func (h *Handler) Login(c *gin.Context) {
	var req dto.LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.Login(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

func (h *Handler) Me(c *gin.Context) {
	res, err := h.svc.FindSysUserById(c.Request.Context(), &dto.FindSysUserByIdReq{Ulid: c.GetString("user_id")})
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

func (h *Handler) Logout(c *gin.Context) {
	if err := h.svc.Logout(c.Request.Context(), c.GetString("auth_token_hash")); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// CreateSysUser handles POST /user/user.
func (h *Handler) CreateSysUser(c *gin.Context) {
	var req dto.CreateSysUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.CreatedBy = c.GetString("user_id")

	res, err := h.svc.CreateSysUser(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, res)
}

// DeleteSysUser handles DELETE /user/user/:ulid.
func (h *Handler) DeleteSysUser(c *gin.Context) {
	var req dto.DelSysUsersReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	req.DeletedBy = c.GetString("user_id")

	if err := h.svc.DeleteSysUser(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// UpdateSysUser handles PUT /user/user/:ulid.
func (h *Handler) UpdateSysUser(c *gin.Context) {
	var req dto.UpdateSysUserReq
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

	if err := h.svc.UpdateSysUser(c.Request.Context(), &req); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

// FindSysUserById handles GET /user/user/:ulid.
func (h *Handler) FindSysUserById(c *gin.Context) {
	var req dto.FindSysUserByIdReq
	if err := c.ShouldBindUri(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysUserById(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysUserByQuery handles POST /user/user/byQuery.
func (h *Handler) FindSysUserByQuery(c *gin.Context) {
	var req dto.FindSysUserByQueryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysUserByQuery(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysUserAll handles POST /user/user/byAll.
func (h *Handler) FindSysUserAll(c *gin.Context) {
	var req dto.FindSysUserAllReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysUserAll(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// FindSysUserPage handles POST /user/userPage.
func (h *Handler) FindSysUserPage(c *gin.Context) {
	var req dto.FindSysUserPageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.FindSysUserPage(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}
