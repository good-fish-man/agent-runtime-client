package public

import (
	"testing"

	"github.com/gin-gonic/gin"

	agenthandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/agent"
	callbackhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/callback"
	channelhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/channel"
	confighandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/config"
	jobhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/job"
	kbhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/knowledge_base"
	modelhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/model"
	skillhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/skill"
	userhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/user"
	weixinhandler "github.com/good-fish-man/agent-runtime-client/api/http/handler/public/weixin"

	agentsvc "github.com/good-fish-man/agent-runtime-client/application/service/agent"
	channelsvc "github.com/good-fish-man/agent-runtime-client/application/service/channel"
	jobsvc "github.com/good-fish-man/agent-runtime-client/application/service/job"
	kbsvc "github.com/good-fish-man/agent-runtime-client/application/service/knowledge_base"
	modelsvc "github.com/good-fish-man/agent-runtime-client/application/service/model"
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
		User:     userhandler.NewHandler(usersvc.NewSysUserService(nil)),
		Config:   confighandler.NewHandler(appconfig.PathsConfig{}),
		Model:    modelhandler.NewHandler(modelsvc.NewSysModelService(nil)),
		KB:       kbhandler.NewHandler(kbsvc.NewSysKnowledgeBaseService(nil)),
		Skill:    skillhandler.NewHandler(skillsvc.NewSysSkillService(nil)),
		Agent:    agenthandler.NewHandler(agentsvc.NewSysAgentService(nil)),
		Channel:  channelhandler.NewHandler(channelsvc.NewSysChannelService(nil)),
		Callback: callbackhandler.NewHandler(),
		Weixin:   weixinhandler.NewHandler(),
		Job:      jobhandler.NewHandler(jobsvc.NewJobExecutionService(nil)),
	}

	engine := gin.New()
	Register(engine.Group("/api/xiaoqinglong/agent-frame/v1"), h)

	if got := len(engine.Routes()); got == 0 {
		t.Fatalf("expected routes to be registered, got %d", got)
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
