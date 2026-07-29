// Package voiceavatar provides disk-backed persistence for the voice-call
// "virtual human" avatars uploaded by users (image or short video). It mirrors
// the storage/serving conventions of handler/public/user/avatar_handler.go but
// supports multiple avatars per user via a per-user JSON index, and does not
// depend on the database (metadata lives on disk), so it degrades gracefully.
package voiceavatar

import (
	"path/filepath"
	"strings"
	"sync"
)

// Handler groups the voice-avatar HTTP handlers.
type Handler struct {
	mediaDir  string
	indexDir  string
	urlPrefix string
	mu        sync.Mutex
}

// NewHandler builds the handler with disk storage under uploadsDir and a public
// URL prefix used to serve stored files.
func NewHandler(uploadsDir, publicPrefix string) *Handler {
	if strings.TrimSpace(uploadsDir) == "" {
		uploadsDir = filepath.Join("data", "uploads")
	}
	if absolute, err := filepath.Abs(uploadsDir); err == nil {
		uploadsDir = absolute
	}
	base := filepath.Join(uploadsDir, "voice-avatars")
	return &Handler{
		mediaDir:  filepath.Join(base, "media"),
		indexDir:  filepath.Join(base, "index"),
		urlPrefix: strings.TrimRight(publicPrefix, "/") + "/auth/voice-avatar",
	}
}
