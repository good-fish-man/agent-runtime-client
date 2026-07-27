package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	agentdto "github.com/good-fish-man/agent-runtime-client/application/dto/agent"
	runtimedto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

type fakeRuntimeRunner struct {
	request *runtimedto.RunReq
	result  *runtimeentity.Completion
}

func (f *fakeRuntimeRunner) Run(_ context.Context, req *runtimedto.RunReq) (*runtimeentity.Completion, error) {
	f.request = req
	return f.result, nil
}

type fakeAgentFinder struct {
	agents []*agentdto.FindSysAgentRsp
}

func (f *fakeAgentFinder) FindSysAgentById(_ context.Context, req *agentdto.FindSysAgentByIdReq) (*agentdto.FindSysAgentRsp, error) {
	for _, agent := range f.agents {
		if agent.Ulid == req.Ulid {
			return agent, nil
		}
	}
	return nil, nil
}

func (f *fakeAgentFinder) FindSysAgentAll(context.Context, *agentdto.FindSysAgentAllReq) ([]*agentdto.FindSysAgentRsp, error) {
	return f.agents, nil
}

func executeCommand(t *testing.T, handler *Handler, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/command/execute", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set(consts.CtxKeyUserID, "user-1")
	handler.Execute(ctx)
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return recorder, envelope.Data
}

func TestExecuteNavigationDoesNotInvokeRuntime(t *testing.T) {
	runner := &fakeRuntimeRunner{}
	recorder, data := executeCommand(t, NewHandler(runner, &fakeAgentFinder{}), `{"command":"创建一个翻译智能体"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if data["action"] != "create_agent" || data["navigate_to"] != "orchestrator" {
		t.Fatalf("unexpected navigation response: %#v", data)
	}
	if runner.request != nil {
		t.Fatal("navigation command must not invoke runtime")
	}
}

func TestExecuteRunsUserAgent(t *testing.T) {
	runner := &fakeRuntimeRunner{result: &runtimeentity.Completion{Content: "优化建议", TraceID: "trace-1"}}
	agents := &fakeAgentFinder{agents: []*agentdto.FindSysAgentRsp{
		{Ulid: "system-agent", Name: "公共助手", Enabled: true, IsSystem: true},
		{Ulid: "user-agent", Name: "我的助手", Enabled: true},
	}}
	recorder, data := executeCommand(t, NewHandler(runner, agents), `{"command":"分析当前项目的性能瓶颈"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if data["action"] != "agent_result" || data["agent_id"] != "user-agent" {
		t.Fatalf("unexpected execution response: %#v", data)
	}
	if runner.request == nil || runner.request.Prompt != "分析当前项目的性能瓶颈" {
		t.Fatalf("runtime request was not forwarded: %#v", runner.request)
	}
	if runner.request.Context[consts.ContextKeyUserID] != "user-1" || runner.request.Context[consts.ContextKeyAgentID] != "user-agent" {
		t.Fatalf("runtime context = %#v", runner.request.Context)
	}
}

func TestKnowledgeTaskRunsAgentInsteadOfOnlyNavigating(t *testing.T) {
	runner := &fakeRuntimeRunner{result: &runtimeentity.Completion{Content: "知识库分析完成"}}
	agents := &fakeAgentFinder{agents: []*agentdto.FindSysAgentRsp{
		{Ulid: "user-agent", Name: "我的助手", Enabled: true},
	}}
	_, data := executeCommand(t, NewHandler(runner, agents), `{"command":"分析知识库里的重复内容"}`)
	if data["action"] != "agent_result" || runner.request == nil {
		t.Fatalf("knowledge task should invoke the Agent: response=%#v request=%#v", data, runner.request)
	}
}
