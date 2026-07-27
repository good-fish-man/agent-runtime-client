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
				log.Errorf("panic recovered: %v\n%s", r, debug.Stack())
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
