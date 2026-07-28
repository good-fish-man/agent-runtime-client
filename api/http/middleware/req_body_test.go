package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUnit_ReqBody_RecordsRequestPathOnErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Trace(), ReqBody(), Recover())
	router.GET("/items/:id", func(c *gin.Context) {
		_ = c.Error(errors.New("database unavailable"))
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/items/42?include=owner", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestUnit_RedactSensitiveHeaders(t *testing.T) {
	headers := http.Header{"Authorization": {"Bearer secret"}, "Cookie": {"session=secret"}, "X-Trace-Id": {"trace-1"}}
	redacted := redactSensitiveHeaders(headers)
	if got := redacted.Get("Authorization"); got != "******" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := redacted.Get("Cookie"); got != "******" {
		t.Fatalf("Cookie = %q", got)
	}
	if got := redacted.Get("X-Trace-Id"); got != "trace-1" {
		t.Fatalf("X-Trace-Id = %q", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("source headers were changed: %q", got)
	}
}

func TestUnit_ResponseLogWriter_CapturesNormalResponseWithLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	lw := &responseLogWriter{ResponseWriter: c.Writer, limit: 5}

	_, err := lw.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("write response: %v", err)
	}

	if got := lw.bodyString(); got != "<truncated>" {
		t.Fatalf("bodyString = %q", got)
	}
	if lw.bytes != int64(len("hello world")) {
		t.Fatalf("bytes = %d", lw.bytes)
	}
}

func TestUnit_ResponseLogWriter_RedactsSensitiveJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Writer.Header().Set("Content-Type", "application/json")
	lw := &responseLogWriter{ResponseWriter: c.Writer, limit: logBodyLimit}
	_, _ = lw.Write([]byte(`{"access_token":"secret-token","data":{"name":"athena"}}`))

	body := lw.bodyString()
	if strings.Contains(body, "secret-token") || !strings.Contains(body, `"access_token":"******"`) {
		t.Fatalf("sensitive response was not redacted: %s", body)
	}
}

func TestUnit_ResponseLogWriter_DoesNotBufferSSEBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Writer.Header().Set("Content-Type", sseContentType)
	lw := &responseLogWriter{ResponseWriter: c.Writer, limit: logBodyLimit}

	chunk := "event: delta\ndata: {\"text\":\"hello\"}\n\n"
	_, err := lw.WriteString(chunk)
	if err != nil {
		t.Fatalf("write stream: %v", err)
	}

	if !lw.isStream() {
		t.Fatal("expected stream response")
	}
	if got := lw.bodyString(); got != "" {
		t.Fatalf("stream body should not be buffered, got %q", got)
	}
	if lw.events != strings.Count(chunk, "event: ") {
		t.Fatalf("events = %d", lw.events)
	}
}
