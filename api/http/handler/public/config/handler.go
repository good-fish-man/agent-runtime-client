// Package config exposes endpoints that read/write local YAML configuration
// files (application config and skills config). Paths are resolved from the
// service configuration, falling back to ~/.xiaoqinglong/config/*.yaml to stay
// compatible with agent-frame's on-disk layout.
package config

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/good-fish-man/agent-runtime-client/config"
)

// Handler serves the config-file read/write endpoints.
type Handler struct {
	appConfigFile    string
	skillsConfigFile string
	restart          chan<- struct{}
	runtimeBaseURL   string
	serviceHTTPAddr  string
	httpClient       *http.Client
}

// NewHandler builds the handler from the configured (optional) file paths.
func NewHandler(paths appconfig.PathsConfig, restart ...chan<- struct{}) *Handler {
	h := &Handler{
		appConfigFile:    firstNonEmpty(paths.AppConfigFile, defaultConfigFile("config.yaml")),
		skillsConfigFile: firstNonEmpty(paths.SkillsConfigFile, defaultConfigFile("skills-config.yaml")),
		httpClient:       &http.Client{Timeout: 10 * time.Second},
	}
	if len(restart) > 0 {
		h.restart = restart[0]
	}
	return h
}

// WithRuntime configures the localhost agent-runtime administration proxy.
func (h *Handler) WithRuntime(baseURL string) *Handler {
	h.runtimeBaseURL = strings.TrimRight(baseURL, "/")
	return h
}

// WithService records the currently active client listen address for restart checks.
func (h *Handler) WithService(httpAddr string) *Handler {
	h.serviceHTTPAddr = httpAddr
	return h
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// defaultConfigFile mirrors agent-frame's ~/.xiaoqinglong/config/<name> default.
func defaultConfigFile(name string) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".xiaoqinglong", "config", name)
}
