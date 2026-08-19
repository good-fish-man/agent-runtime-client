package delegationops

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
)

func TestReplayRejectsMissingAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/delegation/replays", strings.NewReader(`{}`))
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request

	NewHandler(nil, nil).Replay(ctx)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDeleteRequiresExactConfirmationBeforeCallingRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/delegation/data", strings.NewReader(`{"cutoff":"2026-08-20T00:00:00Z","confirmation":"delete"}`))
	request = request.WithContext(authctx.WithUserID(request.Context(), "owner-1"))
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request

	NewHandler(nil, nil).Delete(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), deleteConfirmation) {
		t.Fatalf("response does not explain exact confirmation: %s", recorder.Body.String())
	}
}
