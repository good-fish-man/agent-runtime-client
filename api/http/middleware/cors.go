package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Cors applies permissive CORS headers and short-circuits preflight requests.
func Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers",
			"Origin, Content-Type, Accept, Authorization, "+traceAllowedHeaders())
		c.Header("Access-Control-Expose-Headers", traceExposedHeaders())

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
