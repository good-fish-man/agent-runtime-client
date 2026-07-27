// Package knowledge_base contains the Gin handlers for SysKnowledgeBase CRUD endpoints.
package knowledge_base

import (
	service "github.com/good-fish-man/agent-runtime-client/application/service/knowledge_base"
)

// Handler groups the SysKnowledgeBase HTTP handlers.
type Handler struct {
	svc *service.SysKnowledgeBaseService
}

// NewHandler builds the handler with its application service.
func NewHandler(svc *service.SysKnowledgeBaseService) *Handler {
	return &Handler{svc: svc}
}
