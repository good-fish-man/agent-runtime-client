package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/model"
	modelentity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type localModelInstallJob struct {
	dto.LocalModelInstallJobRsp
	UserID string
}

type localModelInstaller struct {
	mu         sync.RWMutex
	jobs       map[string]*localModelInstallJob
	modelLocks map[string]*sync.Mutex
}

func newLocalModelInstaller() *localModelInstaller {
	return &localModelInstaller{
		jobs:       make(map[string]*localModelInstallJob),
		modelLocks: make(map[string]*sync.Mutex),
	}
}

func (h *Handler) LocalModelEnvironment(c *gin.Context) {
	catalog, err := h.svc.FindModelCatalogByID(c.Request.Context(), c.Param("ulid"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if !catalog.IsFree || !catalog.Installable || !supportedLocalRuntime(catalog.Runtime) {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("该模型不支持本地自动安装"))
		return
	}
	response.Ok(c, inspectLocalEnvironment(c.Request.Context(), catalog))
}

func (h *Handler) InstallLocalModel(c *gin.Context) {
	catalog, err := h.svc.FindModelCatalogByID(c.Request.Context(), c.Param("ulid"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if !catalog.IsFree || !catalog.Installable || !supportedLocalRuntime(catalog.Runtime) {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("该模型不在允许安装的本地模型目录中"))
		return
	}
	environment := inspectLocalEnvironment(c.Request.Context(), catalog)
	if !environment.Compatible {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(environment.Message))
		return
	}
	if !environment.RuntimeInstalled && !environment.RuntimeInstallSupported {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(environment.Message))
		return
	}
	job := h.installer.start(c.GetString("user_id"), catalog)
	response.OkStatus(c, http.StatusAccepted, &dto.LocalModelInstallRsp{JobID: job.JobID})
}

func (h *Handler) LocalModelInstallJob(c *gin.Context) {
	job, ok := h.installer.get(c.Param("job_id"), c.GetString("user_id"))
	if !ok {
		_ = c.Error(apierror.ErrNotFound.WithMessage("安装任务不存在"))
		return
	}
	response.Ok(c, &job.LocalModelInstallJobRsp)
}

func (i *localModelInstaller) start(userID string, catalog *dto.FindModelCatalogRsp) *localModelInstallJob {
	job := &localModelInstallJob{
		UserID: userID,
		LocalModelInstallJobRsp: dto.LocalModelInstallJobRsp{
			JobID: ulid.New(), CatalogID: catalog.Ulid, ModelVersion: catalog.ModelVersion,
			Status: "queued", Stage: "environment", Message: "正在检查本机环境",
		},
	}
	i.mu.Lock()
	for _, existing := range i.jobs {
		if existing.UserID == userID && existing.CatalogID == catalog.Ulid && (existing.Status == "queued" || existing.Status == "running") {
			i.mu.Unlock()
			return existing
		}
	}
	i.jobs[job.JobID] = job
	i.mu.Unlock()
	go i.run(job.JobID, catalog)
	return job
}

func (i *localModelInstaller) get(jobID, userID string) (*localModelInstallJob, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	job, ok := i.jobs[jobID]
	if !ok || job.UserID != userID {
		return nil, false
	}
	copy := *job
	return &copy, true
}

func (i *localModelInstaller) update(jobID string, mutate func(*localModelInstallJob)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if job := i.jobs[jobID]; job != nil {
		mutate(job)
	}
}

func (i *localModelInstaller) lockModel(model string) func() {
	i.mu.Lock()
	lock := i.modelLocks[model]
	if lock == nil {
		lock = &sync.Mutex{}
		i.modelLocks[model] = lock
	}
	i.mu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (i *localModelInstaller) run(jobID string, catalog *dto.FindModelCatalogRsp) {
	fail := func(err error) {
		i.update(jobID, func(job *localModelInstallJob) {
			job.Status = "failed"
			job.Error = err.Error()
			job.Message = "安装失败"
		})
	}
	i.update(jobID, func(job *localModelInstallJob) { job.Status = "running" })
	unlockModel := i.lockModel(catalog.ModelVersion)
	defer unlockModel()
	if strings.EqualFold(catalog.Runtime, modelentity.ProviderDiffusers) {
		i.runDiffusers(jobID, catalog, fail)
		return
	}

	binary, _ := findOllamaBinary()
	if binary == "" {
		i.update(jobID, func(job *localModelInstallJob) {
			job.Stage = "runtime"
			job.Progress = 5
			job.Message = "正在安装 Ollama 运行时"
		})
		var err error
		binary, err = installOllamaRuntime()
		if err != nil {
			fail(err)
			return
		}
	}
	if !ollamaRunning(context.Background()) {
		i.update(jobID, func(job *localModelInstallJob) {
			job.Stage = "runtime"
			job.Progress = 10
			job.Message = "正在启动 Ollama 运行时"
		})
		if err := startOllama(binary); err != nil {
			fail(err)
			return
		}
	}
	if ollamaModelInstalled(context.Background(), catalog.ModelVersion) {
		i.update(jobID, func(job *localModelInstallJob) {
			job.Status = "completed"
			job.Stage = "complete"
			job.Progress = 100
			job.Message = "模型已存在，无需重复下载"
		})
		return
	}
	i.update(jobID, func(job *localModelInstallJob) {
		job.Stage = "model"
		job.Progress = 12
		job.Message = "正在下载 " + catalog.DisplayName
	})
	if err := pullOllamaModel(catalog.ModelVersion, func(progress int, message string) {
		i.update(jobID, func(job *localModelInstallJob) {
			job.Progress = 12 + progress*88/100
			job.Message = message
		})
	}); err != nil {
		fail(err)
		return
	}
	i.update(jobID, func(job *localModelInstallJob) {
		job.Status = "completed"
		job.Stage = "complete"
		job.Progress = 100
		job.Message = "模型已安装，可以直接创建并使用"
	})
}

func inspectLocalEnvironment(ctx context.Context, catalog *dto.FindModelCatalogRsp) *dto.LocalModelEnvironmentRsp {
	if strings.EqualFold(catalog.Runtime, modelentity.ProviderDiffusers) {
		return inspectDiffusersEnvironment(catalog)
	}
	binary, _ := findOllamaBinary()
	running := ollamaRunning(ctx)
	memoryTotal, memoryAvailable := systemMemoryBytes()
	storageTotal, storageAvailable := storageSpaceBytes(ollamaModelsPath())
	memoryGB := bytesToGB(memoryTotal)
	compatible := memoryGB == 0 || memoryGB >= catalog.MinMemoryGB
	result := &dto.LocalModelEnvironmentRsp{
		OS: runtime.GOOS, Arch: runtime.GOARCH, MemoryGB: memoryGB,
		MemoryTotalBytes: memoryTotal, MemoryAvailableBytes: memoryAvailable,
		StorageTotalBytes: storageTotal, StorageAvailableBytes: storageAvailable,
		Runtime:          catalog.Runtime,
		RuntimeInstalled: binary != "", RuntimeRunning: running,
		RuntimeInstallSupported: binary != "" || canInstallOllamaRuntime(),
		ModelInstalled:          running && ollamaModelInstalled(ctx, catalog.ModelVersion),
		Compatible:              compatible,
	}
	if binary != "" {
		versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(versionCtx, binary, "--version").CombinedOutput(); err == nil {
			result.RuntimeVersion = strings.TrimSpace(string(output))
		}
	}
	switch {
	case !compatible:
		result.Message = fmt.Sprintf("当前内存 %dGB，建议至少 %dGB", memoryGB, catalog.MinMemoryGB)
	case result.ModelInstalled:
		result.Message = "模型已经安装"
	case binary == "" && !result.RuntimeInstallSupported:
		result.Message = "当前环境无法自动安装 Ollama，请先按官方说明安装运行时"
	case binary == "":
		result.Message = "将先自动安装 Ollama，再下载模型"
	case !running:
		result.Message = "将启动 Ollama 并下载模型"
	default:
		result.Message = "环境兼容，可以下载安装"
	}
	return result
}

func supportedLocalRuntime(value string) bool {
	return strings.EqualFold(value, modelentity.ProviderOllama) || strings.EqualFold(value, modelentity.ProviderDiffusers)
}

func (i *localModelInstaller) runDiffusers(jobID string, catalog *dto.FindModelCatalogRsp, fail func(error)) {
	python, _ := findPythonBinary()
	if python == "" {
		fail(fmt.Errorf("未找到 Python 3.10+，请先安装 Python"))
		return
	}
	venvPython := diffusersVenvPython()
	if !diffusersRuntimeInstalled() {
		i.update(jobID, func(job *localModelInstallJob) {
			job.Stage, job.Progress, job.Message = "runtime", 5, "正在创建隔离环境并安装 Diffusers 运行时"
		})
		if err := installDiffusersRuntime(python); err != nil {
			fail(err)
			return
		}
	}
	modelDir := diffusersModelPath(catalog.ModelVersion)
	if diffusersModelInstalled(catalog.ModelVersion) {
		i.update(jobID, func(job *localModelInstallJob) {
			job.Status, job.Stage, job.Progress, job.Message = "completed", "complete", 100, "模型已存在，无需重复下载"
		})
		return
	}
	i.update(jobID, func(job *localModelInstallJob) {
		job.Stage, job.Progress, job.Message = "model", 20, "正在从 Hugging Face 下载 "+catalog.DisplayName
	})
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		fail(err)
		return
	}
	if err := cleanupUnusedDiffusersArtifacts(modelDir, catalog.ModelVersion); err != nil {
		fail(fmt.Errorf("清理错误下载格式失败: %w", err))
		return
	}
	script := `from huggingface_hub import snapshot_download
import sys
ignore_patterns = [
    "*.onnx", "*.onnx_data", "*.xml", "*.msgpack", "*.ckpt",
    "*.h5", "*.ot", "*.tflite", "*.png", "*.jpg", "*.jpeg",
    "*openvino*", "*flax*",
]
if "stable-diffusion-xl" in sys.argv[1].lower():
    ignore_patterns += [
        "*.bin",
        "sd_xl*.safetensors",
        "text_encoder/model.safetensors",
        "text_encoder_2/model.safetensors",
        "unet/diffusion_pytorch_model.safetensors",
        "vae/diffusion_pytorch_model.safetensors",
    ]
snapshot_download(
    repo_id=sys.argv[1],
    local_dir=sys.argv[2],
    max_workers=4,
    ignore_patterns=ignore_patterns,
)
open(sys.argv[2] + "/.athena_complete", "w").write("ok")`
	command := exec.Command(venvPython, "-c", script, catalog.ModelVersion, modelDir)
	command.Env = append(os.Environ(), "HF_HOME="+filepath.Join(athenaHome(), "huggingface"))
	logPath := filepath.Join(athenaHome(), "logs", "model-install-"+jobID+".log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		fail(err)
		return
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		fail(err)
		return
	}
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		fail(err)
		return
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	expectedBytes := parseDownloadSize(catalog.DownloadSize)
	for {
		select {
		case err := <-done:
			_ = logFile.Close()
			if err != nil {
				fail(fmt.Errorf("下载图片模型失败: %v；日志: %s；详情: %s", err, logPath, tailFile(logPath, 4096)))
				return
			}
			i.update(jobID, func(job *localModelInstallJob) {
				job.Status, job.Stage, job.Progress, job.Message = "completed", "complete", 100, "图片模型已安装，可以创建并绑定到 Agent"
			})
			return
		case <-ticker.C:
			downloaded := directorySize(modelDir)
			progress := 20
			if expectedBytes > 0 {
				progress += minInt(75, int(downloaded*75/expectedBytes))
			}
			i.update(jobID, func(job *localModelInstallJob) {
				job.Progress = progress
				job.Message = fmt.Sprintf("正在下载 %s：%.1f GB / %s", catalog.DisplayName, float64(downloaded)/(1<<30), catalog.DownloadSize)
			})
		}
	}
}

func cleanupUnusedDiffusersArtifacts(root, model string) error {
	// Old full-repository downloads leave large hashed partial files here. Completed
	// model files remain outside this cache and Hugging Face can rebuild its metadata.
	if err := os.RemoveAll(filepath.Join(root, ".cache", "huggingface", "download")); err != nil {
		return err
	}
	isSDXL := strings.Contains(strings.ToLower(model), "stable-diffusion-xl")
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		name := strings.ToLower(info.Name())
		ext := strings.ToLower(filepath.Ext(name))
		unused := ext == ".onnx" || strings.HasSuffix(name, ".onnx_data") || ext == ".xml" || ext == ".msgpack" || ext == ".ckpt" || ext == ".h5" || ext == ".ot" || ext == ".tflite" || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || strings.Contains(name, "openvino") || strings.Contains(name, "flax") || (isSDXL && ext == ".bin")
		if !unused && (name == "model.safetensors" || name == "diffusion_pytorch_model.safetensors") {
			_, fp16Err := os.Stat(strings.TrimSuffix(path, ".safetensors") + ".fp16.safetensors")
			unused = fp16Err == nil
		}
		if unused {
			return os.Remove(path)
		}
		return nil
	})
}

func directorySize(root string) uint64 {
	var total uint64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && info.Size() > 0 {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}

func parseDownloadSize(value string) uint64 {
	text := strings.ToUpper(strings.TrimSpace(value))
	multiplier := float64(1)
	for suffix, unit := range map[string]float64{"TB": 1 << 40, "GB": 1 << 30, "MB": 1 << 20, "KB": 1 << 10} {
		if strings.HasSuffix(text, suffix) {
			multiplier = unit
			text = strings.TrimSpace(strings.TrimSuffix(text, suffix))
			break
		}
	}
	number, _ := strconv.ParseFloat(text, 64)
	return uint64(number * multiplier)
}

func tailFile(path string, limit int64) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if int64(len(data)) > limit {
		data = data[int64(len(data))-limit:]
	}
	return strings.TrimSpace(string(data))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func inspectDiffusersEnvironment(catalog *dto.FindModelCatalogRsp) *dto.LocalModelEnvironmentRsp {
	memoryTotal, memoryAvailable := systemMemoryBytes()
	storageTotal, storageAvailable := storageSpaceBytes(diffusersModelsPath())
	memoryGB := bytesToGB(memoryTotal)
	python, _ := findPythonBinary()
	installed := diffusersRuntimeInstalled()
	compatible := (memoryGB == 0 || memoryGB >= catalog.MinMemoryGB) && supportedDiffusersPlatform()
	result := &dto.LocalModelEnvironmentRsp{
		OS: runtime.GOOS, Arch: runtime.GOARCH, MemoryGB: memoryGB,
		MemoryTotalBytes: memoryTotal, MemoryAvailableBytes: memoryAvailable,
		StorageTotalBytes: storageTotal, StorageAvailableBytes: storageAvailable,
		Runtime: modelentity.ProviderDiffusers, RuntimeInstalled: installed, RuntimeRunning: installed,
		RuntimeInstallSupported: python != "" && supportedDiffusersPlatform(),
		ModelInstalled:          diffusersModelInstalled(catalog.ModelVersion), Compatible: compatible,
	}
	switch {
	case !supportedDiffusersPlatform():
		result.Message = "当前系统或 CPU 架构暂不支持自动安装 Diffusers"
	case memoryGB > 0 && memoryGB < catalog.MinMemoryGB:
		result.Message = fmt.Sprintf("当前内存 %dGB，建议至少 %dGB", memoryGB, catalog.MinMemoryGB)
	case python == "":
		result.Message = "请先安装 Python 3.10 或更高版本"
	case result.ModelInstalled:
		result.Message = "模型已经安装"
	case !installed:
		result.Message = "将创建 Python 隔离环境、安装 Diffusers 并下载模型"
	default:
		result.Message = "环境兼容，可以下载模型"
	}
	return result
}

func supportedDiffusersPlatform() bool {
	return (runtime.GOOS == "darwin" && runtime.GOARCH == "arm64") ||
		(runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")) ||
		(runtime.GOOS == "windows" && runtime.GOARCH == "amd64")
}

func findPythonBinary() (string, error) {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return findExistingFile(
		"/opt/homebrew/bin/python3",
		"/usr/local/bin/python3",
		"/usr/bin/python3",
	)
}

func athenaHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "athena")
	}
	return filepath.Join(home, ".athena")
}

func diffusersModelsPath() string { return filepath.Join(athenaHome(), "models", "diffusers") }

func diffusersModelPath(model string) string {
	return filepath.Join(diffusersModelsPath(), strings.ReplaceAll(model, "/", "--"))
}

func diffusersModelInstalled(model string) bool {
	_, err := os.Stat(filepath.Join(diffusersModelPath(model), ".athena_complete"))
	return err == nil
}

func diffusersVenvPython() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(athenaHome(), "image-runtime", "venv", "Scripts", "python.exe")
	}
	return filepath.Join(athenaHome(), "image-runtime", "venv", "bin", "python")
}

func diffusersRuntimeInstalled() bool {
	_, err := os.Stat(diffusersVenvPython())
	return err == nil
}

func installDiffusersRuntime(python string) error {
	runtimeDir := filepath.Join(athenaHome(), "image-runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return err
	}
	venvDir := filepath.Join(runtimeDir, "venv")
	if output, err := exec.Command(python, "-m", "venv", venvDir).CombinedOutput(); err != nil {
		return fmt.Errorf("创建 Python 隔离环境失败: %s", strings.TrimSpace(string(output)))
	}
	args := []string{"-m", "pip", "install", "--upgrade", "pip", "torch", "torchvision", "diffusers", "transformers", "accelerate", "safetensors", "huggingface_hub", "sentencepiece", "protobuf"}
	if output, err := exec.Command(diffusersVenvPython(), args...).CombinedOutput(); err != nil {
		return fmt.Errorf("安装 Diffusers 依赖失败: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func findOllamaBinary() (string, error) {
	if path, err := exec.LookPath("ollama"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "darwin" {
		candidates := []string{
			"/opt/homebrew/bin/ollama",
			"/usr/local/bin/ollama",
			"/Applications/Ollama.app/Contents/Resources/ollama",
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "ollama"))
		}
		return findExistingFile(candidates...)
	}
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return findExistingFile(filepath.Join(localAppData, "Programs", "Ollama", "ollama.exe"))
		}
	}
	return "", exec.ErrNotFound
}

// Finder and desktop launchers commonly omit package-manager directories from PATH.
func findExistingFile(candidates ...string) (string, error) {
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func findBrewBinary() (string, error) {
	if path, err := exec.LookPath("brew"); err == nil {
		return path, nil
	}
	return findExistingFile("/opt/homebrew/bin/brew", "/usr/local/bin/brew")
}

func findWingetBinary() (string, error) {
	if path, err := exec.LookPath("winget"); err == nil {
		return path, nil
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return findExistingFile(filepath.Join(localAppData, "Microsoft", "WindowsApps", "winget.exe"))
	}
	return "", exec.ErrNotFound
}

func canInstallOllamaRuntime() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := findBrewBinary()
		return err == nil
	case "windows":
		_, err := findWingetBinary()
		return err == nil && runtime.GOARCH == "amd64"
	default:
		return false
	}
}

func installOllamaRuntime() (string, error) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		brew, err := findBrewBinary()
		if err != nil {
			return "", fmt.Errorf("未找到 Homebrew，请先从 https://ollama.com/download 安装 Ollama")
		}
		command = exec.Command(brew, "install", "ollama")
	case "windows":
		winget, err := findWingetBinary()
		if err != nil || runtime.GOARCH != "amd64" {
			return "", fmt.Errorf("当前 Windows 环境不支持自动安装，请从 https://ollama.com/download 安装 Ollama")
		}
		command = exec.Command(winget, "install", "--id", "Ollama.Ollama", "-e", "--silent", "--accept-package-agreements", "--accept-source-agreements")
	default:
		return "", fmt.Errorf("Linux 自动安装需要管理员权限，请先运行 Ollama 官方安装命令")
	}
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("安装 Ollama 失败: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if binary, err := findOllamaBinary(); err == nil {
		return binary, nil
	}
	return "", fmt.Errorf("Ollama 安装完成，但当前进程尚未找到 ollama 命令，请重启 agent-runtime-client")
}

func startOllama(binary string) error {
	logPath := filepath.Join(os.TempDir(), "athena-ollama.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	command := exec.Command(binary, "serve")
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("启动 Ollama 失败: %w", err)
	}
	_ = logFile.Close()
	for attempt := 0; attempt < 40; attempt++ {
		if ollamaRunning(context.Background()) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("Ollama 未在 20 秒内启动，请检查 %s", logPath)
}

func ollamaRunning(ctx context.Context) bool {
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, modelentity.OllamaAPIBase+"/api/tags", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func ollamaModelInstalled(ctx context.Context, model string) bool {
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, modelentity.OllamaAPIBase+"/api/tags", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil {
		return false
	}
	for _, item := range payload.Models {
		if item.Name == model || strings.TrimSuffix(item.Name, ":latest") == strings.TrimSuffix(model, ":latest") {
			return true
		}
	}
	return false
}

func pullOllamaModel(model string, progress func(int, string)) error {
	body, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	req, _ := http.NewRequest(http.MethodPost, modelentity.OllamaAPIBase+"/api/pull", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("连接 Ollama 下载模型失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Ollama 下载失败 (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Status    string `json:"status"`
			Error     string `json:"error"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.Error != "" {
			return fmt.Errorf("Ollama: %s", event.Error)
		}
		percent := 0
		if event.Total > 0 {
			percent = int(event.Completed * 100 / event.Total)
		}
		message := event.Status
		if event.Total > 0 {
			message = fmt.Sprintf("%s %d%%", event.Status, percent)
		}
		progress(percent, message)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func bytesToGB(value uint64) int {
	if value == 0 {
		return 0
	}
	return int((value + (1 << 30) - 1) >> 30)
}

func systemMemoryBytes() (uint64, uint64) {
	switch runtime.GOOS {
	case "darwin":
		return darwinMemoryBytes()
	case "linux":
		return linuxMemoryBytes()
	case "windows":
		return windowsMemoryBytes()
	}
	return 0, 0
}

func darwinMemoryBytes() (uint64, uint64) {
	totalOutput, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0
	}
	total, _ := strconv.ParseUint(strings.TrimSpace(string(totalOutput)), 10, 64)
	vmOutput, err := exec.Command("vm_stat").Output()
	if err != nil {
		return total, 0
	}
	pageSize := uint64(4096)
	availablePages := uint64(0)
	for index, line := range strings.Split(string(vmOutput), "\n") {
		if index == 0 {
			for _, field := range strings.Fields(line) {
				if value, parseErr := strconv.ParseUint(strings.Trim(field, " .()"), 10, 64); parseErr == nil && value >= 4096 {
					pageSize = value
					break
				}
			}
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name != "Pages free" && name != "Pages inactive" && name != "Pages speculative" && name != "Pages purgeable" {
			continue
		}
		pages, _ := strconv.ParseUint(strings.Trim(strings.TrimSpace(parts[1]), "."), 10, 64)
		availablePages += pages
	}
	available := availablePages * pageSize
	if available > total {
		available = total
	}
	return total, available
}

func linuxMemoryBytes() (uint64, uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	values := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) == 0 {
			continue
		}
		value, _ := strconv.ParseUint(fields[0], 10, 64)
		values[parts[0]] = value * 1024
	}
	return values["MemTotal"], values["MemAvailable"]
}

func windowsMemoryBytes() (uint64, uint64) {
	output, err := exec.Command(
		"powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		`$os=Get-CimInstance Win32_OperatingSystem; Write-Output "$($os.TotalVisibleMemorySize) $($os.FreePhysicalMemory)"`,
	).Output()
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 {
		return 0, 0
	}
	totalKB, _ := strconv.ParseUint(fields[0], 10, 64)
	availableKB, _ := strconv.ParseUint(fields[1], 10, 64)
	return totalKB * 1024, availableKB * 1024
}

func ollamaModelsPath() string {
	if path := strings.TrimSpace(os.Getenv("OLLAMA_MODELS")); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if current, currentErr := os.Getwd(); currentErr == nil {
			return current
		}
		return "."
	}
	return filepath.Join(home, ".ollama", "models")
}

func existingStoragePath(path string) string {
	path = filepath.Clean(path)
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

func storageSpaceBytes(path string) (uint64, uint64) {
	path = existingStoragePath(path)
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(path)
		if volume == "" {
			return 0, 0
		}
		command := fmt.Sprintf(`$disk=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='%s'"; Write-Output "$($disk.Size) $($disk.FreeSpace)"`, volume)
		output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).Output()
		if err != nil {
			return 0, 0
		}
		fields := strings.Fields(string(output))
		if len(fields) < 2 {
			return 0, 0
		}
		total, _ := strconv.ParseUint(fields[0], 10, 64)
		available, _ := strconv.ParseUint(fields[1], 10, 64)
		return total, available
	}

	output, err := exec.Command("df", "-Pk", path).Output()
	if err != nil {
		return 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return 0, 0
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, 0
	}
	totalKB, _ := strconv.ParseUint(fields[1], 10, 64)
	availableKB, _ := strconv.ParseUint(fields[3], 10, 64)
	return totalKB * 1024, availableKB * 1024
}
