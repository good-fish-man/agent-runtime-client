package voiceavatar

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

const (
	maxImageBytes = 5 << 20  // 5MB
	maxVideoBytes = 25 << 20 // 25MB
	maxAvatars    = 24       // per-user cap
)

var imageExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var videoExtensions = map[string]string{
	"video/mp4":  ".mp4",
	"video/webm": ".webm",
}

// record is one stored voice avatar. It is persisted in the per-user JSON index.
type record struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // "image" | "video"
	Filename string `json:"filename"`
}

// view is the API representation returned to clients (with a resolved URL).
type view struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

func (h *Handler) toView(r record) view {
	return view{ID: r.ID, Name: r.Name, Kind: r.Kind, URL: h.urlPrefix + "/" + r.Filename}
}

func safeUserID(c *gin.Context) (string, bool) {
	userID := strings.TrimSpace(c.GetString("user_id"))
	if userID == "" || userID != filepath.Base(userID) || strings.ContainsAny(userID, `/\`) {
		return "", false
	}
	return userID, true
}

func (h *Handler) indexPath(userID string) string {
	return filepath.Join(h.indexDir, userID+".json")
}

func (h *Handler) readIndex(userID string) ([]record, error) {
	data, err := os.ReadFile(h.indexPath(userID))
	if err != nil {
		if os.IsNotExist(err) {
			return []record{}, nil
		}
		return nil, err
	}
	var records []record
	if err := json.Unmarshal(data, &records); err != nil {
		return []record{}, nil
	}
	return records, nil
}

func (h *Handler) writeIndex(userID string, records []record) error {
	if err := os.MkdirAll(h.indexDir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return os.WriteFile(h.indexPath(userID), data, 0o640)
}

// resolveType validates the payload and returns its extension and kind.
func resolveType(data []byte, headerType string) (ext, kind string, ok bool) {
	sniffed := http.DetectContentType(data)
	if e, found := imageExtensions[sniffed]; found {
		return e, "image", true
	}
	if e, found := videoExtensions[sniffed]; found {
		return e, "video", true
	}
	// Container sniffing is unreliable for some video files; fall back to the
	// declared multipart Content-Type for the video allow-list only.
	declared := strings.ToLower(strings.TrimSpace(headerType))
	if e, found := videoExtensions[declared]; found {
		return e, "video", true
	}
	return "", "", false
}

// Upload stores a validated image or short video and appends it to the user's index.
func (h *Handler) Upload(c *gin.Context) {
	userID, ok := safeUserID(c)
	if !ok {
		_ = c.Error(apierror.ErrUnauthorized)
		return
	}
	fileHeader, err := c.FormFile("avatar")
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("请选择要上传的形象文件"))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("无法读取上传的文件"))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxVideoBytes+1))
	if err != nil || len(data) == 0 {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("上传的文件不能为空"))
		return
	}
	ext, kind, valid := resolveType(data, fileHeader.Header.Get("Content-Type"))
	if !valid {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("仅支持图片(PNG/JPEG/GIF/WebP)或短视频(MP4/WebM)"))
		return
	}
	limit := maxImageBytes
	if kind == "video" {
		limit = maxVideoBytes
	}
	if len(data) > limit {
		if kind == "video" {
			_ = c.Error(apierror.ErrBadRequest.WithMessage("视频不能超过 25MB"))
		} else {
			_ = c.Error(apierror.ErrBadRequest.WithMessage("图片不能超过 5MB"))
		}
		return
	}
	if kind == "image" {
		config, _, decErr := image.DecodeConfig(bytes.NewReader(data))
		if decErr != nil || config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 {
			_ = c.Error(apierror.ErrBadRequest.WithMessage("图片无效或尺寸超过 8192x8192"))
			return
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	records, err := h.readIndex(userID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	if len(records) >= maxAvatars {
		_ = c.Error(apierror.ErrBadRequest.WithMessagef("最多只能保存 %d 个自定义形象", maxAvatars))
		return
	}
	if err := os.MkdirAll(h.mediaDir, 0o750); err != nil {
		_ = c.Error(err)
		return
	}
	filename := userID + "-" + ulid.New() + ext
	if err := os.WriteFile(filepath.Join(h.mediaDir, filename), data, 0o640); err != nil {
		_ = c.Error(err)
		return
	}
	name := strings.TrimSuffix(fileHeader.Filename, filepath.Ext(fileHeader.Filename))
	if len(name) > 24 {
		name = name[:24]
	}
	if strings.TrimSpace(name) == "" {
		if kind == "video" {
			name = "Video"
		} else {
			name = "Photo"
		}
	}
	rec := record{ID: "custom:" + ulid.New(), Name: name, Kind: kind, Filename: filename}
	records = append(records, rec)
	if err := h.writeIndex(userID, records); err != nil {
		_ = os.Remove(filepath.Join(h.mediaDir, filename))
		_ = c.Error(err)
		return
	}
	response.Ok(c, h.toView(rec))
}

// List returns all voice avatars belonging to the authenticated user.
func (h *Handler) List(c *gin.Context) {
	userID, ok := safeUserID(c)
	if !ok {
		_ = c.Error(apierror.ErrUnauthorized)
		return
	}
	h.mu.Lock()
	records, err := h.readIndex(userID)
	h.mu.Unlock()
	if err != nil {
		_ = c.Error(err)
		return
	}
	views := make([]view, 0, len(records))
	for _, r := range records {
		views = append(views, h.toView(r))
	}
	response.Ok(c, views)
}

// Delete removes a voice avatar (file + index entry) owned by the user.
func (h *Handler) Delete(c *gin.Context) {
	userID, ok := safeUserID(c)
	if !ok {
		_ = c.Error(apierror.ErrUnauthorized)
		return
	}
	id := c.Param("id")
	if strings.TrimSpace(id) == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("缺少形象 id"))
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	records, err := h.readIndex(userID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	kept := make([]record, 0, len(records))
	var removed *record
	for i := range records {
		if records[i].ID == id {
			r := records[i]
			removed = &r
			continue
		}
		kept = append(kept, records[i])
	}
	if removed == nil {
		_ = c.Error(apierror.ErrNotFound.WithMessage("形象不存在"))
		return
	}
	if err := h.writeIndex(userID, kept); err != nil {
		_ = c.Error(err)
		return
	}
	if removed.Filename == filepath.Base(removed.Filename) && !strings.ContainsAny(removed.Filename, `/\`) {
		_ = os.Remove(filepath.Join(h.mediaDir, removed.Filename))
	}
	response.Ok(c, gin.H{"id": id})
}

// Serve returns a stored voice-avatar file without exposing arbitrary paths.
func (h *Handler) Serve(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" || filename != filepath.Base(filename) || strings.ContainsAny(filename, `/\`) {
		c.Status(http.StatusBadRequest)
		return
	}
	path := filepath.Join(h.mediaDir, filename)
	if _, err := os.Stat(path); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(path)
}
