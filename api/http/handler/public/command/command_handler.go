package command

import (
	"strings"

	"github.com/gin-gonic/gin"

	agentdto "github.com/good-fish-man/agent-runtime-client/application/dto/agent"
	runtimedto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type executeReq struct {
	Command   string `json:"command"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
}

type executeRsp struct {
	Success      bool   `json:"success"`
	Action       string `json:"action"`
	NavigateTo   string `json:"navigate_to,omitempty"`
	Message      string `json:"message"`
	ShowGuidance bool   `json:"show_guidance,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	Result       any    `json:"result,omitempty"`
}

type navigationCommand struct {
	action  string
	target  string
	message string
}

func (h *Handler) Execute(c *gin.Context) {
	var req executeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("command request is invalid: " + err.Error()))
		return
	}
	req.Command = strings.TrimSpace(req.Command)
	if req.Command == "" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("command is required"))
		return
	}

	if navigation := matchNavigation(req.Command); navigation != nil {
		response.Ok(c, &executeRsp{Success: true, Action: navigation.action, NavigateTo: navigation.target, Message: navigation.message})
		return
	}
	if h.runtime == nil || h.agents == nil {
		_ = c.Error(apierror.ErrRuntimeUnavailable.WithMessage("command execution service is unavailable"))
		return
	}

	userID := c.GetString(consts.CtxKeyUserID)
	agent, err := h.resolveAgent(c, userID, strings.TrimSpace(req.AgentID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if agent == nil {
		response.Ok(c, &executeRsp{Success: true, Action: "create_agent", NavigateTo: "orchestrator", Message: "没有可用的 Agent，请先创建或启用一个 Agent。", ShowGuidance: true})
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "command-" + ulid.New()
	}
	result, err := h.runtime.Run(c.Request.Context(), &runtimedto.RunReq{
		Prompt: req.Command,
		Context: map[string]any{
			consts.ContextKeyUserID:    userID,
			consts.ContextKeyAgentID:   agent.Ulid,
			consts.ContextKeySessionID: sessionID,
		},
		RequestID: ulid.New(),
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, &executeRsp{
		Success: true,
		Action:  "agent_result",
		Message: "已由 " + agent.Name + " 执行",
		AgentID: agent.Ulid,
		Result:  result,
	})
}

func (h *Handler) resolveAgent(c *gin.Context, userID, requestedID string) (*agentdto.FindSysAgentRsp, error) {
	if requestedID != "" {
		return h.agents.FindSysAgentById(c.Request.Context(), &agentdto.FindSysAgentByIdReq{Ulid: requestedID, UserID: userID})
	}
	agents, err := h.agents.FindSysAgentAll(c.Request.Context(), &agentdto.FindSysAgentAllReq{UserID: userID})
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if agent != nil && agent.Enabled && !agent.IsSystem {
			return agent, nil
		}
	}
	for _, agent := range agents {
		if agent != nil && agent.Enabled {
			return agent, nil
		}
	}
	return nil, nil
}

func matchNavigation(raw string) *navigationCommand {
	command := strings.ToLower(strings.TrimSpace(raw))
	has := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(command, word) {
				return true
			}
		}
		return false
	}
	create := has("创建", "新建", "添加", "create", "new", "add")
	openOrManage := has("打开", "进入", "查看", "管理", "配置", "open", "show", "manage", "configure", "go to")
	agentResource := has("agent", "智能体")
	modelResource := has("model", "模型")
	skillResource := has("skill", "技能")

	switch {
	case create && agentResource:
		return &navigationCommand{action: "create_agent", target: "orchestrator", message: "正在打开 Agent 创建页面"}
	case create && modelResource:
		return &navigationCommand{action: "add_model", target: "models", message: "正在打开模型管理页面"}
	case (has("安装", "install") || create) && skillResource:
		return &navigationCommand{action: "install_skill", target: "skills", message: "正在打开技能管理页面"}
	case has("收件箱", "inbox"):
		return &navigationCommand{action: "show_inbox", target: "inbox", message: "正在打开收件箱"}
	case openOrManage && has("知识库", "knowledge base"):
		return &navigationCommand{action: "config_kb", target: "knowledge", message: "正在打开知识库"}
	case openOrManage && has("工作区", "workspace"):
		return &navigationCommand{action: "open_workspace", target: "workspace", message: "正在打开工作区"}
	case has("设置", "settings", "service configuration"):
		return &navigationCommand{action: "open_settings", target: "settings", message: "正在打开设置"}
	case has("模型列表", "模型管理", "models"):
		return &navigationCommand{action: "open_models", target: "models", message: "正在打开模型管理页面"}
	case has("agent列表", "agent管理", "智能体列表", "智能体管理", "agents"):
		return &navigationCommand{action: "open_agents", target: "agents", message: "正在打开 Agent 管理页面"}
	case has("技能列表", "技能管理", "skills"):
		return &navigationCommand{action: "open_skills", target: "skills", message: "正在打开技能管理页面"}
	case has("打开聊天", "开始聊天", "open chat"):
		return &navigationCommand{action: "open_chat", target: "chat", message: "正在打开聊天"}
	default:
		return nil
	}
}
