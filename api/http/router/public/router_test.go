package public

import (
	"testing"

	"github.com/gin-gonic/gin"

	agenthandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/agent"
	callbackhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/callback"
	channelhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/channel"
	confighandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/config"
	deploymenthandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/deployment"
	experiencehandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/experience"
	jobhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/job"
	knowledgehandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/knowledge"
	kbhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/knowledge_base"
	learninghandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/learning"
	modelhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/model"
	orchestrationhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/orchestration"
	skillhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/skill"
	userhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/user"
	weixinhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/weixin"

	agentsvc "github.com/good-fish-man/agent-runtime-client/application/service/agent"
	channelsvc "github.com/good-fish-man/agent-runtime-client/application/service/channel"
	deploymentsvc "github.com/good-fish-man/agent-runtime-client/application/service/deployment"
	experiencesvc "github.com/good-fish-man/agent-runtime-client/application/service/experience"
	jobsvc "github.com/good-fish-man/agent-runtime-client/application/service/job"
	knowledgesvc "github.com/good-fish-man/agent-runtime-client/application/service/knowledge"
	kbsvc "github.com/good-fish-man/agent-runtime-client/application/service/knowledge_base"
	learningsvc "github.com/good-fish-man/agent-runtime-client/application/service/learning"
	modelsvc "github.com/good-fish-man/agent-runtime-client/application/service/model"
	orchestrationsvc "github.com/good-fish-man/agent-runtime-client/application/service/orchestration"
	skillsvc "github.com/good-fish-man/agent-runtime-client/application/service/skill"
	usersvc "github.com/good-fish-man/agent-runtime-client/application/service/user"
	appconfig "github.com/good-fish-man/agent-runtime-client/config"
)

// TestRegisterNoConflicts ensures every resource route mounts without gin
// panicking on static-vs-wildcard conflicts (e.g. /execution/:ulid vs
// /execution/byAgentId). Services are built with a nil store because route
// registration never invokes the handlers.
func TestRegisterNoConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handlers{
		User:          userhandler.NewHandler(usersvc.NewSysUserService(nil)),
		Config:        confighandler.NewHandler(appconfig.PathsConfig{}),
		Model:         modelhandler.NewHandler(modelsvc.NewSysModelService(nil)),
		KB:            kbhandler.NewHandler(kbsvc.NewSysKnowledgeBaseService(nil)),
		Skill:         skillhandler.NewHandler(skillsvc.NewSysSkillService(nil)),
		Agent:         agenthandler.NewHandler(agentsvc.NewSysAgentService(nil)),
		Channel:       channelhandler.NewHandler(channelsvc.NewSysChannelService(nil)),
		Experience:    experiencehandler.NewHandler(experiencesvc.NewService(nil, nil)),
		Deployment:    deploymenthandler.NewHandler(deploymentsvc.NewService(nil)),
		Knowledge:     knowledgehandler.NewHandler(knowledgesvc.NewService(nil)),
		Learning:      learninghandler.NewHandler(learningsvc.NewServiceWithDependencies(nil, nil, nil)),
		Orchestration: orchestrationhandler.NewHandler(orchestrationsvc.NewService(nil)),
		Callback:      callbackhandler.NewHandler(),
		Weixin:        weixinhandler.NewHandler(),
		Job:           jobhandler.NewHandler(jobsvc.NewJobExecutionService(nil)),
	}

	engine := gin.New()
	Register(engine.Group("/api/xiaoqinglong/agent-frame/v1"), h)

	if got := len(engine.Routes()); got == 0 {
		t.Fatalf("expected routes to be registered, got %d", got)
	}

	registered := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	prefix := "/api/xiaoqinglong/agent-frame/v1"
	expected := []string{
		"GET " + prefix + "/experience",
		"GET " + prefix + "/experience/preferences",
		"PUT " + prefix + "/experience/preferences",
		"GET " + prefix + "/experience/stats",
		"POST " + prefix + "/experience/search",
		"GET " + prefix + "/experience/:id",
		"DELETE " + prefix + "/experience/:id",
		"POST " + prefix + "/experience/:id/fixture",
		"GET " + prefix + "/evaluation/fixtures",
		"POST " + prefix + "/evaluation/suites",
		"GET " + prefix + "/evaluation/suites",
		"POST " + prefix + "/evaluation/suites/:id/runs",
		"GET " + prefix + "/evaluation/runs",
		"GET " + prefix + "/evaluation/runs/:id/results",
		"POST " + prefix + "/learning/candidates/generate",
		"GET " + prefix + "/learning/candidates",
		"GET " + prefix + "/learning/candidates/:id",
		"PUT " + prefix + "/learning/candidates/:id",
		"POST " + prefix + "/learning/candidates/:id/re-evaluate",
		"POST " + prefix + "/learning/candidates/:id/review",
		"GET " + prefix + "/learning/skills",
		"GET " + prefix + "/learning/strategies",
		"POST " + prefix + "/learning/demonstrations",
		"GET " + prefix + "/learning/demonstrations",
		"POST " + prefix + "/learning/demonstrations/:id/steps",
		"POST " + prefix + "/learning/demonstrations/:id/resume",
		"POST " + prefix + "/learning/demonstrations/:id/preview",
		"PUT " + prefix + "/learning/demonstrations/:id",
		"POST " + prefix + "/learning/demonstrations/:id/confirm",
		"POST " + prefix + "/learning/demonstrations/:id/discard",
		"POST " + prefix + "/deployment/builds",
		"GET " + prefix + "/deployment/builds",
		"GET " + prefix + "/deployment/builds/:id",
		"POST " + prefix + "/deployment/promotions",
		"GET " + prefix + "/deployment/promotions",
		"POST " + prefix + "/deployment/promotions/:id/transition",
		"POST " + prefix + "/deployment/promotions/:id/shadow/evaluate",
		"GET " + prefix + "/deployment/promotions/:id/shadow",
		"GET " + prefix + "/deployment/promotions/:id/canary-samples",
		"GET " + prefix + "/deployment/promotions/:id/metrics",
		"POST " + prefix + "/deployment/promotions/:id/rollback",
		"GET " + prefix + "/deployment/experiment",
		"PUT " + prefix + "/deployment/experiment",
		"GET " + prefix + "/deployment/manifests",
		"GET " + prefix + "/deployment/rollbacks",
		"POST " + prefix + "/knowledge/claims",
		"GET " + prefix + "/knowledge/claims",
		"POST " + prefix + "/knowledge/evidence/confirmations",
		"GET " + prefix + "/knowledge/evidence",
		"POST " + prefix + "/knowledge/retrieve",
		"GET " + prefix + "/knowledge/contradictions",
		"POST " + prefix + "/knowledge/contradictions/:id/resolve",
		"GET " + prefix + "/knowledge/snapshots",
		"POST " + prefix + "/knowledge/ontology/packs",
		"GET " + prefix + "/knowledge/ontology/packs",
		"POST " + prefix + "/knowledge/ontology/candidates",
		"GET " + prefix + "/knowledge/ontology/candidates",
		"POST " + prefix + "/knowledge/ontology/candidates/:id/review",
		"POST " + prefix + "/knowledge/ontology/migrations",
		"POST " + prefix + "/knowledge/ontology/migrations/:id/review",
		"POST " + prefix + "/knowledge/ontology/migrations/:id/execute",
		"POST " + prefix + "/goals",
		"POST " + prefix + "/internal/goals",
		"GET " + prefix + "/goals",
		"GET " + prefix + "/goals/schedule-triggers",
		"GET " + prefix + "/goals/:id",
		"POST " + prefix + "/goals/:id/plan",
		"POST " + prefix + "/goals/:id/tasks/:taskID/start",
		"POST " + prefix + "/goals/:id/tasks/:taskID/result",
		"GET " + prefix + "/goals/:id/tasks/:taskID/world-slice",
		"POST " + prefix + "/goals/:id/checkpoints",
		"GET " + prefix + "/goals/:id/checkpoints",
		"POST " + prefix + "/goals/:id/pause",
		"POST " + prefix + "/goals/:id/resume",
	}
	for _, route := range expected {
		if _, ok := registered[route]; !ok {
			t.Errorf("required route was not registered: %s", route)
		}
	}
}

// TestRegisterNilGraceful ensures a nil-handler set (DB disabled) registers
// only the always-on file-backed and skeleton routes without panicking.
func TestRegisterNilGraceful(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	Register(engine.Group("/p"), &Handlers{
		Config:   confighandler.NewHandler(appconfig.PathsConfig{}),
		Callback: callbackhandler.NewHandler(),
		Weixin:   weixinhandler.NewHandler(),
	})

	if got := len(engine.Routes()); got == 0 {
		t.Fatalf("expected config/callback/weixin routes, got %d", got)
	}
}
