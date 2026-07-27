package user

import (
	"bytes"
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

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/user"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

const maxAvatarBytes = 5 << 20

var avatarExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
}

// UpdateAvatar stores a validated image and assigns it to the authenticated user.
func (h *Handler) UpdateAvatar(c *gin.Context) {
	fileHeader, err := c.FormFile("avatar")
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("请选择头像图片"))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("无法读取头像图片"))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxAvatarBytes {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("头像图片不能为空且不能超过 5MB"))
		return
	}
	mimeType := http.DetectContentType(data)
	extension, supported := avatarExtensions[mimeType]
	if !supported {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("头像仅支持 PNG、JPEG 或 GIF"))
		return
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("头像图片无效或尺寸超过 4096x4096"))
		return
	}
	if err := os.MkdirAll(h.avatarDir, 0o750); err != nil {
		_ = c.Error(err)
		return
	}
	userID := c.GetString("user_id")
	oldUser, _ := h.svc.FindSysUserById(c.Request.Context(), &dto.FindSysUserByIdReq{Ulid: userID})
	filename := userID + "-" + ulid.New() + extension
	path := filepath.Join(h.avatarDir, filename)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		_ = c.Error(err)
		return
	}

	avatarURL := h.avatarURLPrefix + "/" + filename
	user, err := h.svc.UpdateAvatar(c.Request.Context(), userID, avatarURL)
	if err != nil {
		_ = os.Remove(path)
		_ = c.Error(err)
		return
	}
	if oldUser != nil && strings.HasPrefix(oldUser.AvatarURL, h.avatarURLPrefix+"/") {
		oldFilename := filepath.Base(oldUser.AvatarURL)
		if oldFilename != filename {
			_ = os.Remove(filepath.Join(h.avatarDir, oldFilename))
		}
	}
	response.Ok(c, user)
}

// Avatar serves a generated avatar filename without exposing arbitrary files.
func (h *Handler) Avatar(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" || filename != filepath.Base(filename) || strings.ContainsAny(filename, `/\\`) {
		c.Status(http.StatusBadRequest)
		return
	}
	path := filepath.Join(h.avatarDir, filename)
	if _, err := os.Stat(path); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(path)
}
