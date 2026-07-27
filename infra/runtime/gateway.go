package runtime

import (
	"context"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	irepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/runtime"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

// Gateway implements domain irepository.RuntimeGateway over gRPC.
type Gateway struct {
	client *Client
}

// NewGateway builds the gRPC-backed gateway.
func NewGateway(client *Client) *Gateway {
	return &Gateway{client: client}
}

// Compile-time assertion that Gateway satisfies the port.
var _ irepo.RuntimeGateway = (*Gateway)(nil)

// Run executes a non-streaming run.
func (g *Gateway) Run(ctx context.Context, in *entity.RunInput) (*entity.Completion, error) {
	ctx, cancel := g.unaryCtx(ctx, in.TraceID)
	defer cancel()
	resp, err := g.client.rpc.Run(ctx, toRunRequest(in))
	if err != nil {
		return nil, err
	}
	return fromRunResponse(resp), nil
}

// RunStream executes a streaming run, invoking emit per event.
func (g *Gateway) RunStream(ctx context.Context, in *entity.RunInput, emit irepo.StreamFunc) error {
	stream, err := g.client.rpc.RunStream(g.streamCtx(ctx, in.TraceID), toRunRequest(in))
	if err != nil {
		return err
	}
	return consumeStream(stream, emit)
}

// RunAgent executes a non-streaming agent run.
func (g *Gateway) RunAgent(ctx context.Context, in *entity.AgentInput) (*entity.AgentResult, error) {
	ctx, cancel := g.unaryCtx(ctx, in.TraceID)
	defer cancel()
	resp, err := g.client.rpc.RunAgent(ctx, toAgentRequest(in))
	if err != nil {
		return nil, err
	}
	return fromAgentResponse(resp), nil
}

// RunAgentStream executes a streaming agent run, invoking emit per event.
func (g *Gateway) RunAgentStream(ctx context.Context, in *entity.AgentInput, emit irepo.StreamFunc) error {
	stream, err := g.client.rpc.RunAgentStream(g.streamCtx(ctx, in.TraceID), toAgentRequest(in))
	if err != nil {
		return err
	}
	return consumeStream(stream, emit)
}

// Resume resumes a checkpointed run.
func (g *Gateway) Resume(ctx context.Context, in *entity.ResumeInput) (*entity.ResumeResult, error) {
	ctx, cancel := g.unaryCtx(ctx, in.TraceID)
	defer cancel()
	resp, err := g.client.rpc.Resume(ctx, toResumeRequest(in))
	if err != nil {
		return nil, err
	}
	return fromResumeResponse(resp), nil
}

// Stop stops a run.
func (g *Gateway) Stop(ctx context.Context, in *entity.StopInput) (*entity.StopResult, error) {
	ctx, cancel := g.unaryCtx(ctx, in.TraceID)
	defer cancel()
	resp, err := g.client.rpc.Stop(ctx, toStopRequest(in))
	if err != nil {
		return nil, err
	}
	return fromStopResponse(resp), nil
}

// Health probes runtime health.
func (g *Gateway) Health(ctx context.Context, in *entity.HealthInput) (*entity.HealthStatus, error) {
	ctx, cancel := g.unaryCtx(ctx, in.TraceID)
	defer cancel()
	resp, err := g.client.rpc.HealthCheck(ctx, toHealthRequest(in))
	if err != nil {
		return nil, err
	}
	return fromHealthResponse(resp), nil
}

// unaryCtx attaches trace metadata and a per-call timeout for unary RPCs.
func (g *Gateway) unaryCtx(ctx context.Context, traceID string) (context.Context, context.CancelFunc) {
	ctx = withTrace(ctx, traceID)
	if g.client.reqTimeout > 0 {
		return context.WithTimeout(ctx, g.client.reqTimeout)
	}
	return ctx, func() {}
}

// streamCtx attaches trace metadata but no timeout (streams may be long-lived;
// the caller's context controls the lifetime).
func (g *Gateway) streamCtx(ctx context.Context, traceID string) context.Context {
	return withTrace(ctx, traceID)
}

// withTrace forwards the trace id to agent-runtime via gRPC metadata.
func withTrace(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, consts.MetaKeyTraceID, traceID)
}

// consumeStream drains a server stream, mapping each event and forwarding it to emit.
func consumeStream(stream grpc.ServerStreamingClient[runtimev1.StreamEvent], emit irepo.StreamFunc) error {
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := emit(fromStreamEvent(ev)); err != nil {
			return err
		}
	}
}
