package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/pkg/errtrace"
	"github.com/good-fish-man/agent-runtime-client/pkg/log"
)

const (
	logBodyLimit       = 10 * 1024
	sseContentType     = "text/event-stream"
	unknownRequestBody = "<skipped>"
)

type responseLogWriter struct {
	gin.ResponseWriter
	body       bytes.Buffer
	limit      int
	bytes      int64
	writes     int
	events     int
	truncated  bool
	streaming  bool
	streamPath bool
}

func (w *responseLogWriter) Write(b []byte) (int, error) {
	w.capture(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseLogWriter) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

func (w *responseLogWriter) capture(b []byte) {
	w.writes++
	w.bytes += int64(len(b))
	if w.isStream() {
		w.events += bytes.Count(b, []byte("event: "))
		return
	}
	remaining := w.limit - w.body.Len()
	if remaining <= 0 {
		w.truncated = true
		return
	}
	if len(b) > remaining {
		w.body.Write(b[:remaining])
		w.truncated = true
		return
	}
	w.body.Write(b)
}

func (w *responseLogWriter) isStream() bool {
	if w.streaming {
		return true
	}
	if w.streamPath {
		w.streaming = true
		return true
	}
	if strings.Contains(strings.ToLower(w.Header().Get("Content-Type")), sseContentType) {
		w.streaming = true
		return true
	}
	return false
}

func (w *responseLogWriter) bodyString() string {
	if w.truncated {
		return "<truncated>"
	}
	return redactSensitiveBody(w.body.Bytes(), w.Header().Get("Content-Type"))
}

// ReqBody logs request and response summaries. Streaming responses are not
// buffered; only status, bytes, write count, event count, and cost are logged.
func ReqBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestBody := readRequestBodyForLog(c.Request)
		requestPath := requestPathForLog(c.Request)

		blw := &responseLogWriter{
			ResponseWriter: c.Writer,
			limit:          logBodyLimit,
			streamPath:     isStreamPath(c.Request.URL.Path),
		}
		c.Writer = blw

		log.Infof("==== Request ====")
		log.Infof("Path=%s,Method=%s,Query=%s,Headers=%v,Body=%s",
			requestPath,
			c.Request.Method,
			c.Request.URL.RawQuery,
			redactSensitiveHeaders(c.Request.Header),
			requestBody,
		)

		c.Next()

		cost := time.Since(start)
		logRequestError(c, requestPath, cost)
		log.Infof("==== Response ====")
		if blw.isStream() {
			log.Infof("Path=%s,Method=%s,Status=%d,Stream=true,Bytes=%d,Writes=%d,Events=%d,Cost=%v",
				requestPath,
				c.Request.Method,
				blw.Status(),
				blw.bytes,
				blw.writes,
				blw.events,
				cost,
			)
			return
		}
		log.Infof("Path=%s,Method=%s,Status=%d,Body=%s,Bytes=%d,Cost=%v",
			requestPath,
			c.Request.Method,
			blw.Status(),
			blw.bodyString(),
			blw.bytes,
			cost,
		)
	}
}

func requestPathForLog(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.EscapedPath()
}

func logRequestError(c *gin.Context, path string, cost time.Duration) {
	status := c.Writer.Status()
	var requestErr error
	if len(c.Errors) > 0 {
		requestErr = c.Errors.Last().Err
	}
	if status < http.StatusBadRequest && requestErr == nil {
		return
	}
	kv := []any{
		"method", c.Request.Method,
		"path", path,
		"query", c.Request.URL.RawQuery,
		"route", c.FullPath(),
		"status", status,
		"cost_ms", cost.Milliseconds(),
	}
	if requestErr != nil {
		kv = append(kv, "error_chain", errtrace.Format(requestErr))
	}
	if status >= http.StatusInternalServerError || requestErr != nil && status < http.StatusBadRequest {
		log.ErrorwCtx(c.Request.Context(), "http request failed", kv...)
		return
	}
	log.WarnwCtx(c.Request.Context(), "http request rejected", kv...)
}

func redactSensitiveHeaders(headers http.Header) http.Header {
	redacted := headers.Clone()
	for _, key := range []string{"Authorization", "Cookie", "Set-Cookie", "X-Api-Key", "Api-Key"} {
		if redacted.Get(key) != "" {
			redacted.Set(key, "******")
		}
	}
	return redacted
}

func readRequestBodyForLog(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	if !shouldLogRequestBody(r.Header.Get("Content-Type")) {
		return unknownRequestBody
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "<read_error:" + err.Error() + ">"
	}
	r.Body = io.NopCloser(bytes.NewBuffer(body))
	redacted := redactSensitiveBody(body, r.Header.Get("Content-Type"))
	if len(redacted) > logBodyLimit {
		return redacted[:logBodyLimit] + "...<truncated>"
	}
	return redacted
}

func shouldLogRequestBody(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "application/x-www-form-urlencoded") ||
		strings.HasPrefix(ct, "text/")
}

func isStreamPath(path string) bool {
	return strings.HasSuffix(path, "/stream")
}

func redactSensitiveBody(body []byte, contentType string) string {
	if !strings.Contains(strings.ToLower(contentType), "application/json") {
		return string(body)
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return string(body)
	}
	redactSensitiveValue(value)
	out, err := json.Marshal(value)
	if err != nil {
		return string(body)
	}
	return string(out)
}

func redactSensitiveValue(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if isSensitiveLogKey(key) {
				v[key] = "******"
				continue
			}
			redactSensitiveValue(child)
		}
	case []any:
		for _, child := range v {
			redactSensitiveValue(child)
		}
	}
}

func isSensitiveLogKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	switch normalized {
	case "apikey", "token", "password", "secret", "accesstoken", "refreshtoken":
		return true
	default:
		return strings.Contains(normalized, "secret") || strings.Contains(normalized, "password")
	}
}
