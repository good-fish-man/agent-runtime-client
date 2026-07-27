package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/pkg/log"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

func TestUnit_Trace_UsesIncomingRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const want = "req-from-client"
	router := gin.New()
	router.Use(Trace())
	router.GET("/ping", func(c *gin.Context) {
		got, _ := c.Request.Context().Value(log.ReqIDKey).(string)
		if got != want {
			t.Fatalf("request context trace id = %q, want %q", got, want)
		}
		if got := c.GetString(consts.CtxKeyTraceID); got != want {
			t.Fatalf("gin context trace id = %q, want %q", got, want)
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(consts.HeaderRequestID, want)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if got := w.Header().Get(consts.HeaderTraceID); got != want {
		t.Fatalf("%s response header = %q, want %q", consts.HeaderTraceID, got, want)
	}
	if got := w.Header().Get(consts.HeaderRequestID); got != want {
		t.Fatalf("%s response header = %q, want %q", consts.HeaderRequestID, got, want)
	}
}

func TestUnit_Trace_ParsesTraceparent(t *testing.T) {
	const want = "4bf92f3577b34da6a3ce929d0e0e4736"
	if got := traceIDFromTraceparent("00-" + want + "-00f067aa0ba902b7-01"); got != want {
		t.Fatalf("traceparent trace id = %q, want %q", got, want)
	}
}

func TestUnit_Cors_AllowsTraceHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Cors())
	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	allowed := w.Header().Get("Access-Control-Allow-Headers")
	for _, header := range traceHeaderCandidates {
		if !strings.Contains(allowed, header) {
			t.Fatalf("allow headers %q missing %s", allowed, header)
		}
	}
}
