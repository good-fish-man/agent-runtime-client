// Package runtime (infra) is the gRPC implementation of the domain RuntimeGateway
// port. client.go owns the connection lifecycle.
package runtime

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	runtimev1 "github.com/good-fish-man/agent-runtime/gen/agent/runtime/v1"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/pkg/errtrace"
	"github.com/good-fish-man/agent-runtime-client/pkg/log"
)

// Client wraps a gRPC connection to agent-runtime.
type Client struct {
	conn       *grpc.ClientConn
	rpc        runtimev1.AgentRuntimeClient
	reqTimeout time.Duration
}

// NewClient creates a lazily-connecting gRPC client. grpc.NewClient does not dial
// eagerly; the connection is established on first RPC and bounded by each call's
// context timeout.
func NewClient(grpcAddr string, reqTimeout time.Duration) (*Client, error) {
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, errtrace.Wrap(err, "RuntimeClient.New")
	}
	log.Infof("runtime gRPC client configured for %s (request timeout %s)", grpcAddr, reqTimeout)
	return &Client{
		conn:       conn,
		rpc:        runtimev1.NewAgentRuntimeClient(conn),
		reqTimeout: reqTimeout,
	}, nil
}

// Ping performs a bounded health check, useful as a startup probe. It never
// mutates client state and tolerates the runtime being unavailable.
func (c *Client) Ping(ctx context.Context, dialTimeout time.Duration) (*entity.HealthStatus, error) {
	if dialTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dialTimeout)
		defer cancel()
	}
	resp, err := c.rpc.HealthCheck(ctx, &runtimev1.HealthCheckRequest{Service: "agent-runtime"})
	if err != nil {
		return nil, errtrace.Wrap(err, "RuntimeClient.Ping")
	}
	return fromHealthResponse(resp), nil
}

// Close releases the underlying connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
