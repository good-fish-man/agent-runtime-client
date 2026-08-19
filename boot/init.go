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
	controlhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/control"
	deploymenthandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/deployment"
	knowledgehandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/knowledge"
	operationshandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/operations"
	orchestrationhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/orchestration"
	pluginregistryhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/pluginregistry"
	handler "github.com/good-fish-man/agent-runtime-client/api/http/handler/runtime"
	"github.com/good-fish-man/agent-runtime-client/api/http/middleware"
	"github.com/good-fish-man/agent-runtime-client/api/http/router/public"
	controlsvc "github.com/good-fish-man/agent-runtime-client/application/service/control"
	delegationsvc "github.com/good-fish-man/agent-runtime-client/application/service/delegation"
	deploymentsvc "github.com/good-fish-man/agent-runtime-client/application/service/deployment"
	experiencesvc "github.com/good-fish-man/agent-runtime-client/application/service/experience"
	knowledgesvc "github.com/good-fish-man/agent-runtime-client/application/service/knowledge"
	learningsvc "github.com/good-fish-man/agent-runtime-client/application/service/learning"
	operationssvc "github.com/good-fish-man/agent-runtime-client/application/service/operations"
	orchestrationsvc "github.com/good-fish-man/agent-runtime-client/application/service/orchestration"
	pluginregistrysvc "github.com/good-fish-man/agent-runtime-client/application/service/pluginregistry"
	appsvc "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	"github.com/good-fish-man/agent-runtime-client/config"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	agentdsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/agent"
	modeldsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/model"
	dsrv "github.com/good-fish-man/agent-runtime-client/domain/srv/runtime"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	"github.com/good-fish-man/agent-runtime-client/infra/db"
	"github.com/good-fish-man/agent-runtime-client/infra/repository/migration"
	controlrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/control"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/delegation"
	deploymentrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/deployment"
	experiencerepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/experience"
	knowledgerepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/knowledge"
	learningrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/learning"
	operationsrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/operations"
	orchestrationrepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/orchestration"
	runtimerepo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/runtime"
	inruntime "github.com/good-fish-man/agent-runtime-client/infra/runtime"
	log "github.com/good-fish-man/logx"

	agenthandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/agent"
	browsercredentialhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/browsercredential"
	callbackhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/callback"
	channelhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/channel"
	commandhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/command"
	confighandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/config"
	dashboardhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/dashboard"
	experiencehandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/experience"
	jobhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/job"
	kbhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/knowledge_base"
	learninghandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/learning"
	memoryhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/memory"
	modelhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/model"
	scheduledtaskhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/scheduledtask"
	skillhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/skill"
	userhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/user"
	voiceavatarhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/voiceavatar"
	weixinhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/weixin"
	workspacehandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/workspace"

	agentsvc "github.com/good-fish-man/agent-runtime-client/application/service/agent"
	browsercredentialsvc "github.com/good-fish-man/agent-runtime-client/application/service/browsercredential"
	channelsvc "github.com/good-fish-man/agent-runtime-client/application/service/channel"
	jobsvc "github.com/good-fish-man/agent-runtime-client/application/service/job"
	kbsvc "github.com/good-fish-man/agent-runtime-client/application/service/knowledge_base"
	memorysvc "github.com/good-fish-man/agent-runtime-client/application/service/memory"
	modelsvc "github.com/good-fish-man/agent-runtime-client/application/service/model"
	scheduledtasksvc "github.com/good-fish-man/agent-runtime-client/application/service/scheduledtask"
	skillsvc "github.com/good-fish-man/agent-runtime-client/application/service/skill"
	usersvc "github.com/good-fish-man/agent-runtime-client/application/service/user"
)

// App holds the wired application ready to serve.
type App struct {
	Cfg     *config.Config
	Engine  *gin.Engine
	Restart <-chan struct{}

	client        *inruntime.Client
	data          *data.Data
	Control       *controlsvc.Hub
	Delegation    *delegationsvc.Orchestrator
	Experience    *experiencesvc.Service
	Deployment    *deploymentsvc.Service
	Knowledge     *knowledgesvc.Service
	Orchestration *orchestrationsvc.Service
	Supervisor    *orchestrationsvc.Supervisor
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
		if err := migration.InitTables(context.Background(), store); err != nil {
			return nil, err
		}
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
		appService = appsvc.NewRuntimeService(domainSvc, runtimeAgentSvc, runtimeModelSvc, runtimeMemorySvc, runtimerepo.NewMediaJobRepo(store)).WithChatRecorder(store)
	}
	var controlHub *controlsvc.Hub
	var experienceService *experiencesvc.Service
	var deploymentService *deploymentsvc.Service
	var knowledgeService *knowledgesvc.Service
	var orchestrationService *orchestrationsvc.Service
	var delegationOrchestrator *delegationsvc.Orchestrator
	var delegationExecution *delegationsvc.ExecutionService
	if store != nil {
		controlStore := controlrepo.NewStore(store)
		if err := controlStore.MarkAllDevicesOffline(context.Background(), time.Now().UTC()); err != nil {
			return nil, err
		}
		if err := controlStore.PauseInterruptedTasks(context.Background(), "control plane restarted before the decision loop completed"); err != nil {
			return nil, err
		}
		controlHub = controlsvc.NewHub(controlStore)
		experienceService = experiencesvc.NewService(experiencerepo.NewStore(store), controlStore)
		deploymentService = deploymentsvc.NewService(deploymentrepo.NewStore(store))
		knowledgeService = knowledgesvc.NewService(knowledgerepo.NewStore(store))
		orchestrationService = orchestrationsvc.NewService(orchestrationrepo.NewStore(store))
		delegationStore := delegationrepo.NewStore(store)
		delegationOrchestrator = delegationsvc.NewOrchestrator(delegationStore, delegationsvc.Config{}, nil)
		delegationExecution = delegationsvc.NewExecutionService(delegationOrchestrator, domainSvc, nil)
		controlHub.OnTaskTerminal(func(_ context.Context, taskID string) { experienceService.Enqueue(taskID) })
	} else {
		controlHub = controlsvc.NewHub()
	}
	appService.SetControlHub(controlHub)
	appService.SetDeploymentService(deploymentService)
	appService.SetKnowledgeService(knowledgeService)
	appService.SetDelegationService(delegationExecution)
	h := handler.NewHandler(appService)

	restart := make(chan struct{}, 1)
	paths := cfg.Paths
	if paths.AppConfigFile == "" && resolvedConfigPath != "" {
		paths.AppConfigFile = resolvedConfigPath
	}
	pub := buildPublicHandlers(cfg, store, appService, experienceService, deploymentService, knowledgeService, orchestrationService, controlHub, paths, restart)
	var databaseProbe operationssvc.DatabaseProbe
	if store != nil {
		databaseProbe = func(ctx context.Context) error { return store.DB(ctx).Exec("SELECT 1").Error }
	}
	backupManager, err := operationssvc.NewBackupManager(cfg.Operations, cfg.DB)
	if err != nil {
		return nil, err
	}
	operationsService := operationssvc.New(cfg.Runtime.HTTPAddr, databaseProbe, controlHub).
		WithBackupManager(backupManager).
		WithGAConfig(operationssvc.GAConfig{
			DataStore: store != nil, Conversation: appService != nil, Memory: store != nil,
			Knowledge: knowledgeService != nil, Deployment: deploymentService != nil,
			Orchestration: orchestrationService != nil, GoalSupervisor: orchestrationService != nil && cfg.Orchestration.Enabled,
			PluginRegistry: store != nil,
		})
	if store != nil {
		operationsService = operationsService.WithGAEvidenceStore(operationsrepo.NewStore(store))
	}
	pub.Operations = operationshandler.NewHandler(operationsService)
	engine := httpapi.NewEngine(h, pub, cfg.Server.PublicPrefix, cfg.Server.Mode)
	controlhandler.NewHandler(controlHub, cfg.Control.DeviceToken).Register(engine, pub.Auth, cfg.Server.PublicPrefix)
	if delegationOrchestrator != nil {
		if err := delegationOrchestrator.Start(context.Background()); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	controlHub.Start(context.Background())
	if experienceService != nil {
		experienceService.Start(context.Background())
	}
	var supervisor *orchestrationsvc.Supervisor
	if orchestrationService != nil && cfg.Orchestration.Enabled {
		supervisor = orchestrationsvc.NewSupervisor(orchestrationService, appService, controlHub, orchestrationsvc.SupervisorConfig{ScanInterval: time.Duration(cfg.Orchestration.ScanIntervalSec) * time.Second, MaxConcurrentRuns: cfg.Orchestration.MaxConcurrentRuns})
		if err := supervisor.Start(context.Background()); err != nil {
			if delegationOrchestrator != nil {
				delegationOrchestrator.Stop()
			}
			controlHub.Stop()
			if experienceService != nil {
				experienceService.Stop()
			}
			_ = client.Close()
			return nil, err
		}
	}

	return &App{Cfg: cfg, Engine: engine, Restart: restart, client: client, data: store, Control: controlHub, Delegation: delegationOrchestrator, Experience: experienceService, Deployment: deploymentService, Knowledge: knowledgeService, Orchestration: orchestrationService, Supervisor: supervisor}, nil
}

// buildPublicHandlers wires the agent-frame-compatible resource handlers. The
// DB-backed handlers are only constructed when the shared database is enabled;
// the file-backed config handler and the channel skeletons are always available.
func buildPublicHandlers(cfg *config.Config, store *data.Data, runtimeService *appsvc.RuntimeService, experienceService *experiencesvc.Service, deploymentService *deploymentsvc.Service, knowledgeService *knowledgesvc.Service, orchestrationService *orchestrationsvc.Service, controlHub *controlsvc.Hub, paths config.PathsConfig, restart chan<- struct{}) *public.Handlers {
	pub := &public.Handlers{
		Config:    confighandler.NewHandler(paths, restart).WithRuntime(cfg.Runtime.HTTPAddr).WithService(cfg.Server.HTTPAddr),
		Callback:  callbackhandler.NewHandler(),
		Weixin:    weixinhandler.NewHandler(),
		Workspace: workspacehandler.NewHandler(),
	}

	if store != nil {
		modelService := modelsvc.NewSysModelService(store)
		if err := runtimeService.FailInterruptedMediaJobs(context.Background()); err != nil {
			log.Warnf("recover interrupted media jobs failed: %v", err)
		}
		pub.User = userhandler.NewHandler(usersvc.NewSysUserService(store)).WithAvatarStorage(paths.UploadsDir, cfg.Server.PublicPrefix)
		pub.VoiceAvatar = voiceavatarhandler.NewHandler(paths.UploadsDir, cfg.Server.PublicPrefix)
		pub.Auth = middleware.Auth(store)
		pub.Model = modelhandler.NewHandler(modelService).WithRuntime(cfg.Runtime.HTTPAddr).WithTraining(store, runtimeService, paths.UploadsDir)
		pub.Memory = memoryhandler.NewHandler(memorysvc.NewService(store))
		pub.Experience = experiencehandler.NewHandler(experienceService)
		pub.Learning = learninghandler.NewHandler(learningsvc.NewService(learningrepo.NewStore(store), experiencerepo.NewStore(store), experienceService))
		pub.Deployment = deploymenthandler.NewHandler(deploymentService)
		pub.Knowledge = knowledgehandler.NewHandler(knowledgeService)
		pub.Orchestration = orchestrationhandler.NewHandler(orchestrationService)
		pub.PluginRegistry = pluginregistryhandler.NewHandler(pluginregistrysvc.NewService(store, cfg.Plugins).WithRuntime(cfg.Runtime.HTTPAddr))
		pub.BrowserCredential = browsercredentialhandler.NewHandler(browsercredentialsvc.NewService(store))
		scheduledService := scheduledtasksvc.NewService(store, runtimeService, time.Duration(cfg.ScheduledTask.ScanIntervalSec)*time.Second).WithControlPlane(controlHub, orchestrationService)
		scheduledService.Start(context.Background())
		pub.ScheduledTask = scheduledtaskhandler.NewHandler(scheduledService)
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
	if a.Delegation != nil {
		a.Delegation.Stop()
	}
	if a.Supervisor != nil {
		a.Supervisor.Stop()
	}
	if a.Experience != nil {
		a.Experience.Stop()
	}
	if a.Control != nil {
		a.Control.Stop()
	}
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
