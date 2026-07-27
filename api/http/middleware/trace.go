// Package middleware holds Gin middleware: trace-id binding, CORS, and a central
// recover/error renderer.
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/pkg/log"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

var traceHeaderCandidates = []string{
	consts.HeaderTraceID,
	consts.HeaderRequestID,
	consts.HeaderCorrelationID,
	consts.HeaderTraceparent,
}

var traceResponseHeaders = []string{
	consts.HeaderTraceID,
	consts.HeaderRequestID,
	consts.HeaderCorrelationID,
}

// Trace resolves a trace id from incoming HTTP trace headers (or generated),
// binds it to the gin context, request context, response header, and the
// current goroutine's logger, then clears the binding when the request
// completes.
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := incomingTraceID(c)
		if id == "" {
			id = genTraceID()
		}
		c.Set(consts.CtxKeyTraceID, id)
		for _, header := range traceResponseHeaders {
			c.Writer.Header().Set(header, id)
		}
		c.Request = c.Request.WithContext(log.WithReqID(c.Request.Context(), id))

		log.SetReqId(id)
		defer log.ClearReqId()

		c.Next()
	}
}

func incomingTraceID(c *gin.Context) string {
	for _, header := range traceHeaderCandidates {
		raw := strings.TrimSpace(c.GetHeader(header))
		if raw == "" {
			continue
		}
		if strings.EqualFold(header, consts.HeaderTraceparent) {
			return traceIDFromTraceparent(raw)
		}
		return raw
	}
	return ""
}

func traceIDFromTraceparent(header string) string {
	parts := strings.Split(header, "-")
	if len(parts) < 4 {
		return ""
	}
	id := strings.TrimSpace(parts[1])
	if len(id) != 32 || id == "00000000000000000000000000000000" {
		return ""
	}
	return id
}

func traceAllowedHeaders() string {
	return strings.Join(traceHeaderCandidates, ", ")
}

func traceExposedHeaders() string {
	return strings.Join(traceResponseHeaders, ", ")
}

func genTraceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return consts.ServiceName
	}
	return "arc-" + hex.EncodeToString(b[:])
}
