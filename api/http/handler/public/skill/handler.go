// Package skill contains the Gin handlers for SysSkill CRUD endpoints.
package skill

import (
	service "github.com/good-fish-man/agent-runtime-client/application/service/skill"
)

// Handler groups the SysSkill HTTP handlers.
type Handler struct {
	svc *service.SysSkillService
}

// NewHandler builds the handler with its application service.
func NewHandler(svc *service.SysSkillService) *Handler {
	return &Handler{svc: svc}
}
