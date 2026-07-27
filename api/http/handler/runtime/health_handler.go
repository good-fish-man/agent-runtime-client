package runtime

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// Health reports this service's health and the upstream runtime's status. It
// always returns 200 (the client is up); the runtime sub-status degrades
// gracefully when agent-runtime is unavailable.
func (h *Handler) Health(c *gin.Context) {
	body := gin.H{"service": consts.ServiceName, "status": "ok"}

	st, err := h.svc.Health(c.Request.Context())
	if err != nil {
		body["runtime"] = gin.H{
			"status": "NOT_SERVING",
			"error":  apierror.FromError(err).Message,
		}
	} else {
		body["runtime"] = st
	}

	response.Ok(c, body)
}

// Alive is a liveness probe: it returns 200 as long as the process is serving,
// independent of upstream dependencies.
func (h *Handler) Alive(c *gin.Context) {
	response.Ok(c, gin.H{"service": consts.ServiceName, "status": "alive"})
}

// Ready is a readiness probe: it reports 200 only when the upstream runtime is
// reachable, otherwise 503 so orchestrators can gate traffic.
func (h *Handler) Ready(c *gin.Context) {
	st, err := h.svc.Health(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"service": consts.ServiceName,
			"status":  "not_ready",
			"error":   apierror.FromError(err).Message,
		})
		return
	}
	response.Ok(c, gin.H{"service": consts.ServiceName, "status": "ready", "runtime": st})
}
