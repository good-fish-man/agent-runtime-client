// Package runtime (domain/irepository) declares the outbound port the domain
// depends on. The gRPC implementation lives in infra and is injected at boot, so
// the domain never imports infra.
package runtime

import (
	"context"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

// StreamFunc receives streaming events. Returning a non-nil error stops the stream.
// Declared as an alias so callers across layers can pass compatible callbacks
// without explicit conversion.
type StreamFunc = func(*entity.StreamEvent) error

// RuntimeGateway is the port to the agent-runtime service.
type RuntimeGateway interface {
	Run(ctx context.Context, in *entity.RunInput) (*entity.Completion, error)
	RunStream(ctx context.Context, in *entity.RunInput, emit StreamFunc) error
	RunAgent(ctx context.Context, in *entity.AgentInput) (*entity.AgentResult, error)
	RunAgentStream(ctx context.Context, in *entity.AgentInput, emit StreamFunc) error
	GenerateMedia(ctx context.Context, in *entity.MediaGenerationInput) (*entity.MediaGenerationResult, error)
	Resume(ctx context.Context, in *entity.ResumeInput) (*entity.ResumeResult, error)
	Stop(ctx context.Context, in *entity.StopInput) (*entity.StopResult, error)
	Health(ctx context.Context, in *entity.HealthInput) (*entity.HealthStatus, error)
}
