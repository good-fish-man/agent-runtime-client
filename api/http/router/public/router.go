// Package public registers the agent-frame-compatible resource routes onto a
// Gin router group. It mirrors agent-frame's api/http/router/public/sys_router.go
// endpoint layout so existing clients can target this service unchanged.
//
// Each resource group is registered only when its handler is non-nil, so the
// service degrades gracefully when the shared database is not configured (the
// DB-backed handlers stay nil while file-backed ones like config still mount).
package public

import (
	"github.com/gin-gonic/gin"

	agenth "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/agent"
	browsercredentialh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/browsercredential"
	callbackh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/callback"
	channelh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/channel"
	commandh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/command"
	confighh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/config"
	dashboardh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/dashboard"
	deploymenth "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/deployment"
	experienceh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/experience"
	jobh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/job"
	knowledgeh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/knowledge"
	kbh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/knowledge_base"
	learningh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/learning"
	memoryh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/memory"
	modelh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/model"
	operationsh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/operations"
	orchestrationh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/orchestration"
	pluginregistryh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/pluginregistry"
	scheduledtaskh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/scheduledtask"
	skillh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/skill"
	userh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/user"
	voiceavatarh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/voiceavatar"
	weixinh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/weixin"
	workspaceh "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/workspace"
)

// Handlers aggregates the public resource handlers wired by the composition
// root. DB-backed handlers may be nil when the database is not configured.
type Handlers struct {
	Auth              gin.HandlerFunc
	User              *userh.Handler
	Config            *confighh.Handler
	Model             *modelh.Handler
	Memory            *memoryh.Handler
	KB                *kbh.Handler
	Skill             *skillh.Handler
	Agent             *agenth.Handler
	BrowserCredential *browsercredentialh.Handler
	ScheduledTask     *scheduledtaskh.Handler
	Channel           *channelh.Handler
	Command           *commandh.Handler
	Callback          *callbackh.Handler
	Dashboard         *dashboardh.Handler
	Deployment        *deploymenth.Handler
	Knowledge         *knowledgeh.Handler
	Experience        *experienceh.Handler
	Learning          *learningh.Handler
	Orchestration     *orchestrationh.Handler
	Operations        *operationsh.Handler
	PluginRegistry    *pluginregistryh.Handler
	Weixin            *weixinh.Handler
	Job               *jobh.Handler
	Workspace         *workspaceh.Handler
	VoiceAvatar       *voiceavatarh.Handler
}

// Register mounts all available resource routes under the given group.
func Register(g *gin.RouterGroup, h *Handlers) {
	if h == nil {
		return
	}
	if h.User != nil {
		auth := g.Group("/auth")
		auth.POST("/register", h.User.Register)
		auth.POST("/login", h.User.Login)
		auth.GET("/avatar/:filename", h.User.Avatar)
	}
	if h.VoiceAvatar != nil {
		g.GET("/auth/voice-avatar/:filename", h.VoiceAvatar.Serve)
	}
	if h.ScheduledTask != nil {
		g.POST("/internal/scheduled-task", h.ScheduledTask.CreateInternal)
	}
	if h.Orchestration != nil {
		g.POST("/internal/goals", h.Orchestration.CreateInternal)
	}
	if h.Operations != nil {
		g.POST("/internal/operations/backups", h.Operations.CreateBackupInternal)
	}
	if h.Auth != nil {
		g.Use(h.Auth)
	}
	if h.Operations != nil {
		g.GET("/operations/health", h.Operations.Snapshot)
		g.GET("/operations/slo", h.Operations.Snapshot)
		g.GET("/operations/readiness", h.Operations.Readiness)
		g.GET("/operations/golden-journeys", h.Operations.GoldenJourneys)
		g.POST("/operations/golden-journeys/run", h.Operations.RunGoldenJourneys)
		g.GET("/operations/backups", h.Operations.ListBackups)
		g.POST("/operations/backups", h.Operations.CreateBackup)
		g.POST("/operations/backups/:backup_id/verify", h.Operations.VerifyBackup)
		g.POST("/operations/backups/:backup_id/restore", h.Operations.RestoreBackup)
	}
	if h.User != nil {
		g.GET("/auth/me", h.User.Me)
		g.POST("/auth/logout", h.User.Logout)
		g.PUT("/auth/me/avatar", h.User.UpdateAvatar)
	}
	if h.VoiceAvatar != nil {
		g.GET("/auth/me/voice-avatars", h.VoiceAvatar.List)
		g.POST("/auth/me/voice-avatars", h.VoiceAvatar.Upload)
		g.DELETE("/auth/me/voice-avatars/:id", h.VoiceAvatar.Delete)
	}

	if h.User != nil {
		r := g.Group("/user")
		r.POST("/user", h.User.CreateSysUser)
		r.DELETE("/user/:ulid", h.User.DeleteSysUser)
		r.PUT("/user/:ulid", h.User.UpdateSysUser)
		r.GET("/user/:ulid", h.User.FindSysUserById)
		r.POST("/user/byQuery", h.User.FindSysUserByQuery)
		r.POST("/user/byAll", h.User.FindSysUserAll)
		r.POST("/userPage", h.User.FindSysUserPage)
	}

	if h.Config != nil {
		r := g.Group("/config")
		r.GET("/app", h.Config.GetAppConfig)
		r.PUT("/app", h.Config.SaveAppConfig)
		r.GET("/skills", h.Config.GetSkillsConfig)
		r.PUT("/skills", h.Config.SaveSkillsConfig)
		r.GET("/status", h.Config.Status)
		r.POST("/restart", h.Config.Restart)
		r.GET("/restart/check", h.Config.RestartCheck)
		r.GET("/runtime/status", h.Config.RuntimeStatus)
		r.GET("/runtime", h.Config.RuntimeConfig)
		r.PUT("/runtime", h.Config.RuntimeConfig)
		r.GET("/runtime/skills", h.Config.RuntimeSkillsConfig)
		r.PUT("/runtime/skills", h.Config.RuntimeSkillsConfig)
		r.POST("/runtime/restart", h.Config.RestartRuntime)
	}

	if h.Model != nil {
		r := g.Group("/model")
		r.POST("", h.Model.CreateSysModel)
		r.DELETE("/:ulid", h.Model.DeleteSysModel)
		r.PUT("/:ulid", h.Model.UpdateSysModel)
		r.GET("/admin/all", h.Model.FindSysModelAdminAll)
		r.PUT("/:ulid/enabled", h.Model.UpdateSysModelEnabled)
		r.PUT("/:ulid/runtime-mode", h.Model.UpdateSysModelRuntimeMode)
		r.GET("/catalog", h.Model.FindModelCatalog)
		r.GET("/catalog/:ulid/environment", h.Model.LocalModelEnvironment)
		r.POST("/catalog/:ulid/install", h.Model.InstallLocalModel)
		r.GET("/install/:job_id", h.Model.LocalModelInstallJob)
		r.GET("/training/environment", h.Model.ModelTrainingEnvironment)
		r.POST("/training", h.Model.CreateModelTraining)
		r.GET("/training", h.Model.ListModelTrainings)
		r.GET("/training/:job_id", h.Model.GetModelTraining)
		r.POST("/training/:job_id/cancel", h.Model.CancelModelTraining)
		r.GET("/:ulid", h.Model.FindSysModelById)
		r.POST("/all", h.Model.FindSysModelAll)
		r.POST("/page", h.Model.FindSysModelPage)
	}

	if h.Model != nil {
		r := g.Group("/model-key")
		r.POST("", h.Model.CreateModelKey)
		r.GET("/all", h.Model.FindModelKeys)
		r.PUT("/:ulid", h.Model.UpdateModelKey)
		r.DELETE("/:ulid", h.Model.DeleteModelKey)
	}

	if h.Memory != nil {
		r := g.Group("/memory")
		r.POST("", h.Memory.Create)
		r.POST("/all", h.Memory.List)
		r.DELETE("/:ulid", h.Memory.Delete)
	}

	if h.Experience != nil {
		r := g.Group("/experience")
		r.GET("", h.Experience.List)
		r.GET("/preferences", h.Experience.GetPreference)
		r.PUT("/preferences", h.Experience.UpdatePreference)
		r.GET("/stats", h.Experience.Stats)
		r.POST("/search", h.Experience.Search)
		r.GET("/:id", h.Experience.Find)
		r.DELETE("/:id", h.Experience.Delete)
		r.POST("/:id/fixture", h.Experience.CreateFixture)

		evaluation := g.Group("/evaluation")
		evaluation.GET("/fixtures", h.Experience.ListFixtures)
		evaluation.POST("/suites", h.Experience.CreateSuite)
		evaluation.GET("/suites", h.Experience.ListSuites)
		evaluation.POST("/suites/:id/runs", h.Experience.RunSuite)
		evaluation.GET("/runs", h.Experience.ListRuns)
		evaluation.GET("/runs/:id/results", h.Experience.ListResults)
	}

	if h.Learning != nil {
		r := g.Group("/learning")
		r.POST("/candidates/generate", h.Learning.GenerateCandidate)
		r.GET("/candidates", h.Learning.ListCandidates)
		r.GET("/candidates/:id", h.Learning.FindCandidate)
		r.PUT("/candidates/:id", h.Learning.UpdateCandidate)
		r.POST("/candidates/:id/re-evaluate", h.Learning.ReevaluateCandidate)
		r.POST("/candidates/:id/review", h.Learning.ReviewCandidate)
		r.GET("/skills", h.Learning.ListSkills)
		r.GET("/strategies", h.Learning.ListStrategies)
		r.POST("/demonstrations", h.Learning.StartDemonstration)
		r.GET("/demonstrations", h.Learning.ListDemonstrations)
		r.POST("/demonstrations/:id/steps", h.Learning.RecordDemonstrationStep)
		r.POST("/demonstrations/:id/resume", h.Learning.ResumeDemonstration)
		r.POST("/demonstrations/:id/preview", h.Learning.PreviewDemonstration)
		r.PUT("/demonstrations/:id", h.Learning.EditDemonstration)
		r.POST("/demonstrations/:id/confirm", h.Learning.ConfirmDemonstration)
		r.POST("/demonstrations/:id/discard", h.Learning.DiscardDemonstration)
	}

	if h.Deployment != nil {
		r := g.Group("/deployment")
		r.POST("/builds", h.Deployment.CreateBuild)
		r.GET("/builds", h.Deployment.ListBuilds)
		r.GET("/builds/:id", h.Deployment.FindBuild)
		r.POST("/promotions", h.Deployment.ProposePromotion)
		r.GET("/promotions", h.Deployment.ListPromotions)
		r.POST("/promotions/:id/transition", h.Deployment.TransitionPromotion)
		r.POST("/promotions/:id/shadow", h.Deployment.RecordShadow)
		r.GET("/promotions/:id/shadow", h.Deployment.ListShadow)
		r.POST("/promotions/:id/metrics", h.Deployment.RecordMetric)
		r.GET("/promotions/:id/metrics", h.Deployment.ListMetrics)
		r.POST("/promotions/:id/rollback", h.Deployment.Rollback)
		r.GET("/experiment", h.Deployment.GetExperiment)
		r.PUT("/experiment", h.Deployment.SetOptOut)
		r.GET("/manifests", h.Deployment.ListManifests)
		r.GET("/rollbacks", h.Deployment.ListRollbacks)
	}

	if h.PluginRegistry != nil {
		r := g.Group("/plugins")
		r.GET("", h.PluginRegistry.List)
		r.GET("/audit", h.PluginRegistry.Audit)
		r.POST("/reload", h.PluginRegistry.Reload)
		r.POST("/install", h.PluginRegistry.Install)
		r.PUT("/:provider/:version/status", h.PluginRegistry.Transition)
		r.PUT("/:provider/:version/review", h.PluginRegistry.Review)
	}

	if h.Knowledge != nil {
		r := g.Group("/knowledge")
		r.POST("/claims", h.Knowledge.CreateClaim)
		r.GET("/claims", h.Knowledge.ListClaims)
		r.GET("/evidence", h.Knowledge.ListEvidence)
		r.POST("/retrieve", h.Knowledge.Retrieve)
		r.GET("/contradictions", h.Knowledge.ListContradictions)
		r.GET("/snapshots", h.Knowledge.ListSnapshots)
		r.POST("/ontology/packs", h.Knowledge.CreateOntologyPack)
		r.GET("/ontology/packs", h.Knowledge.ListOntologyPacks)
		r.POST("/ontology/candidates", h.Knowledge.CreateOntologyCandidate)
		r.POST("/ontology/candidates/:id/review", h.Knowledge.ReviewOntologyCandidate)
		r.POST("/ontology/migrations", h.Knowledge.CreateOntologyMigration)
	}
	if h.Orchestration != nil {
		r := g.Group("/goals")
		r.POST("", h.Orchestration.CreateGoal)
		r.GET("", h.Orchestration.ListGoals)
		r.GET("/:id", h.Orchestration.GetGoal)
		r.POST("/:id/plan", h.Orchestration.PlanGoal)
		r.POST("/:id/tasks/:taskID/start", h.Orchestration.StartTask)
		r.POST("/:id/tasks/:taskID/result", h.Orchestration.RecordResult)
		r.GET("/:id/tasks/:taskID/world-slice", h.Orchestration.WorldSlice)
		r.POST("/:id/checkpoints", h.Orchestration.SaveCheckpoint)
		r.GET("/:id/checkpoints", h.Orchestration.ListCheckpoints)
		r.POST("/:id/pause", h.Orchestration.Pause)
		r.POST("/:id/resume", h.Orchestration.Resume)
	}

	if h.BrowserCredential != nil {
		r := g.Group("/site-credential")
		r.POST("", h.BrowserCredential.Create)
		r.GET("/all", h.BrowserCredential.List)
		r.PUT("/:ulid", h.BrowserCredential.Update)
		r.DELETE("/:ulid", h.BrowserCredential.Delete)
		r.POST("/:ulid/login", h.BrowserCredential.Login)
	}

	if h.ScheduledTask != nil {
		r := g.Group("/scheduled-task")
		r.GET("/all", h.ScheduledTask.List)
		r.PUT("/:ulid", h.ScheduledTask.Update)
		r.DELETE("/:ulid", h.ScheduledTask.Delete)
		r.GET("/approvals", h.ScheduledTask.ListApprovals)
		r.POST("/approvals/:ulid", h.ScheduledTask.DecideApproval)
	}

	if h.KB != nil {
		r := g.Group("/knowledge_base")
		r.POST("", h.KB.CreateSysKnowledgeBase)
		r.DELETE("/:ulid", h.KB.DeleteSysKnowledgeBase)
		r.PUT("/:ulid", h.KB.UpdateSysKnowledgeBase)
		r.GET("/:ulid", h.KB.FindSysKnowledgeBaseById)
		r.POST("/all", h.KB.FindSysKnowledgeBaseAll)
		r.POST("/page", h.KB.FindSysKnowledgeBasePage)
		r.POST("/:ulid/recall", h.KB.RecallTest)
	}

	if h.Skill != nil {
		r := g.Group("/skill")
		r.POST("", h.Skill.CreateSysSkill)
		r.DELETE("/:ulid", h.Skill.DeleteSysSkill)
		r.PUT("/:ulid", h.Skill.UpdateSysSkill)
		r.GET("/:ulid", h.Skill.FindSysSkillById)
		r.POST("/all", h.Skill.FindSysSkillAll)
		r.POST("/page", h.Skill.FindSysSkillPage)
		r.POST("/upload", h.Skill.UploadSysSkill)
		r.POST("/check-name", h.Skill.CheckSkillName)
	}

	if h.Agent != nil {
		r := g.Group("/agent")
		r.POST("", h.Agent.CreateSysAgent)
		r.DELETE("/:ulid", h.Agent.DeleteSysAgent)
		r.PUT("/:ulid", h.Agent.UpdateSysAgent)
		r.GET("/:ulid", h.Agent.FindSysAgentById)
		r.POST("/all", h.Agent.FindSysAgentAll)
		r.POST("/page", h.Agent.FindSysAgentPage)
		r.POST("/upload", h.Agent.UploadSysAgent)
		r.PUT("/:ulid/enabled", h.Agent.UpdateSysAgentEnabled)
	}

	if h.Channel != nil {
		r := g.Group("/channel")
		r.POST("", h.Channel.CreateSysChannel)
		r.DELETE("/:ulid", h.Channel.DeleteSysChannel)
		r.PUT("/:ulid", h.Channel.UpdateSysChannel)
		r.GET("/:ulid", h.Channel.FindSysChannelById)
		r.POST("/all", h.Channel.FindSysChannelAll)
		r.POST("/page", h.Channel.FindSysChannelPage)
	}

	if h.Command != nil {
		r := g.Group("/command")
		r.POST("/execute", h.Command.Execute)
	}

	if h.Callback != nil {
		r := g.Group("/callback")
		r.POST("/:channel", h.Callback.HandleCallback)
	}

	if h.Weixin != nil {
		r := g.Group("/weixin")
		r.GET("/login", h.Weixin.Login)
		r.GET("/login/status", h.Weixin.Status)
	}

	if h.Job != nil {
		r := g.Group("/job")
		r.GET("/execution/:ulid", h.Job.FindJobExecutionById)
		r.GET("/execution/byAgentId", h.Job.FindJobExecutionByAgentId)
		r.POST("/execution/page", h.Job.FindJobExecutionPage)
	}

	if h.Dashboard != nil {
		r := g.Group("/dashboard")
		r.GET("/overview", h.Dashboard.Overview)
		r.GET("/token-ranking", h.Dashboard.TokenRanking)
		r.GET("/channel-activity", h.Dashboard.ChannelActivity)
		r.GET("/recent-sessions", h.Dashboard.RecentSessions)
	}

	if h.Workspace != nil {
		r := g.Group("/workspace")
		r.GET("/select-folder", h.Workspace.SelectFolder)
		r.POST("/import", h.Workspace.Import)
		r.GET("/:id/tree", h.Workspace.Tree)
		r.GET("/:id/file", h.Workspace.ReadFile)
		r.POST("/:id/search", h.Workspace.Search)
		r.POST("/:id/context", h.Workspace.Context)
		r.POST("/:id/build-patch", h.Workspace.BuildPatch)
		r.POST("/:id/apply-patch", h.Workspace.ApplyPatch)
	}
}
