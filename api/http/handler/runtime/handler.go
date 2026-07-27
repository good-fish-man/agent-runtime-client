// Package runtime (api/http/handler) holds the Gin handlers for the runtime
// invocation endpoints. Handlers bind DTOs, delegate to the application service,
// and render via the standard response envelope (or SSE for streaming).
package runtime

import (
	service "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
)

// Handler groups the runtime HTTP handlers.
type Handler struct {
	svc *service.RuntimeService
}

// NewHandler builds the handler with its application service.
func NewHandler(svc *service.RuntimeService) *Handler {
	return &Handler{svc: svc}
}
