package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

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
	body := w.body.String()
	if w.truncated {
		return body + "...<truncated>"
	}
	return body
}

// ReqBody logs request and response summaries. Streaming responses are not
// buffered; only status, bytes, write count, event count, and cost are logged.
func ReqBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestBody := readRequestBodyForLog(c.Request)

		blw := &responseLogWriter{
			ResponseWriter: c.Writer,
			limit:          logBodyLimit,
			streamPath:     isStreamPath(c.Request.URL.Path),
		}
		c.Writer = blw

		log.Infof("==== Request ====")
		log.Infof("Path=%s,Method=%s,Query=%s,Headers=%v,Body=%s",
			c.Request.URL.Path,
			c.Request.Method,
			c.Request.URL.RawQuery,
			c.Request.Header,
			requestBody,
		)

		c.Next()

		cost := time.Since(start)
		log.Infof("==== Response ====")
		if blw.isStream() {
			log.Infof("Status=%d,Stream=true,Bytes=%d,Writes=%d,Events=%d,Cost=%v",
				blw.Status(),
				blw.bytes,
				blw.writes,
				blw.events,
				cost,
			)
			return
		}
		log.Infof("Status=%d,Body=%s,Bytes=%d,Cost=%v",
			blw.Status(),
			blw.bodyString(),
			blw.bytes,
			cost,
		)
	}
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
