package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUnit_ResponseLogWriter_CapturesNormalResponseWithLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	lw := &responseLogWriter{ResponseWriter: c.Writer, limit: 5}

	_, err := lw.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("write response: %v", err)
	}

	if got := lw.bodyString(); got != "hello...<truncated>" {
		t.Fatalf("bodyString = %q", got)
	}
	if lw.bytes != int64(len("hello world")) {
		t.Fatalf("bytes = %d", lw.bytes)
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
