// Package router registers HTTP routes onto the Gin engine.
package router

import (
	"github.com/gin-gonic/gin"

	handler "github.com/good-fish-man/agent-runtime-client/api/http/handler/runtime"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

// Register wires the runtime invocation endpoints.
func Register(engine *gin.Engine, h *handler.Handler, auth gin.HandlerFunc) {
	engine.GET(consts.RouteHealth, h.Health)
	engine.GET(consts.RouteHealthAlive, h.Alive)
	engine.GET(consts.RouteHealthReady, h.Ready)

	v1 := engine.Group("/v1")
	if auth != nil {
		v1.Use(auth)
	}
	{
		v1.POST("/run", h.Run)
		v1.POST("/run/stream", h.RunStream)
		v1.POST("/agent", h.Agent)
		v1.POST("/agent/stream", h.AgentStream)
		v1.POST("/resume", h.Resume)
		v1.POST("/stop", h.Stop)
	}
}
