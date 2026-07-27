// Package job contains the Gin handlers for job execution log endpoints.
package job

import service "github.com/good-fish-man/agent-runtime-client/application/service/job"

// Handler groups the job execution HTTP handlers.
type Handler struct {
	svc *service.JobExecutionService
}

// NewHandler builds the handler with its application service.
func NewHandler(svc *service.JobExecutionService) *Handler {
	return &Handler{svc: svc}
}
