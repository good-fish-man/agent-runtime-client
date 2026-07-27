// Package agent contains the Gin handlers for SysAgent CRUD endpoints.
package agent

import service "github.com/good-fish-man/agent-runtime-client/application/service/agent"

// Handler groups the SysAgent HTTP handlers.
type Handler struct {
	svc *service.SysAgentService
}

// NewHandler builds the handler with its application service.
func NewHandler(svc *service.SysAgentService) *Handler {
	return &Handler{svc: svc}
}
