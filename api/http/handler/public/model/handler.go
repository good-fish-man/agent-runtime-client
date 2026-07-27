// Package model contains the Gin handlers for SysModel CRUD endpoints.
package model

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	service "github.com/good-fish-man/agent-runtime-client/application/service/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/log"
)

// Handler groups the SysModel HTTP handlers.
type Handler struct {
	svc         *service.SysModelService
	installer   *localModelInstaller
	training    *modelTrainingManager
	runtimeHTTP string
}

func (h *Handler) WithRuntime(httpAddr string) *Handler {
	h.runtimeHTTP = strings.TrimRight(strings.TrimSpace(httpAddr), "/")
	return h
}

func (h *Handler) applyRuntimeMode(provider, model, mode string) {
	if h.runtimeHTTP == "" {
		return
	}
	payload, _ := json.Marshal(map[string]string{"provider": provider, "model": model, "mode": mode})
	req, _ := http.NewRequest(http.MethodPut, h.runtimeHTTP+"/admin/local-model/lifecycle", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Warnf("apply local model runtime mode failed: %v", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warnf("apply local model runtime mode returned status=%d model=%s mode=%s", resp.StatusCode, model, mode)
	}
}

// NewHandler builds the handler with its application service.
func NewHandler(svc *service.SysModelService) *Handler {
	return &Handler{svc: svc, installer: newLocalModelInstaller()}
}
