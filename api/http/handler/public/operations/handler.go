package operations

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"

	"github.com/good-fish-man/agent-runtime-client/api/http/middleware"
	operationssvc "github.com/good-fish-man/agent-runtime-client/application/service/operations"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
)

type Handler struct{ service *operationssvc.Service }

func NewHandler(service *operationssvc.Service) *Handler { return &Handler{service: service} }

func (h *Handler) Snapshot(c *gin.Context) {
	c.JSON(http.StatusOK, h.service.Snapshot(c.Request.Context(), authctx.UserID(c.Request.Context())))
}

func (h *Handler) ListBackups(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	manager := h.service.BackupManager()
	if manager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backup management is not configured"})
		return
	}
	items, err := manager.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) CreateBackup(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	manager := h.service.BackupManager()
	if manager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backup management is not configured"})
		return
	}
	manifest, err := manager.Create(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, manifest)
}

// CreateBackupInternal is used by the local launcher before replacing a
// running release. It deliberately accepts only the machine-local service
// token and never a browser session.
func (h *Handler) CreateBackupInternal(c *gin.Context) {
	if !middleware.InternalTokenValid(c.GetHeader(consts.HeaderAthenaInternalToken)) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid internal service token"})
		return
	}
	manager := h.service.BackupManager()
	if manager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backup management is not configured"})
		return
	}
	manifest, err := manager.Create(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, manifest)
}

func (h *Handler) VerifyBackup(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	manager := h.service.BackupManager()
	if manager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backup management is not configured"})
		return
	}
	manifest, err := manager.Verify(c.Request.Context(), c.Param("backup_id"))
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, manifest)
}

func (h *Handler) RestoreBackup(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	manager := h.service.BackupManager()
	if manager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "backup management is not configured"})
		return
	}
	var request operationsv1.RestoreRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	request.Schema = operationsv1.Schema
	request.RestoreID = "restore-" + c.Param("backup_id") + "-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	request.BackupID = c.Param("backup_id")
	request.TargetVersion = consts.Version
	request.RequestedBy = authctx.UserID(c.Request.Context())
	request.RequestedAt = time.Now().UTC()
	request.Confirmation = strings.TrimSpace(request.Confirmation)
	manifest, err := manager.Restore(c.Request.Context(), request)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backup": manifest, "validate_only": request.ValidateOnly, "restored": !request.ValidateOnly})
}

func requireAdmin(c *gin.Context) bool {
	if c.GetInt(consts.CtxKeyAdminLevel) <= 0 && c.GetUint(consts.CtxKeyAdminLevel) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "administrator access is required"})
		return false
	}
	return true
}
