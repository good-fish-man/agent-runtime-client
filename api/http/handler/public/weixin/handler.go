// Package weixin provides a skeleton for the WeChat scan-login endpoints. The
// full WeChat/websocket login flow from agent-frame is intentionally not ported;
// these handlers keep the route contract and return a not-implemented envelope.
package weixin

import (
	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/types/apierror"
)

// Handler serves the weixin login endpoints.
type Handler struct{}

// NewHandler builds the weixin login handler.
func NewHandler() *Handler { return &Handler{} }

// Login handles GET /weixin/login — returns 501 (scan-login not yet supported).
func (h *Handler) Login(c *gin.Context) {
	_ = c.Error(apierror.ErrNotImplemented.WithMessage("weixin scan-login not yet implemented"))
}

// Status handles GET /weixin/login/status — returns 501.
func (h *Handler) Status(c *gin.Context) {
	_ = c.Error(apierror.ErrNotImplemented.WithMessage("weixin login status not yet implemented"))
}
