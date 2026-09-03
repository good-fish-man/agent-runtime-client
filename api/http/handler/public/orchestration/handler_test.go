package orchestration

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	service "github.com/good-fish-man/agent-runtime-client/application/service/orchestration"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

func TestControlPlaneMutationHandlersRequireInternalToken(t *testing.T) {
	t.Setenv(consts.EnvInternalServiceToken, "machine-local-secret")
	gin.SetMode(gin.TestMode)
	handler := NewHandler(service.NewService(nil))

	tests := []struct {
		name string
		call func(*gin.Context)
	}{
		{name: "start task", call: handler.StartTask},
		{name: "record result", call: handler.RecordResult},
		{name: "save checkpoint", call: handler.SaveCheckpoint},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("POST", "/", nil)
			test.call(context)
			if len(context.Errors) != 1 {
				t.Fatalf("untrusted mutation reached the orchestration service: errors=%v", context.Errors)
			}
		})
	}
}

func TestControlPlaneMutationTokenIsExact(t *testing.T) {
	t.Setenv(consts.EnvInternalServiceToken, "machine-local-secret")
	gin.SetMode(gin.TestMode)
	handler := NewHandler(service.NewService(nil))
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/", nil)
	context.Request.Header.Set(consts.HeaderAthenaInternalToken, "machine-local-secret-extra")
	handler.StartTask(context)
	if len(context.Errors) != 1 {
		t.Fatal("a non-exact internal token was accepted")
	}
}
