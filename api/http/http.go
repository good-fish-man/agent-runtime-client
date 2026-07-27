// Package http builds the Gin engine with the middleware chain and routes.
package http

import (
	"strings"

	"github.com/gin-gonic/gin"

	handler "github.com/good-fish-man/agent-runtime-client/api/http/handler/runtime"
	"github.com/good-fish-man/agent-runtime-client/api/http/middleware"
	"github.com/good-fish-man/agent-runtime-client/api/http/router"
	"github.com/good-fish-man/agent-runtime-client/api/http/router/public"
)

// NewEngine constructs a Gin engine with the recover/cors/trace middleware chain
// and both the runtime routes and the agent-frame-compatible public routes
// registered. mode is a gin mode (debug|release|test). pub may be nil (no DB),
// in which case only the runtime and health routes are mounted. prefix is the
// mount point for the public routes (e.g. /api/xiaoqinglong/agent-frame/v1).
func NewEngine(h *handler.Handler, pub *public.Handlers, prefix, mode string) *gin.Engine {
	if mode != "" {
		gin.SetMode(mode)
	}

	engine := gin.New()
	// ReqBody wraps Recover so response logging runs after handler-pushed errors
	// are rendered into the standard JSON envelope.
	engine.Use(middleware.Cors(), middleware.Trace(), middleware.ReqBody(), middleware.Recover())

	var auth gin.HandlerFunc
	if pub != nil {
		auth = pub.Auth
	}
	router.Register(engine, h, auth)

	if pub != nil {
		prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
		public.Register(engine.Group(prefix), pub)
	}

	return engine
}
