// Package runtime (domain/srv) is the domain service for runtime invocation. It
// applies validation and default-model injection, then delegates to the
// RuntimeGateway port. It depends only on the port (never on infra).
package runtime

import (
	"context"
	"github.com/good-fish-man/agent-runtime/log"
	"strings"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/runtime"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

// RuntimeSvc orchestrates validation/defaults over the gateway port.
type RuntimeSvc struct {
	gateway      irepo.RuntimeGateway
	defaultModel *entity.ModelConfig
}

// NewRuntimeSvc builds the domain service. defaultModel may be nil, in which case
// callers must supply models["default"] themselves.
func NewRuntimeSvc(gateway irepo.RuntimeGateway, defaultModel *entity.ModelConfig) *RuntimeSvc {
	return &RuntimeSvc{gateway: gateway, defaultModel: defaultModel}
}

// Run validates and executes a non-streaming run.
func (s *RuntimeSvc) Run(ctx context.Context, in *entity.RunInput) (*entity.Completion, error) {
	if err := s.prepareRun(in); err != nil {
		return nil, err
	}
	return s.gateway.Run(ctx, in)
}

// RunStream validates and executes a streaming run.
func (s *RuntimeSvc) RunStream(ctx context.Context, in *entity.RunInput, emit irepo.StreamFunc) error {
	if err := s.prepareRun(in); err != nil {
		return err
	}
	return s.gateway.RunStream(ctx, in, emit)
}

// RunAgent validates and executes a non-streaming agent run.
func (s *RuntimeSvc) RunAgent(ctx context.Context, in *entity.AgentInput) (*entity.AgentResult, error) {
	if err := s.prepareAgent(in); err != nil {
		return nil, err
	}
	return s.gateway.RunAgent(ctx, in)
}

// RunAgentStream validates and executes a streaming agent run.
func (s *RuntimeSvc) RunAgentStream(ctx context.Context, in *entity.AgentInput, emit irepo.StreamFunc) error {
	if err := s.prepareAgent(in); err != nil {
		return err
	}
	return s.gateway.RunAgentStream(ctx, in, emit)
}

// Resume validates and resumes a checkpointed run.
func (s *RuntimeSvc) Resume(ctx context.Context, in *entity.ResumeInput) (*entity.ResumeResult, error) {
	if strings.TrimSpace(in.CheckpointID) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("checkpoint_id is required")
	}
	return s.gateway.Resume(ctx, in)
}

// Stop validates and stops a run.
func (s *RuntimeSvc) Stop(ctx context.Context, in *entity.StopInput) (*entity.StopResult, error) {
	if strings.TrimSpace(in.CheckpointID) == "" && strings.TrimSpace(in.SessionID) == "" {
		return nil, apierror.ErrBadRequest.WithMessage("checkpoint_id or session_id is required")
	}
	return s.gateway.Stop(ctx, in)
}

// Health probes runtime health.
func (s *RuntimeSvc) Health(ctx context.Context, in *entity.HealthInput) (*entity.HealthStatus, error) {
	return s.gateway.Health(ctx, in)
}

func (s *RuntimeSvc) prepareRun(in *entity.RunInput) error {
	if in == nil {
		log.Error("empty run input")
		return apierror.ErrBadRequest.WithMessage("empty request")
	}
	if strings.TrimSpace(in.Prompt) == "" && len(in.Messages) == 0 {
		return apierror.ErrBadRequest.WithMessage("prompt or messages is required")
	}
	models, err := s.applyDefaultModel(in.Models)
	if err != nil {
		return err
	}
	in.Models = models
	return nil
}

func (s *RuntimeSvc) prepareAgent(in *entity.AgentInput) error {
	if in == nil {
		log.Error("empty agent input")
		return apierror.ErrBadRequest.WithMessage("empty request")
	}
	if strings.TrimSpace(in.Task) == "" {
		log.Error("task is required")
		return apierror.ErrBadRequest.WithMessage("task is required")
	}
	models, err := s.applyDefaultModel(in.Models)
	if err != nil {
		return err
	}
	in.Models = models
	return nil
}

// applyDefaultModel injects the configured default model when the request omits
// models["default"], erroring if neither is present.
func (s *RuntimeSvc) applyDefaultModel(models map[string]entity.ModelConfig) (map[string]entity.ModelConfig, error) {
	if models == nil {
		models = map[string]entity.ModelConfig{}
	}
	if m, ok := models[consts.ModelRoleDefault]; !ok || strings.TrimSpace(m.Name) == "" {
		if s.defaultModel == nil || strings.TrimSpace(s.defaultModel.Name) == "" {
			return nil, apierror.ErrBadRequest.WithMessage(
				"model not configured: provide models.default or set a default model in config")
		}
		models[consts.ModelRoleDefault] = *s.defaultModel
	}
	return models, nil
}
