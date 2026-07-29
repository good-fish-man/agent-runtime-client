// Package boot is the composition root. It loads config and wires the layers:
// config -> infra gRPC client/gateway -> domain service -> application service ->
// HTTP handler -> Gin engine. This is the only place infra is constructed and
// injected into the domain, keeping the domain free of infra imports.
package boot

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	httpapi "github.com/good-fish-man/agent-runtime-client/api/http"
	handler "github.com/good-fish-man/agent-runtime-client/api/http/handler/runtime"
	"github.com/good-fish-man/agent-runtime-client/api/http/middleware"
	"github.com/good-fish-man/agent-runtime-client/api/http/router/public"
	appsvc "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	"github.com/good-fish-man/agent-runtime-client/config"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	agentdsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/agent"
	modeldsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/model"
	dsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	"github.com/good-fish-man/agent-runtime-client/infra/db"
	"github.com/good-fish-man/agent-runtime-client/infra/repository/migration"
	runtimerepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/runtime"
	inruntime "github.com/good-fish-man/agent-runtime-client/infra/runtime"
	"github.com/good-fish-man/agent-runtime-client/pkg/log"

	agenthandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/agent"
	callbackhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/callback"
	channelhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/channel"
	commandhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/command"
	confighandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/config"
	dashboardhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/dashboard"
	jobhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/job"
	kbhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/knowledge_base"
	memoryhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/memory"
	modelhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/model"
	skillhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/skill"
	userhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/user"
	voiceavatarhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/voiceavatar"
	weixinhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/weixin"
	workspacehandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/workspace"

	agentsvc "github.com/good-fish-man/agent-runtime-client/application/service/agent"
	channelsvc "github.com/good-fish-man/agent-runtime-client/application/service/channel"
	jobsvc "github.com/good-fish-man/agent-runtime-client/application/service/job"
	kbsvc "github.com/good-fish-man/agent-runtime-client/application/service/knowledge_base"
	memorysvc "github.com/good-fish-man/agent-runtime-client/application/service/memory"
	modelsvc "github.com/good-fish-man/agent-runtime-client/application/service/model"
	skillsvc "github.com/good-fish-man/agent-runtime-client/application/service/skill"
	usersvc "github.com/good-fish-man/agent-runtime-client/application/service/user"
)

// App holds the wired application ready to serve.
type App struct {
	Cfg     *config.Config
	Engine  *gin.Engine
	Restart <-chan struct{}

	client *inruntime.Client
	data   *data.Data
}

// Init builds the App from the config at cfgPath (empty uses defaults+env).
func Init(cfgPath string) (*App, error) {
	resolvedConfigPath := config.ResolvePath(cfgPath)
	cfg, err := config.Load(resolvedConfigPath)
	if err != nil {
		return nil, err
	}
	applyLogLevel(cfg.Log.Level)

	// Optional shared database (CRUD resources). Absent config keeps the
	// service running as a pure runtime client.
	var store *data.Data
	if cfg.DB.Enabled() {
		gdb, err := db.New(cfg.DB)
		if err != nil {
			return nil, err
		}
		store = data.New(gdb)
	}

	client, err := inruntime.NewClient(
		cfg.Runtime.GRPCAddr,
		time.Duration(cfg.Runtime.RequestTimeoutSec)*time.Second,
	)
	if err != nil {
		return nil, err
	}

	gateway := inruntime.NewGateway(client)
	domainSvc := dsrv.NewRuntimeSvc(gateway, defaultModel(cfg))
	var runtimeAgentSvc *agentdsrv.SysAgentSvc
	var runtimeModelSvc *modeldsrv.SysModelSvc
	if store != nil {
		runtimeAgentSvc = agentdsrv.NewSysAgentSvc(store)
		runtimeModelSvc = modeldsrv.NewSysModelSvc(store)
	}
	var runtimeMemorySvc *memorysvc.Service
	if store != nil {
		runtimeMemorySvc = memorysvc.NewService(store)
	}
	appService := appsvc.NewRuntimeService(domainSvc, runtimeAgentSvc, runtimeModelSvc, runtimeMemorySvc)
	if store != nil {
		appService = appsvc.NewRuntimeService(domainSvc, runtimeAgentSvc, runtimeModelSvc, runtimeMemorySvc, runtimerepo.NewMediaJobRepo(store))
	}
	h := handler.NewHandler(appService)

	restart := make(chan struct{}, 1)
	paths := cfg.Paths
	if paths.AppConfigFile == "" && resolvedConfigPath != "" {
		paths.AppConfigFile = resolvedConfigPath
	}
	pub := buildPublicHandlers(cfg, store, appService, paths, restart)
	engine := httpapi.NewEngine(h, pub, cfg.Server.PublicPrefix, cfg.Server.Mode)

	return &App{Cfg: cfg, Engine: engine, Restart: restart, client: client, data: store}, nil
}

// buildPublicHandlers wires the agent-frame-compatible resource handlers. The
// DB-backed handlers are only constructed when the shared database is enabled;
// the file-backed config handler and the channel skeletons are always available.
func buildPublicHandlers(cfg *config.Config, store *data.Data, runtimeService *appsvc.RuntimeService, paths config.PathsConfig, restart chan<- struct{}) *public.Handlers {
	pub := &public.Handlers{
		Config:    confighandler.NewHandler(paths, restart).WithRuntime(cfg.Runtime.HTTPAddr).WithService(cfg.Server.HTTPAddr),
		Callback:  callbackhandler.NewHandler(),
		Weixin:    weixinhandler.NewHandler(),
		Workspace: workspacehandler.NewHandler(),
	}

	if store != nil {
		modelService := modelsvc.NewSysModelService(store)
		if err := migration.InitTables(context.Background(), store); err != nil {
			log.Warnf("init database tables failed: %v", err)
		} else if err := runtimeService.FailInterruptedMediaJobs(context.Background()); err != nil {
			log.Warnf("recover interrupted media jobs failed: %v", err)
		}
		pub.User = userhandler.NewHandler(usersvc.NewSysUserService(store)).WithAvatarStorage(paths.UploadsDir, cfg.Server.PublicPrefix)
		pub.VoiceAvatar = voiceavatarhandler.NewHandler(paths.UploadsDir, cfg.Server.PublicPrefix)
		pub.Auth = middleware.Auth(store)
		pub.Model = modelhandler.NewHandler(modelService).WithRuntime(cfg.Runtime.HTTPAddr).WithTraining(store, runtimeService, paths.UploadsDir)
		pub.Memory = memoryhandler.NewHandler(memorysvc.NewService(store))
		pub.KB = kbhandler.NewHandler(kbsvc.NewSysKnowledgeBaseService(store))
		pub.Skill = skillhandler.NewHandler(skillsvc.NewSysSkillService(store))
		agentService := agentsvc.NewSysAgentService(store)
		pub.Agent = agenthandler.NewHandler(agentService)
		pub.Channel = channelhandler.NewHandler(channelsvc.NewSysChannelService(store))
		pub.Command = commandhandler.NewHandler(runtimeService, agentService)
		pub.Dashboard = dashboardhandler.NewHandler(store)
		pub.Job = jobhandler.NewHandler(jobsvc.NewJobExecutionService(store))
	}

	return pub
}

// PingRuntime performs a bounded startup health probe (non-fatal for callers).
func (a *App) PingRuntime() (*entity.HealthStatus, error) {
	return a.client.Ping(context.Background(), time.Duration(a.Cfg.Runtime.DialTimeoutSec)*time.Second)
}

// Close releases resources (the gRPC connection).
func (a *App) Close() error {
	if a.client != nil {
		return a.client.Close()
	}
	return nil
}

// defaultModel builds the fallback model from config, or nil if none is set.
func defaultModel(cfg *config.Config) *entity.ModelConfig {
	m := cfg.Model
	if strings.TrimSpace(m.Name) == "" {
		return nil
	}
	return &entity.ModelConfig{
		Provider: m.Provider,
		Name:     m.Name,
		APIKey:   m.APIKey,
		APIBase:  m.APIBase,
	}
}

func applyLogLevel(level string) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		log.SetLogLevel(log.DebugLevel)
	case "info":
		log.SetLogLevel(log.InfoLevel)
	case "warn", "warning":
		log.SetLogLevel(log.WarnLevel)
	case "error":
		log.SetLogLevel(log.ErrorLevel)
	}
}
