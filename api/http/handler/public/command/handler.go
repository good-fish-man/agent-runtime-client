package command

import (
	"context"

	agentdto "github.com/good-fish-man/agent-runtime-client/application/dto/agent"
	runtimedto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

type runtimeRunner interface {
	Run(context.Context, *runtimedto.RunReq) (*runtimeentity.Completion, error)
}

type agentFinder interface {
	FindSysAgentById(context.Context, *agentdto.FindSysAgentByIdReq) (*agentdto.FindSysAgentRsp, error)
	FindSysAgentAll(context.Context, *agentdto.FindSysAgentAllReq) ([]*agentdto.FindSysAgentRsp, error)
}

// Handler executes natural-language commands or routes explicit UI commands.
type Handler struct {
	runtime runtimeRunner
	agents  agentFinder
}

func NewHandler(runtime runtimeRunner, agents agentFinder) *Handler {
	return &Handler{runtime: runtime, agents: agents}
}
