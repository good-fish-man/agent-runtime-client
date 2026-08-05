package runtime

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

func TestStreamDoesNotDuplicateAnUpstreamErrorEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/agent/stream", nil)

	handler := &Handler{}
	handler.stream(ctx, func(_ context.Context, emit func(*entity.StreamEvent) error) error {
		if err := emit(&entity.StreamEvent{Type: entity.StreamTypeError, Error: &entity.ErrorEvent{Message: "upstream failed"}}); err != nil {
			return err
		}
		return errors.New("wrapped upstream failed")
	})

	if count := strings.Count(recorder.Body.String(), "event: error"); count != 1 {
		t.Fatalf("error event count = %d, body = %q", count, recorder.Body.String())
	}
}

func TestStreamRequestDisconnectDoesNotCancelBackgroundRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	requestContext, cancelRequest := context.WithCancel(context.Background())
	ginContext.Request = httptest.NewRequest("POST", "/v1/agent/stream", nil).WithContext(requestContext)

	started := make(chan struct{})
	completed := make(chan struct{})
	returned := make(chan struct{})
	handler := &Handler{}
	go func() {
		handler.stream(ginContext, func(taskContext context.Context, _ func(*entity.StreamEvent) error) error {
			close(started)
			time.Sleep(40 * time.Millisecond)
			if taskContext.Err() != nil {
				return taskContext.Err()
			}
			close(completed)
			return nil
		})
		close(returned)
	}()

	<-started
	cancelRequest()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("HTTP stream did not detach after request cancellation")
	}
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("background run was cancelled with the frontend request")
	}
}
