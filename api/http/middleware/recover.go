package middleware

import (
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/pkg/log"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// Recover catches panics and renders any handler-pushed errors (c.Error) via the
// standard response envelope. Mirrors agent-frame's CatchError but without the
// SQL-specific handling (this service has no database).
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.ErrorfCtx(c.Request.Context(), "panic recovered method=%s path=%s err=%v\n%s", c.Request.Method, c.Request.URL.RequestURI(), r, debug.Stack())
				if !c.Writer.Written() {
					response.Err(c, apierror.ErrInternal.WithMessagef("%v", r))
				}
				c.Abort()
				return
			}
			// Render the last handler-pushed error, if any and nothing written yet.
			if len(c.Errors) > 0 && !c.Writer.Written() {
				response.Err(c, c.Errors.Last().Err)
				c.Abort()
			}
		}()
		c.Next()
	}
}
