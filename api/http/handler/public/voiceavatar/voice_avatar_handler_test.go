package voiceavatar

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/api/http/middleware"
)

const testPrefix = "/api/agent-runtime-client/v1"

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newTestRouter(h *Handler, userID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Recover())
	r.Use(func(c *gin.Context) {
		if userID != "" {
			c.Set("user_id", userID)
		}
		c.Next()
	})
	r.POST("/voice-avatars", h.Upload)
	r.GET("/voice-avatars", h.List)
	r.DELETE("/voice-avatars/:id", h.Delete)
	r.GET("/voice-avatar/:filename", h.Serve)
	return r
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func multipartBody(t *testing.T, filename, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="avatar"; filename="`+filename+`"`)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &body, w.FormDataContentType()
}

func doUpload(t *testing.T, r *gin.Engine, filename, contentType string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBody(t, filename, contentType, data)
	req := httptest.NewRequest(http.MethodPost, "/voice-avatars", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodeView(t *testing.T, rec *httptest.ResponseRecorder) view {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, rec.Body.String())
	}
	var v view
	if err := json.Unmarshal(env.Data, &v); err != nil {
		t.Fatalf("decode view: %v (body=%s)", err, rec.Body.String())
	}
	return v
}

func TestUploadImageRoundTrip(t *testing.T) {
	h := NewHandler(t.TempDir(), testPrefix)
	r := newTestRouter(h, "user123")

	rec := doUpload(t, r, "me.png", "image/png", pngBytes(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body.String())
	}
	v := decodeView(t, rec)
	if v.Kind != "image" {
		t.Fatalf("kind = %q, want image", v.Kind)
	}
	if !strings.HasPrefix(v.ID, "custom:") {
		t.Fatalf("id = %q, want custom: prefix", v.ID)
	}
	if !strings.HasPrefix(v.URL, testPrefix+"/auth/voice-avatar/") {
		t.Fatalf("url = %q, want served under prefix", v.URL)
	}

	// List should contain the uploaded avatar.
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/voice-avatars", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	var listEnv envelope
	_ = json.Unmarshal(listRec.Body.Bytes(), &listEnv)
	var views []view
	if err := json.Unmarshal(listEnv.Data, &views); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(views) != 1 || views[0].ID != v.ID {
		t.Fatalf("list = %#v, want single uploaded avatar", views)
	}

	// Serve should return the stored file.
	filename := strings.TrimPrefix(v.URL, testPrefix+"/auth/voice-avatar/")
	serveRec := httptest.NewRecorder()
	r.ServeHTTP(serveRec, httptest.NewRequest(http.MethodGet, "/voice-avatar/"+filename, nil))
	if serveRec.Code != http.StatusOK {
		t.Fatalf("serve status = %d", serveRec.Code)
	}
	if serveRec.Body.Len() == 0 {
		t.Fatal("serve returned empty body")
	}

	// Delete should remove it.
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, httptest.NewRequest(http.MethodDelete, "/voice-avatars/"+v.ID, nil))
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", delRec.Code, delRec.Body.String())
	}
	afterRec := httptest.NewRecorder()
	r.ServeHTTP(afterRec, httptest.NewRequest(http.MethodGet, "/voice-avatar/"+filename, nil))
	if afterRec.Code != http.StatusNotFound {
		t.Fatalf("serve after delete = %d, want 404", afterRec.Code)
	}
}

func TestUploadVideoViaDeclaredType(t *testing.T) {
	h := NewHandler(t.TempDir(), testPrefix)
	r := newTestRouter(h, "user123")

	// Opaque bytes sniff to application/octet-stream; declared type drives acceptance.
	payload := make([]byte, 64)
	rec := doUpload(t, r, "clip.mp4", "video/mp4", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", rec.Code, rec.Body.String())
	}
	v := decodeView(t, rec)
	if v.Kind != "video" {
		t.Fatalf("kind = %q, want video", v.Kind)
	}
	if !strings.HasSuffix(v.URL, ".mp4") {
		t.Fatalf("url = %q, want .mp4 extension", v.URL)
	}
}

func TestUploadUnsupportedType(t *testing.T) {
	h := NewHandler(t.TempDir(), testPrefix)
	r := newTestRouter(h, "user123")

	rec := doUpload(t, r, "notes.txt", "text/plain", []byte("just some text, not media"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestListEmptyReturnsArray(t *testing.T) {
	h := NewHandler(t.TempDir(), testPrefix)
	r := newTestRouter(h, "user123")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/voice-avatars", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if strings.TrimSpace(string(env.Data)) != "[]" {
		t.Fatalf("data = %s, want []", string(env.Data))
	}
}

func TestDeleteMissingReturnsNotFound(t *testing.T) {
	h := NewHandler(t.TempDir(), testPrefix)
	r := newTestRouter(h, "user123")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/voice-avatars/custom:nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestUnauthorizedWithoutUser(t *testing.T) {
	h := NewHandler(t.TempDir(), testPrefix)
	r := newTestRouter(h, "") // no user_id set

	rec := doUpload(t, r, "me.png", "image/png", pngBytes(t))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("upload status = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/voice-avatars", nil))
	if listRec.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d, want 401", listRec.Code)
	}
}

func TestUploadIsolatedPerUser(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler(dir, testPrefix)

	ra := newTestRouter(h, "alice")
	if rec := doUpload(t, ra, "a.png", "image/png", pngBytes(t)); rec.Code != http.StatusOK {
		t.Fatalf("alice upload = %d", rec.Code)
	}

	// Bob should see none of alice's avatars.
	rb := newTestRouter(h, "bob")
	rec := httptest.NewRecorder()
	rb.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/voice-avatars", nil))
	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if strings.TrimSpace(string(env.Data)) != "[]" {
		t.Fatalf("bob list = %s, want [] (isolation)", string(env.Data))
	}
}
