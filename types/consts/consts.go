// Package consts holds framework-wide constants for agent-runtime-client:
// route paths, defaults, header/metadata keys, and environment variable names.
package consts

const (
	// ServiceName identifies this service in logs and health responses.
	ServiceName = "agent-runtime-client"
	Version     = "1.0.0"

	// ---- HTTP routes (v1 slice) ----
	RouteHealth      = "/healthz"
	RouteHealthReady = "/health/ready"
	RouteHealthAlive = "/health/alive"
	RouteRun         = "/v1/run"
	RouteRunStream   = "/v1/run/stream"
	RouteAgent       = "/v1/agent"
	RouteAgentStream = "/v1/agent/stream"

	// ---- Model role ----
	// ModelRoleDefault is the key under RunRequest.models the runtime reads first.
	ModelRoleDefault = "default"

	// ---- Trace propagation ----
	// HeaderTraceID is the inbound/outbound HTTP header carrying the trace id.
	HeaderTraceID = "X-Trace-Id"
	// HeaderRequestID is accepted for compatibility with common HTTP clients.
	HeaderRequestID = "X-Request-Id"
	// HeaderCorrelationID is accepted for compatibility with gateway/proxy tracing.
	HeaderCorrelationID = "X-Correlation-Id"
	// HeaderTraceparent is the W3C trace context header.
	HeaderTraceparent = "Traceparent"
	// MetaKeyTraceID is the gRPC metadata key forwarded to agent-runtime.
	MetaKeyTraceID = "x-trace-id"
	// CtxKeyTraceID is the gin context key under which the trace id is stored.
	CtxKeyTraceID = "trace_id"
	// CtxKeyUserID is populated by the authentication middleware.
	CtxKeyUserID = "user_id"
	// CtxKeyAdminLevel is greater than zero for administrators.
	CtxKeyAdminLevel = "admin_level"
	// CtxKeyTokenHash identifies the current database-backed login session.
	CtxKeyTokenHash = "auth_token_hash"

	// ---- Runtime context payload ----
	ContextKeySessionID = "session_id"
	ContextKeyUserID    = "user_id"
	ContextKeyAgentID   = "agent_id"

	// ---- Defaults ----
	DefaultHTTPAddr                     = ":8090"
	DefaultRuntimeGRPCAddr              = "localhost:18080"
	DefaultRuntimeHTTPAddr              = "http://127.0.0.1:18081"
	DefaultRequestTimeoutSec            = 60
	DefaultDialTimeoutSec               = 5
	DefaultScheduledTaskScanIntervalSec = 60
	DefaultOrchestrationScanIntervalSec = 3
	DefaultOrchestrationConcurrency     = 2
	DefaultGinMode                      = "debug"

	// DefaultPublicPrefix mounts the agent-frame-compatible resource routes so
	// existing agent-frame clients can target this service without changes.
	DefaultPublicPrefix = "/api/xiaoqinglong/agent-frame/v1"

	// ---- SSE ----
	SSEEventDelta       = "delta"
	SSEEventMeta        = "meta"
	SSEEventToolCall    = "tool_call"
	SSEEventToolResult  = "tool_result"
	SSEEventInterrupted = "interrupted"
	SSEEventError       = "error"
	SSEEventDone        = "done"

	// ---- Environment overrides ----
	EnvConfigPath                   = "ARC_CONFIG_PATH"
	EnvHTTPAddr                     = "ARC_HTTP_ADDR"
	EnvRuntimeGRPCAddr              = "ARC_RUNTIME_GRPC_ADDR"
	EnvRuntimeHTTPAddr              = "ARC_RUNTIME_HTTP_ADDR"
	EnvGinMode                      = "ARC_GIN_MODE"
	EnvPublicPrefix                 = "ARC_PUBLIC_PREFIX"
	EnvDefaultModel                 = "ARC_DEFAULT_MODEL"
	EnvDefaultAPIKey                = "ARC_DEFAULT_API_KEY"
	EnvDefaultAPIBase               = "ARC_DEFAULT_API_BASE"
	EnvDefaultProvider              = "ARC_DEFAULT_PROVIDER"
	EnvScheduledTaskScanIntervalSec = "ARC_SCHEDULED_TASK_SCAN_INTERVAL_SEC"
	EnvOrchestrationEnabled         = "ARC_ORCHESTRATION_ENABLED"
	EnvOrchestrationScanIntervalSec = "ARC_ORCHESTRATION_SCAN_INTERVAL_SEC"
	EnvOrchestrationConcurrency     = "ARC_ORCHESTRATION_MAX_CONCURRENT_RUNS"
	EnvInternalServiceToken         = "ATHENA_INTERNAL_SERVICE_TOKEN"
	EnvHFHome                       = "HF_HOME"
	EnvOllamaModels                 = "OLLAMA_MODELS"

	// ---- Internal headers ----
	HeaderAthenaInternalToken = "X-Athena-Internal-Token"

	// ---- Local Athena filesystem layout ----
	DefaultAthenaHomeDirName        = ".athena"
	DefaultAthenaTempDirName        = "athena"
	DirModels                       = "models"
	DirDiffusers                    = "diffusers"
	DirImageRuntime                 = "image-runtime"
	DirVenv                         = "venv"
	DirLogs                         = "logs"
	DirHuggingFace                  = "huggingface"
	DirModelTraining                = "model-training"
	DiffusersCompleteFileName       = ".athena_complete"
	OllamaStartupLogFileName        = "athena-ollama.log"
	DefaultModelTrainingTempDirName = "athena-model-training"
	DefaultOllamaHomeDirName        = ".ollama"

	// ---- Database overrides ----
	EnvDBType     = "ARC_DB_TYPE"
	EnvDBHost     = "ARC_DB_HOST"
	EnvDBPort     = "ARC_DB_PORT"
	EnvDBUser     = "ARC_DB_USER"
	EnvDBPassword = "ARC_DB_PASSWORD"
	EnvDBName     = "ARC_DB_NAME"
)
