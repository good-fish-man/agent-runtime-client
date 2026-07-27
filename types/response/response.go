// Package response defines the uniform HTTP JSON envelope {code,message,data,
// trace_id} and helpers to render success/error responses. Streaming endpoints
// bypass this envelope and emit SSE directly.
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

// Body is the standard response envelope. Code 0 means success.
type Body struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

// Ok writes a 200 success envelope.
func Ok(c *gin.Context, data any) {
	OkStatus(c, http.StatusOK, data)
}

// OkStatus writes a success envelope with an explicit HTTP status.
func OkStatus(c *gin.Context, httpStatus int, data any) {
	c.JSON(httpStatus, Body{Code: 0, Message: "ok", Data: data, TraceID: traceID(c)})
}

// Err normalizes err into an APIError and writes the matching envelope+status.
func Err(c *gin.Context, err error) {
	apiErr := apierror.FromError(err)
	c.JSON(apiErr.HTTPStatus, Body{Code: apiErr.Code, Message: apiErr.Message, TraceID: traceID(c)})
}

func traceID(c *gin.Context) string {
	return c.GetString(consts.CtxKeyTraceID)
}
