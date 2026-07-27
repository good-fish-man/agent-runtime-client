// Package channel contains the Gin handlers for SysChannel CRUD endpoints.
package channel

import service "github.com/good-fish-man/agent-runtime-client/application/service/channel"

// Handler groups the SysChannel HTTP handlers.
type Handler struct {
	svc *service.SysChannelService
}

// NewHandler builds the handler with its application service.
func NewHandler(svc *service.SysChannelService) *Handler {
	return &Handler{svc: svc}
}
