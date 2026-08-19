package delegationops

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	delegationsvc "github.com/good-fish-man/agent-runtime-client/application/service/delegation"
	delegationrepo "github.com/good-fish-man/agent-runtime-client/domain/irepository/delegation"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/pkg/ulid"
	dso "github.com/good-fish-man/athena-protocol/draft/dso/v0alpha"
)

const deleteConfirmation = "DELETE DELEGATION DATA"

type Handler struct {
	recovery *delegationsvc.RecoveryService
	replay   *delegationsvc.ReplayRunner
}

func NewHandler(recovery *delegationsvc.RecoveryService, replay *delegationsvc.ReplayRunner) *Handler {
	return &Handler{recovery: recovery, replay: replay}
}

func (h *Handler) Replay(c *gin.Context) {
	ownerID, ok := authenticatedOwner(c)
	if !ok {
		return
	}
	var request dso.ReplayRequest
	if !decodeJSON(c, &request) {
		return
	}
	request.Schema = dso.Schema
	if strings.TrimSpace(request.ReplayID) == "" {
		request.ReplayID = "replay-" + ulid.New()
	}
	request.OwnerID = ownerID
	request.RequestedBy = ownerID
	request.CreatedAt = time.Now().UTC()
	result, err := h.replay.Run(c.Request.Context(), request)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, delegationrepo.ErrIdempotencyConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error(), "result": result})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *Handler) Export(c *gin.Context) {
	ownerID, ok := authenticatedOwner(c)
	if !ok {
		return
	}
	request := dso.DataLifecycleRequest{
		Schema: dso.Schema, RequestID: "lifecycle-" + ulid.New(), OwnerID: ownerID,
		Operation: dso.DataLifecycleExport, RequestedBy: ownerID, CreatedAt: time.Now().UTC(),
	}
	data, err := h.recovery.ExportOwnerData(c.Request.Context(), request)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="athena-delegation-export.json"`)
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

func (h *Handler) Delete(c *gin.Context) {
	ownerID, ok := authenticatedOwner(c)
	if !ok {
		return
	}
	var input struct {
		Cutoff       time.Time `json:"cutoff"`
		Confirmation string    `json:"confirmation"`
	}
	if !decodeJSON(c, &input) {
		return
	}
	if input.Confirmation != deleteConfirmation {
		c.JSON(http.StatusBadRequest, gin.H{"error": "confirmation must exactly match " + deleteConfirmation})
		return
	}
	request := dso.DataLifecycleRequest{
		Schema: dso.Schema, RequestID: "lifecycle-" + ulid.New(), OwnerID: ownerID,
		Operation: dso.DataLifecycleDelete, Cutoff: input.Cutoff.UTC(), RequestedBy: ownerID, CreatedAt: time.Now().UTC(),
	}
	result, err := h.recovery.DeleteOwnerData(c.Request.Context(), request)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func authenticatedOwner(c *gin.Context) (string, bool) {
	ownerID := strings.TrimSpace(authctx.UserID(c.Request.Context()))
	if ownerID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated owner is required"})
		return "", false
	}
	return ownerID, true
}

func decodeJSON(c *gin.Context, destination any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return false
	}
	return true
}
