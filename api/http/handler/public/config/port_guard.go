package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	appconfig "github.com/good-fish-man/agent-runtime-client/config"
)

type portConflict struct {
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	PID         int    `json:"pid"`
	Command     string `json:"command"`
	Service     string `json:"service,omitempty"`
	Managed     bool   `json:"managed"`
	SameService bool   `json:"same_service"`
}

type restartRequest struct {
	KillPIDs []int `json:"kill_pids"`
}

func (h *Handler) prepareRestart(c *gin.Context, target string) bool {
	var req restartRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "invalid restart request: " + err.Error()})
			return false
		}
	}
	confirmed := make(map[int]bool, len(req.KillPIDs))
	for _, pid := range req.KillPIDs {
		confirmed[pid] = true
	}
	conflicts, err := h.restartConflicts(c.Request.Context(), target)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "failed to inspect ports: " + err.Error()})
		return false
	}
	requiresConfirmation := make([]portConflict, 0)
	for _, conflict := range conflicts {
		if conflict.Managed {
			continue
		}
		if !conflict.SameService && !confirmed[conflict.PID] {
			requiresConfirmation = append(requiresConfirmation, conflict)
			continue
		}
		if err := terminateProcess(conflict.PID); err != nil {
			c.JSON(http.StatusConflict, gin.H{"code": http.StatusConflict, "message": err.Error(), "data": gin.H{"conflicts": conflicts}})
			return false
		}
	}
	if len(requiresConfirmation) > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"code": http.StatusConflict, "message": "ports are occupied by other programs; confirmation is required",
			"data": gin.H{"conflicts": requiresConfirmation, "requires_confirmation": true},
		})
		return false
	}
	return true
}

func (h *Handler) restartConflicts(ctx context.Context, target string) ([]portConflict, error) {
	ports, managedPID, err := h.targetPorts(ctx, target)
	if err != nil {
		return nil, err
	}
	serviceName := "agent-runtime-client"
	if target == "runtime" {
		serviceName = "agent-runtime"
	}
	conflicts := make([]portConflict, 0)
	seen := make(map[string]bool)
	for protocol, port := range ports {
		processes, err := listeningProcesses(port)
		if err != nil {
			return nil, err
		}
		for _, process := range processes {
			key := fmt.Sprintf("%d:%d", process.PID, port)
			if seen[key] {
				continue
			}
			seen[key] = true
			detectedService := detectHTTPService(ctx, port)
			process.Port = port
			process.Protocol = protocol
			process.Service = detectedService
			process.Managed = process.PID == managedPID
			process.SameService = detectedService == serviceName
			conflicts = append(conflicts, process)
		}
	}
	return conflicts, nil
}

func (h *Handler) targetPorts(ctx context.Context, target string) (map[string]int, int, error) {
	if target == "client" {
		cfg, err := appconfig.Load(h.appConfigFile)
		if err != nil {
			return nil, 0, err
		}
		port, err := addressPort(cfg.Server.HTTPAddr)
		return map[string]int{"http": port}, os.Getpid(), err
	}

	document, err := h.getRuntimeDocument(ctx, "/admin/config/runtime")
	if err != nil {
		return nil, 0, err
	}
	var cfg struct {
		Server struct {
			GRPCAddr string `yaml:"grpc_addr"`
			HTTPAddr string `yaml:"http_addr"`
		} `yaml:"server"`
	}
	if err := yaml.Unmarshal([]byte(document.Content), &cfg); err != nil {
		return nil, 0, err
	}
	grpcPort, err := addressPort(cfg.Server.GRPCAddr)
	if err != nil {
		return nil, 0, err
	}
	httpPort, err := addressPort(cfg.Server.HTTPAddr)
	if err != nil {
		return nil, 0, err
	}
	status, err := h.getRuntimeStatus(ctx)
	if err != nil {
		return nil, 0, err
	}
	return map[string]int{"grpc": grpcPort, "http": httpPort}, status.PID, nil
}

type runtimeDocument struct {
	Content string `json:"content"`
}

type runtimeStatus struct {
	PID int `json:"pid"`
}

func (h *Handler) getRuntimeDocument(ctx context.Context, path string) (*runtimeDocument, error) {
	var document runtimeDocument
	if err := h.getRuntimeData(ctx, path, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

func (h *Handler) getRuntimeStatus(ctx context.Context) (*runtimeStatus, error) {
	var status runtimeStatus
	if err := h.getRuntimeData(ctx, "/admin/config/status", &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (h *Handler) getRuntimeData(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.runtimeBaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("runtime returned status %d", resp.StatusCode)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, target)
}

func addressPort(address string) (int, error) {
	value := strings.TrimSpace(address)
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Host
	}
	_, portValue, err := net.SplitHostPort(value)
	if err != nil {
		return 0, fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port in address %q", address)
	}
	return port, nil
}

func listeningProcesses(port int) ([]portConflict, error) {
	command := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-Fpct")
	output, err := command.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("lsof port %d: %w", port, err)
	}
	result := make([]portConflict, 0)
	var current *portConflict
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ := strconv.Atoi(string(line[1:]))
			result = append(result, portConflict{PID: pid})
			current = &result[len(result)-1]
		case 'c':
			if current != nil {
				current.Command = string(line[1:])
			}
		}
	}
	return result, nil
}

func detectHTTPService(ctx context.Context, port int) string {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for _, candidate := range []struct {
		path    string
		service string
	}{
		{path: "/health/alive", service: "agent-runtime-client"},
		{path: "/admin/config/status", service: "agent-runtime"},
	} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, candidate.path), nil)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK && strings.Contains(body.String(), candidate.service) {
			return candidate.service
		}
	}
	return ""
}

func terminateProcess(pid int) error {
	if pid <= 1 || pid == os.Getpid() {
		return fmt.Errorf("refusing to terminate protected pid %d", pid)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("terminate pid %d: %w", pid, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := process.Kill(); err != nil {
		return fmt.Errorf("force kill pid %d: %w", pid, err)
	}
	return nil
}
