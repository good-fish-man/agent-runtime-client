package model

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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

	modeldto "github.com/good-fish-man/agent-runtime-client/application/dto/model"
	runtimedto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	runtimesvc "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	modelentity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	modelpo "github.com/good-fish-man/agent-runtime-client/infra/repository/po/model"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
	log "github.com/good-fish-man/logx"
)

const maxTrainingDatasetBytes = 100 << 20

type modelTrainingManager struct {
	data       *data.Data
	runtimeSvc *runtimesvc.RuntimeService
	root       string
	mu         sync.Mutex
	cancels    map[string]context.CancelFunc
}

type trainingDatasetRow struct {
	Messages   []runtimeentity.ChatMessage `json:"messages"`
	Prompt     string                      `json:"prompt"`
	Completion string                      `json:"completion"`
}

func newModelTrainingManager(ctx context.Context, store *data.Data, runtimeSvc *runtimesvc.RuntimeService, configuredRoot string) *modelTrainingManager {
	if ctx == nil {
		ctx = context.Background()
	}
	root := strings.TrimSpace(configuredRoot)
	if root == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			root = filepath.Join(cache, consts.DefaultAthenaTempDirName, consts.DirModelTraining)
		} else {
			root = filepath.Join(os.TempDir(), consts.DefaultModelTrainingTempDirName)
		}
	} else {
		root = filepath.Join(root, consts.DirModelTraining)
	}
	_ = os.MkdirAll(root, 0o700)
	manager := &modelTrainingManager{data: store, runtimeSvc: runtimeSvc, root: root, cancels: make(map[string]context.CancelFunc)}
	store.DB(ctx).Model(&modelpo.ModelTrainingJob{}).
		Where("status IN ? AND deleted_at = 0", []string{"queued", "running"}).
		Updates(map[string]any{"status": "failed", "stage": "interrupted", "error_msg": "服务重启导致训练中断，请重新创建任务", "finished_at": time.Now().UnixMilli()})
	return manager
}

func (h *Handler) WithTraining(ctx context.Context, store *data.Data, runtimeSvc *runtimesvc.RuntimeService, root string) *Handler {
	if store != nil && runtimeSvc != nil {
		h.training = newModelTrainingManager(ctx, store, runtimeSvc, root)
	}
	return h
}

func (h *Handler) ModelTrainingEnvironment(c *gin.Context) {
	if h.training == nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("训练服务未启用"))
		return
	}
	response.Ok(c, h.training.environment())
}

func (h *Handler) CreateModelTraining(c *gin.Context) {
	if h.training == nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("训练服务未启用"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTrainingDatasetBytes+(1<<20))
	var req modeldto.CreateModelTrainingReq
	if err := c.ShouldBind(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("训练参数或数据集无效: " + err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if req.Mode == "distill" && strings.TrimSpace(req.TeacherModelID) == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("蒸馏模式必须选择教师模型"))
		return
	}
	if req.Mode == "distill" && !req.AcknowledgeDistillation {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("请先确认蒸馏数据将发送给所选教师模型"))
		return
	}
	file, header, err := c.Request.FormFile("dataset")
	if err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("请选择 JSONL 数据集"))
		return
	}
	defer file.Close()

	userID := c.GetString("user_id")
	job, err := h.training.create(c.Request.Context(), h.svc, userID, &req, file, header)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusAccepted, trainingJobResponse(job, false))
}

func (h *Handler) ListModelTrainings(c *gin.Context) {
	if h.training == nil {
		response.Ok(c, []*modeldto.ModelTrainingJobRsp{})
		return
	}
	jobs, err := h.training.list(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]*modeldto.ModelTrainingJobRsp, 0, len(jobs))
	for index := range jobs {
		items = append(items, trainingJobResponse(&jobs[index], false))
	}
	response.Ok(c, items)
}

func (h *Handler) GetModelTraining(c *gin.Context) {
	if h.training == nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("训练服务未启用"))
		return
	}
	job, err := h.training.get(c.Request.Context(), c.Param("job_id"), c.GetString("user_id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, trainingJobResponse(job, true))
}

func (h *Handler) CancelModelTraining(c *gin.Context) {
	if h.training == nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("训练服务未启用"))
		return
	}
	if err := h.training.cancel(c.Request.Context(), c.Param("job_id"), c.GetString("user_id")); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

func (m *modelTrainingManager) environment() *modeldto.ModelTrainingEnvironmentRsp {
	backend, supported, accelerator, message := detectTrainingBackend()
	python := m.trainingPython()
	pythonInstalled := python != ""
	ready := pythonInstalled && trainingModuleReady(python, backend)
	if supported && !pythonInstalled {
		message = "未找到 Python 3，安装后才能创建训练环境"
	} else if supported && !ready {
		message = "首次训练会自动创建隔离环境并安装训练依赖"
	} else if supported {
		message = "训练环境已就绪"
	}
	return &modeldto.ModelTrainingEnvironmentRsp{
		OS: runtime.GOOS, Arch: runtime.GOARCH, Backend: backend, Supported: supported,
		PythonInstalled: pythonInstalled, DependenciesReady: ready, AcceleratorAvailable: accelerator, Message: message,
	}
}

func detectTrainingBackend() (string, bool, bool, string) {
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "mlx", true, true, "Apple Silicon 将使用 MLX LoRA"
	case runtime.GOOS == "linux":
		if commandAvailable("nvidia-smi") {
			return "cuda", true, true, "NVIDIA GPU 将使用 TRL/PEFT LoRA"
		}
		return "cuda", false, false, "Linux 微调需要 NVIDIA GPU 和可用的 CUDA 驱动"
	case runtime.GOOS == "windows":
		return "wsl-cuda", false, false, "Windows 请在启用 NVIDIA CUDA 的 WSL2 中运行服务"
	default:
		return "", false, false, "当前系统暂不支持本地模型微调"
	}
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (m *modelTrainingManager) venvPython() string {
	name := "python"
	if runtime.GOOS == "windows" {
		return filepath.Join(m.root, ".venv", "Scripts", "python.exe")
	}
	return filepath.Join(m.root, ".venv", "bin", name)
}

func (m *modelTrainingManager) trainingPython() string {
	if path := m.venvPython(); fileExists(path) {
		return path
	}
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func trainingModuleReady(python, backend string) bool {
	module := "mlx_lm"
	if backend == "cuda" {
		module = "torch, trl, peft, datasets"
	}
	return exec.Command(python, "-c", "import "+module).Run() == nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (m *modelTrainingManager) create(ctx context.Context, svc modelService, userID string, req *modeldto.CreateModelTrainingReq, file multipart.File, header *multipart.FileHeader) (*modelpo.ModelTrainingJob, error) {
	student, err := svc.FindSysModelById(ctx, &modeldto.FindSysModelByIdReq{Ulid: req.StudentModelID, UserID: userID})
	if err != nil {
		return nil, err
	}
	if student.ModelType != modelentity.ModelTypeLLM || !strings.EqualFold(student.Provider, modelentity.ProviderOllama) {
		return nil, apierror.ErrBadRequest.WithMessage("当前仅支持对用户自己的 Ollama 文本模型执行 LoRA 微调")
	}
	backend, supported, _, message := detectTrainingBackend()
	if !supported {
		return nil, apierror.ErrBadRequest.WithMessage(message)
	}
	if _, err := trainingBaseModel(student.Name); err != nil {
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	var teacherName string
	if req.Mode == "distill" {
		teacher, findErr := svc.FindSysModelById(ctx, &modeldto.FindSysModelByIdReq{Ulid: req.TeacherModelID, UserID: userID})
		if findErr != nil {
			return nil, findErr
		}
		if teacher.ModelType != "llm" || teacher.Ulid == student.Ulid {
			return nil, apierror.ErrBadRequest.WithMessage("教师模型必须是另一个可用的文本大模型")
		}
		teacherName = teacher.Name
	}

	config := normalizeTrainingConfig(req)
	configJSON, _ := json.Marshal(config)
	jobID := ulid.New()
	jobDir := filepath.Join(m.root, safePathSegment(userID), jobID)
	if err := os.MkdirAll(filepath.Join(jobDir, "data"), 0o700); err != nil {
		return nil, err
	}
	sourcePath := filepath.Join(jobDir, "source.jsonl")
	count, err := saveAndValidateDataset(file, sourcePath, req.Mode, config.MaxSamples)
	if err != nil {
		_ = os.RemoveAll(jobDir)
		return nil, apierror.ErrBadRequest.WithMessage(err.Error())
	}
	if req.Mode == "fine_tune" {
		if err := copyPrivateFile(sourcePath, filepath.Join(jobDir, "data", "train.jsonl")); err != nil {
			_ = os.RemoveAll(jobDir)
			return nil, err
		}
	}
	job := &modelpo.ModelTrainingJob{
		Ulid: jobID, UserID: userID, Mode: req.Mode, Name: strings.TrimSpace(req.Name),
		StudentModelID: student.Ulid, StudentModelName: student.Name,
		TeacherModelID: req.TeacherModelID, TeacherModelName: teacherName,
		DatasetPath: sourcePath, DatasetOriginalName: filepath.Base(header.Filename),
		OutputName: normalizeOllamaName(req.OutputName), Backend: backend,
		Status: "queued", Stage: "queued", Progress: 0, SampleCount: count, ConfigJSON: string(configJSON),
	}
	if job.OutputName == "" {
		_ = os.RemoveAll(jobDir)
		return nil, apierror.ErrBadRequest.WithMessage("产物模型名称只能包含字母、数字、点、下划线、短横线、冒号或斜杠")
	}
	if strings.EqualFold(job.OutputName, student.Name) {
		_ = os.RemoveAll(jobDir)
		return nil, apierror.ErrBadRequest.WithMessage("产物模型必须使用新名称，不能覆盖基础模型")
	}
	if ollamaRunning(ctx) && ollamaModelInstalled(ctx, job.OutputName) {
		_ = os.RemoveAll(jobDir)
		return nil, apierror.ErrBadRequest.WithMessage("同名 Ollama 模型已经存在，请更换产物名称")
	}
	var duplicateCount int64
	if err := m.data.DB(ctx).Model(&modelpo.ModelTrainingJob{}).
		Where("user_id = ? AND output_name = ? AND deleted_at = 0 AND status IN ?", userID, job.OutputName, []string{"queued", "running", "completed"}).
		Count(&duplicateCount).Error; err != nil {
		_ = os.RemoveAll(jobDir)
		return nil, err
	}
	if duplicateCount > 0 {
		_ = os.RemoveAll(jobDir)
		return nil, apierror.ErrBadRequest.WithMessage("同名训练产物已经存在或正在生成，请更换名称")
	}
	if err := m.data.DB(ctx).Create(job).Error; err != nil {
		_ = os.RemoveAll(jobDir)
		return nil, err
	}
	backgroundCtx := context.WithoutCancel(ctx)
	runCtx, cancel := context.WithCancel(authctx.WithUserID(backgroundCtx, userID))
	m.mu.Lock()
	m.cancels[jobID] = cancel
	m.mu.Unlock()
	log.Go(runCtx, func(workerCtx context.Context) {
		m.run(workerCtx, job, config)
	})
	return job, nil
}

// modelService keeps the manager testable without coupling it to the concrete service.
type modelService interface {
	FindSysModelById(context.Context, *modeldto.FindSysModelByIdReq) (*modeldto.FindSysModelRsp, error)
	CreateSysModel(context.Context, *modeldto.CreateSysModelReq) (*modeldto.CreateSysModelRsp, error)
}

func normalizeTrainingConfig(req *modeldto.CreateModelTrainingReq) modeldto.ModelTrainingConfig {
	config := modeldto.ModelTrainingConfig{Iterations: req.Iterations, BatchSize: req.BatchSize, LearningRate: req.LearningRate, LoraRank: req.LoraRank, MaxSamples: req.MaxSamples}
	if config.Iterations <= 0 || config.Iterations > 10000 {
		config.Iterations = 600
	}
	if config.BatchSize <= 0 || config.BatchSize > 16 {
		config.BatchSize = 1
	}
	if config.LearningRate <= 0 || config.LearningRate > 0.1 {
		config.LearningRate = 1e-5
	}
	if config.LoraRank <= 0 || config.LoraRank > 256 {
		config.LoraRank = 8
	}
	if config.MaxSamples <= 0 || config.MaxSamples > 10000 {
		config.MaxSamples = 500
	}
	return config
}

func saveAndValidateDataset(source io.Reader, path, mode string, maxSamples int) (int, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(source, maxTrainingDatasetBytes+1))
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row trainingDatasetRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return 0, fmt.Errorf("数据集第 %d 条不是有效 JSON: %w", count+1, err)
		}
		if err := validateTrainingRow(row, mode); err != nil {
			return 0, fmt.Errorf("数据集第 %d 条无效: %w", count+1, err)
		}
		if count >= maxSamples {
			break
		}
		if _, err := file.WriteString(line + "\n"); err != nil {
			return 0, err
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if count < 2 {
		return 0, errors.New("训练数据集至少需要 2 条有效样本")
	}
	return count, nil
}

func validateTrainingRow(row trainingDatasetRow, mode string) error {
	if mode == "distill" {
		if strings.TrimSpace(row.Prompt) != "" || lastUserMessage(row.Messages) != "" {
			return nil
		}
		return errors.New("蒸馏数据需要 prompt 或至少一条 user 消息")
	}
	if strings.TrimSpace(row.Prompt) != "" && strings.TrimSpace(row.Completion) != "" {
		return nil
	}
	if len(row.Messages) >= 2 && row.Messages[len(row.Messages)-1].Role == "assistant" && strings.TrimSpace(row.Messages[len(row.Messages)-1].Content) != "" {
		return nil
	}
	return errors.New("微调数据需要 prompt/completion，或以 assistant 消息结尾的 messages")
}

func lastUserMessage(messages []runtimeentity.ChatMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" && strings.TrimSpace(messages[index].Content) != "" {
			return strings.TrimSpace(messages[index].Content)
		}
	}
	return ""
}

func copyPrivateFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func safePathSegment(value string) string {
	var out strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			out.WriteRune(char)
		}
	}
	if out.Len() == 0 {
		return "user"
	}
	return out.String()
}

func normalizeOllamaName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("._-:/", char) {
			out.WriteRune(char)
		}
	}
	return strings.Trim(out.String(), "./:-")
}

func trainingBaseModel(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	known := map[string]string{
		"qwen3:0.6b": "Qwen/Qwen3-0.6B", "qwen3:1.7b": "Qwen/Qwen3-1.7B",
		"qwen3:4b": "Qwen/Qwen3-4B", "qwen3:8b": "Qwen/Qwen3-8B",
		"gemma3:1b": "google/gemma-3-1b-it", "gemma3:4b": "google/gemma-3-4b-it",
	}
	if value := known[normalized]; value != "" {
		return value, nil
	}
	return "", fmt.Errorf("模型 %s 暂无可信的训练源映射；当前支持 Qwen3 0.6B/1.7B/4B/8B 和 Gemma3 1B/4B", name)
}

func (m *modelTrainingManager) list(ctx context.Context, userID string) ([]modelpo.ModelTrainingJob, error) {
	var jobs []modelpo.ModelTrainingJob
	err := m.data.DB(ctx).Where("user_id = ? AND deleted_at = 0", userID).Order("created_at DESC").Limit(100).Find(&jobs).Error
	return jobs, err
}

func (m *modelTrainingManager) get(ctx context.Context, jobID, userID string) (*modelpo.ModelTrainingJob, error) {
	var job modelpo.ModelTrainingJob
	if err := m.data.DB(ctx).Where("ulid = ? AND user_id = ? AND deleted_at = 0", jobID, userID).First(&job).Error; err != nil {
		return nil, apierror.ErrNotFound.WithMessage("训练任务不存在")
	}
	return &job, nil
}

func (m *modelTrainingManager) cancel(ctx context.Context, jobID, userID string) error {
	job, err := m.get(ctx, jobID, userID)
	if err != nil {
		return err
	}
	if job.Status != "queued" && job.Status != "running" {
		return apierror.ErrBadRequest.WithMessage("当前任务已经结束")
	}
	m.mu.Lock()
	cancel := m.cancels[jobID]
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return m.data.DB(ctx).Model(&modelpo.ModelTrainingJob{}).Where("ulid = ? AND user_id = ?", jobID, userID).
		Updates(map[string]any{"status": "canceled", "stage": "canceled", "error_msg": "用户已取消任务", "finished_at": time.Now().UnixMilli()}).Error
}

func trainingJobResponse(job *modelpo.ModelTrainingJob, includeLog bool) *modeldto.ModelTrainingJobRsp {
	result := &modeldto.ModelTrainingJobRsp{
		Ulid: job.Ulid, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Mode: job.Mode, Name: job.Name,
		StudentModelID: job.StudentModelID, StudentModelName: job.StudentModelName,
		TeacherModelID: job.TeacherModelID, TeacherModelName: job.TeacherModelName,
		DatasetOriginalName: job.DatasetOriginalName, OutputName: job.OutputName, OutputModelID: job.OutputModelID,
		Backend: job.Backend, Status: job.Status, Stage: job.Stage, Progress: job.Progress,
		SampleCount: job.SampleCount, ConfigJSON: job.ConfigJSON, MetricsJSON: job.MetricsJSON,
		ErrorMsg: job.ErrorMsg, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt,
	}
	if includeLog {
		result.LogText = job.LogText
	}
	return result
}

func (m *modelTrainingManager) update(ctx context.Context, jobID string, values map[string]any) {
	m.data.DB(ctx).Model(&modelpo.ModelTrainingJob{}).Where("ulid = ?", jobID).Updates(values)
}

func (m *modelTrainingManager) run(ctx context.Context, job *modelpo.ModelTrainingJob, config modeldto.ModelTrainingConfig) {
	defer func() { m.mu.Lock(); delete(m.cancels, job.Ulid); m.mu.Unlock() }()
	m.update(ctx, job.Ulid, map[string]any{"status": "running", "stage": "preparing", "progress": 2, "started_at": time.Now().UnixMilli()})
	fail := func(err error) {
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		m.update(ctx, job.Ulid, map[string]any{"status": "failed", "stage": "failed", "error_msg": err.Error(), "finished_at": time.Now().UnixMilli()})
	}
	python, err := m.ensureTrainingRuntime(ctx, job)
	if err != nil {
		fail(err)
		return
	}
	jobDir := filepath.Dir(job.DatasetPath)
	if job.Mode == "distill" {
		if err := m.distillDataset(ctx, job, filepath.Join(jobDir, "data", "train.jsonl"), config.MaxSamples); err != nil {
			fail(err)
			return
		}
	}
	baseModel, err := trainingBaseModel(job.StudentModelName)
	if err != nil {
		fail(err)
		return
	}
	m.update(ctx, job.Ulid, map[string]any{"stage": "training", "progress": 45})
	adapterPath := filepath.Join(jobDir, "adapter")
	fusedPath := filepath.Join(jobDir, "fused-model")
	logText, err := m.runTrainingCommand(ctx, python, job.Backend, baseModel, filepath.Join(jobDir, "data"), adapterPath, fusedPath, config)
	m.update(ctx, job.Ulid, map[string]any{"log_text": truncateLog(logText)})
	if err != nil {
		fail(fmt.Errorf("LoRA 训练失败: %w", err))
		return
	}
	m.update(ctx, job.Ulid, map[string]any{"stage": "importing", "progress": 88})
	if err := importOllamaModel(ctx, job.OutputName, fusedPath, jobDir); err != nil {
		fail(err)
		return
	}
	created, err := m.createOutputModel(ctx, job)
	if err != nil {
		fail(err)
		return
	}
	m.update(ctx, job.Ulid, map[string]any{"status": "completed", "stage": "complete", "progress": 100, "output_model_id": created.Ulid, "finished_at": time.Now().UnixMilli()})
}

func (m *modelTrainingManager) ensureTrainingRuntime(ctx context.Context, job *modelpo.ModelTrainingJob) (string, error) {
	python := m.trainingPython()
	if python == "" {
		return "", errors.New("未找到 Python 3")
	}
	if trainingModuleReady(python, job.Backend) {
		return python, nil
	}
	m.update(ctx, job.Ulid, map[string]any{"stage": "dependencies", "progress": 5})
	venv := filepath.Join(m.root, ".venv")
	if !fileExists(m.venvPython()) {
		if output, err := exec.CommandContext(ctx, python, "-m", "venv", venv).CombinedOutput(); err != nil {
			return "", fmt.Errorf("创建 Python 隔离环境失败: %s", strings.TrimSpace(string(output)))
		}
	}
	python = m.venvPython()
	packages := []string{"mlx-lm[train]"}
	if job.Backend == "cuda" {
		packages = []string{"torch", "transformers", "datasets", "trl", "peft", "accelerate", "bitsandbytes"}
	}
	args := append([]string{"-m", "pip", "install", "--disable-pip-version-check"}, packages...)
	output, err := exec.CommandContext(ctx, python, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("安装训练依赖失败: %s", strings.TrimSpace(string(output)))
	}
	return python, nil
}

func (m *modelTrainingManager) distillDataset(ctx context.Context, job *modelpo.ModelTrainingJob, target string, maxSamples int) error {
	m.update(ctx, job.Ulid, map[string]any{"stage": "distilling", "progress": 15})
	source, err := os.Open(job.DatasetPath)
	if err != nil {
		return err
	}
	defer source.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	index := 0
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var row trainingDatasetRow
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		prompt := strings.TrimSpace(row.Prompt)
		if prompt == "" {
			prompt = lastUserMessage(row.Messages)
		}
		result, callErr := m.runtimeSvc.RunAgent(ctx, &runtimedto.AgentReq{
			Task:    "你是负责知识蒸馏的教师模型。请针对下面的用户请求给出准确、完整、可直接用于训练学生模型的答案。只输出最终答案，不要解释蒸馏过程。\n\n用户请求：\n" + prompt,
			Models:  map[string]runtimeentity.ModelConfig{"default": {ExtraFields: map[string]any{"model_id": job.TeacherModelID}}},
			Context: map[string]any{"training_job_id": job.Ulid},
		})
		if callErr != nil {
			return fmt.Errorf("教师模型生成第 %d 条样本失败: %w", index+1, callErr)
		}
		if result == nil || strings.TrimSpace(result.Content) == "" {
			return fmt.Errorf("教师模型第 %d 条样本返回空内容", index+1)
		}
		generated := trainingDatasetRow{Messages: []runtimeentity.ChatMessage{{Role: "user", Content: prompt}, {Role: "assistant", Content: strings.TrimSpace(result.Content)}}}
		line, _ := json.Marshal(generated)
		if _, err := out.Write(append(line, '\n')); err != nil {
			return err
		}
		index++
		m.update(ctx, job.Ulid, map[string]any{"progress": 15 + index*25/max(1, min(job.SampleCount, maxSamples))})
		if index >= maxSamples {
			break
		}
	}
	return scanner.Err()
}

func (m *modelTrainingManager) runTrainingCommand(ctx context.Context, python, backend, baseModel, dataPath, adapterPath, fusedPath string, config modeldto.ModelTrainingConfig) (string, error) {
	if err := os.MkdirAll(adapterPath, 0o700); err != nil {
		return "", err
	}
	if backend == "mlx" {
		configPath := filepath.Join(filepath.Dir(dataPath), "mlx-config.yaml")
		content := fmt.Sprintf("model: %q\ntrain: true\nfine_tune_type: lora\ndata: %q\nadapter_path: %q\niters: %d\nbatch_size: %d\nlearning_rate: %s\nmask_prompt: true\nlora_parameters:\n  rank: %d\n", baseModel, dataPath, adapterPath, config.Iterations, config.BatchSize, strconv.FormatFloat(config.LearningRate, 'g', -1, 64), config.LoraRank)
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			return "", err
		}
		trainingOutput, err := exec.CommandContext(ctx, python, "-m", "mlx_lm.lora", "--config", configPath).CombinedOutput()
		if err != nil {
			return string(trainingOutput), commandError(err, trainingOutput)
		}
		fuseOutput, err := exec.CommandContext(ctx, python, "-m", "mlx_lm.fuse", "--model", baseModel, "--adapter-path", adapterPath, "--save-path", fusedPath).CombinedOutput()
		combined := string(trainingOutput) + "\n" + string(fuseOutput)
		return combined, commandError(err, fuseOutput)
	}
	scriptPath := filepath.Join(filepath.Dir(dataPath), "train.py")
	if err := os.WriteFile(scriptPath, []byte(cudaTrainingScript), 0o600); err != nil {
		return "", err
	}
	args := []string{scriptPath, "--model", baseModel, "--data", filepath.Join(dataPath, "train.jsonl"), "--output", adapterPath, "--fused", fusedPath, "--steps", strconv.Itoa(config.Iterations), "--batch", strconv.Itoa(config.BatchSize), "--lr", strconv.FormatFloat(config.LearningRate, 'g', -1, 64), "--rank", strconv.Itoa(config.LoraRank)}
	output, err := exec.CommandContext(ctx, python, args...).CombinedOutput()
	return string(output), commandError(err, output)
}

func commandError(err error, output []byte) error {
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if len(message) > 4000 {
		message = message[len(message)-4000:]
	}
	return fmt.Errorf("%w: %s", err, message)
}

func truncateLog(value string) string {
	const limit = 200000
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func importOllamaModel(ctx context.Context, outputName, fusedPath, jobDir string) error {
	binary, _ := findOllamaBinary()
	if binary == "" {
		return errors.New("训练完成，但未找到 Ollama，无法导入产物模型")
	}
	modelfile := filepath.Join(jobDir, "Modelfile")
	content := fmt.Sprintf("FROM %s\n", fusedPath)
	if err := os.WriteFile(modelfile, []byte(content), 0o600); err != nil {
		return err
	}
	output, err := exec.CommandContext(ctx, binary, "create", "--quantize", "q4_K_M", outputName, "-f", modelfile).CombinedOutput()
	if err != nil {
		return fmt.Errorf("融合模型已生成，但 Ollama 导入失败；请确认该模型架构支持 Safetensors 导入: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func (m *modelTrainingManager) createOutputModel(ctx context.Context, job *modelpo.ModelTrainingJob) (*modeldto.CreateSysModelRsp, error) {
	return (&serviceAdapter{manager: m}).CreateSysModel(ctx, &modeldto.CreateSysModelReq{CreatedBy: job.UserID, Name: job.OutputName, Provider: modelentity.ProviderOllamaDisplay, BaseUrl: modelentity.OllamaOpenAIBaseURL, ModelType: modelentity.ModelTypeLLM, Category: "trained"})
}

// serviceAdapter is replaced by the concrete model service callback set by Handler.
type serviceAdapter struct{ manager *modelTrainingManager }

func (s *serviceAdapter) CreateSysModel(ctx context.Context, req *modeldto.CreateSysModelReq) (*modeldto.CreateSysModelRsp, error) {
	model := &modelpo.SysModel{CreatedBy: req.CreatedBy, UpdatedBy: req.CreatedBy, Name: req.Name, Provider: req.Provider, BaseUrl: req.BaseUrl, ModelType: req.ModelType, Category: req.Category, Status: modelentity.ModelStatusActive, Enabled: true, RuntimeMode: modelentity.RuntimeModeOnDemand}
	if err := s.manager.data.DB(ctx).Create(model).Error; err != nil {
		return nil, err
	}
	return &modeldto.CreateSysModelRsp{Ulid: model.Ulid}, nil
}

const cudaTrainingScript = `
import argparse
import torch
from datasets import load_dataset
from peft import LoraConfig
from trl import SFTConfig, SFTTrainer

p = argparse.ArgumentParser()
p.add_argument("--model", required=True)
p.add_argument("--data", required=True)
p.add_argument("--output", required=True)
p.add_argument("--fused", required=True)
p.add_argument("--steps", type=int, default=600)
p.add_argument("--batch", type=int, default=1)
p.add_argument("--lr", type=float, default=1e-5)
p.add_argument("--rank", type=int, default=8)
a = p.parse_args()
dataset = load_dataset("json", data_files=a.data, split="train")
use_bf16 = torch.cuda.is_bf16_supported()
config = SFTConfig(output_dir=a.output, max_steps=a.steps, per_device_train_batch_size=a.batch, learning_rate=a.lr, logging_steps=5, save_strategy="no", report_to="none", bf16=use_bf16, fp16=not use_bf16)
trainer = SFTTrainer(model=a.model, args=config, train_dataset=dataset, peft_config=LoraConfig(r=a.rank, lora_alpha=a.rank * 2, lora_dropout=0.05, bias="none", task_type="CAUSAL_LM"))
trainer.train()
trainer.save_model(a.output)
merged = trainer.model.merge_and_unload()
merged.save_pretrained(a.fused, safe_serialization=True)
if trainer.processing_class is not None:
    trainer.processing_class.save_pretrained(a.fused)
`
