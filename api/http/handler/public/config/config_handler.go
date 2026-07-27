package config

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	appconfig "github.com/good-fish-man/agent-runtime-client/config"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// saveReq is the body for the save endpoints.
type saveReq struct {
	Content string `json:"content" binding:"required"`
}

// GetAppConfig handles GET /config/app — returns the app YAML content.
func (h *Handler) GetAppConfig(c *gin.Context) {
	h.readFile(c, h.appConfigFile)
}

// SaveAppConfig handles PUT /config/app — writes the app YAML content.
func (h *Handler) SaveAppConfig(c *gin.Context) {
	h.writeFile(c, h.appConfigFile, "Config saved successfully", true)
}

// GetSkillsConfig handles GET /config/skills — returns the skills YAML content.
func (h *Handler) GetSkillsConfig(c *gin.Context) {
	h.readFile(c, h.skillsConfigFile)
}

// SaveSkillsConfig handles PUT /config/skills — writes the skills YAML content.
func (h *Handler) SaveSkillsConfig(c *gin.Context) {
	h.writeFile(c, h.skillsConfigFile, "Skills config saved successfully", false)
}

// Status returns the actual files managed by this process.
func (h *Handler) Status(c *gin.Context) {
	response.Ok(c, gin.H{
		"service":            "agent-runtime-client",
		"pid":                os.Getpid(),
		"http_addr":          h.serviceHTTPAddr,
		"app_config_file":    h.appConfigFile,
		"skills_config_file": h.skillsConfigFile,
		"restart_supported":  h.restart != nil,
	})
}

// Restart requests a graceful in-process service restart after the response is flushed.
func (h *Handler) Restart(c *gin.Context) {
	if h.restart == nil {
		_ = c.Error(apierror.ErrInternal.WithMessage("service restart is not available"))
		return
	}
	if !h.prepareRestart(c, "client") {
		return
	}
	response.Ok(c, gin.H{"message": "restart scheduled"})
	time.AfterFunc(250*time.Millisecond, func() {
		select {
		case h.restart <- struct{}{}:
		default:
		}
	})
}

func (h *Handler) RuntimeStatus(c *gin.Context) {
	h.proxyRuntime(c, "/admin/config/status")
}

func (h *Handler) RuntimeConfig(c *gin.Context) {
	h.proxyRuntime(c, "/admin/config/runtime")
}

func (h *Handler) RuntimeSkillsConfig(c *gin.Context) {
	h.proxyRuntime(c, "/admin/config/skills")
}

func (h *Handler) RestartRuntime(c *gin.Context) {
	if !h.prepareRestart(c, "runtime") {
		return
	}
	h.proxyRuntime(c, "/admin/restart")
}

func (h *Handler) RestartCheck(c *gin.Context) {
	target := c.Query("target")
	if target != "client" && target != "runtime" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("target must be client or runtime"))
		return
	}
	conflicts, err := h.restartConflicts(c.Request.Context(), target)
	if err != nil {
		_ = c.Error(apierror.ErrInternal.WithMessage("failed to inspect ports: " + err.Error()))
		return
	}
	response.Ok(c, gin.H{"target": target, "conflicts": conflicts})
}

func (h *Handler) proxyRuntime(c *gin.Context, path string) {
	if h.runtimeBaseURL == "" {
		_ = c.Error(apierror.ErrInternal.WithMessage("agent-runtime HTTP address is not configured"))
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, h.runtimeBaseURL+path, c.Request.Body)
	if err != nil {
		_ = c.Error(apierror.ErrInternal.WithMessage("failed to build runtime request: " + err.Error()))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		_ = c.Error(apierror.ErrInternal.WithMessage("agent-runtime is unavailable: " + err.Error()))
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = c.Error(apierror.ErrInternal.WithMessage("failed to read runtime response: " + err.Error()))
		return
	}
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

func (h *Handler) readFile(c *gin.Context, path string) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			response.Ok(c, gin.H{"content": "", "path": path})
			return
		}
		_ = c.Error(apierror.ErrInternal.WithMessage("failed to read config file: " + err.Error()))
		return
	}
	response.Ok(c, gin.H{"content": string(content), "path": path})
}

func (h *Handler) writeFile(c *gin.Context, path, okMsg string, restartRequired bool) {
	var req saveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("invalid request: " + err.Error()))
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		_ = c.Error(apierror.ErrInternal.WithMessage("failed to create directory: " + err.Error()))
		return
	}
	if err := validateYAML(req.Content, restartRequired); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("invalid YAML: " + err.Error()))
		return
	}
	if err := writeFileAtomic(path, []byte(req.Content)); err != nil {
		_ = c.Error(apierror.ErrInternal.WithMessage("failed to write config file: " + err.Error()))
		return
	}
	response.Ok(c, gin.H{"message": okMsg, "path": path, "restart_required": restartRequired})
}

func validateYAML(content string, appConfig bool) error {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return err
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return errors.New("root value must be a mapping")
	}
	if !appConfig {
		return nil
	}
	cfg := appconfig.Default()
	decoder := yaml.NewDecoder(strings.NewReader(content))
	decoder.KnownFields(true)
	return decoder.Decode(cfg)
}

func writeFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
