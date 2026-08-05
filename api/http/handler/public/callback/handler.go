// Package callback provides a channel-callback dispatch skeleton. The heavy
// WebSocket/long-connection integrations (feishu/dingtalk/wework) from
// agent-frame are intentionally NOT ported here; this endpoint accepts inbound
// callbacks, records the channel, and returns a not-implemented envelope so the
// route contract exists and can be filled in per-channel later.
package callback

import (
	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	log "github.com/good-fish-man/logx"
)

// Handler serves POST /callback/:channel.
type Handler struct{}

// NewHandler builds the callback handler.
func NewHandler() *Handler { return &Handler{} }

// HandleCallback logs the inbound channel callback and returns 501 until a
// concrete per-channel integration is wired in.
func (h *Handler) HandleCallback(c *gin.Context) {
	channel := c.Param("channel")
	log.Infof("[channel callback] received inbound callback for channel=%s", channel)
	_ = c.Error(apierror.ErrNotImplemented.WithMessagef("channel callback %q not yet implemented", channel))
}
