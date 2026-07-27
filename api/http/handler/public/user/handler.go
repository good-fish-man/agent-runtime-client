// Package user contains the Gin handlers for SysUser CRUD endpoints.
package user

import (
	"path/filepath"
	"strings"

	service "github.com/good-fish-man/agent-runtime-client/application/service/user"
)

// Handler groups the SysUser HTTP handlers.
type Handler struct {
	svc             *service.SysUserService
	avatarDir       string
	avatarURLPrefix string
}

// NewHandler builds the handler with its application service.
func NewHandler(svc *service.SysUserService) *Handler {
	h := &Handler{svc: svc}
	return h.WithAvatarStorage("", "/api/agent-runtime-client/v1")
}

// WithAvatarStorage configures persistent avatar files and their public URL prefix.
func (h *Handler) WithAvatarStorage(uploadsDir, publicPrefix string) *Handler {
	if strings.TrimSpace(uploadsDir) == "" {
		uploadsDir = filepath.Join("data", "uploads")
	}
	if absolute, err := filepath.Abs(uploadsDir); err == nil {
		uploadsDir = absolute
	}
	h.avatarDir = filepath.Join(uploadsDir, "avatars")
	h.avatarURLPrefix = strings.TrimRight(publicPrefix, "/") + "/auth/avatar"
	return h
}
