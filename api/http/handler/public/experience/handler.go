package experience

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	service "github.com/good-fish-man/agent-runtime-client/application/service/experience"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/experience"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

type Handler struct{ service *service.Service }

func NewHandler(value *service.Service) *Handler { return &Handler{service: value} }

type updatePreferenceRequest struct {
	LearningEnabled *bool  `json:"learning_enabled"`
	RetentionDays   *int   `json:"retention_days"`
	MaxSensitivity  string `json:"max_sensitivity"`
}

func (h *Handler) GetPreference(c *gin.Context) {
	value, err := h.service.Preference(c.Request.Context(), userID(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) UpdatePreference(c *gin.Context) {
	var request updatePreferenceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	current, err := h.service.Preference(c.Request.Context(), userID(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if request.LearningEnabled != nil {
		current.LearningEnabled = *request.LearningEnabled
	}
	if request.RetentionDays != nil {
		current.RetentionDays = *request.RetentionDays
	}
	if strings.TrimSpace(request.MaxSensitivity) != "" {
		current.MaxSensitivity = strings.TrimSpace(request.MaxSensitivity)
	}
	value, err := h.service.SavePreference(c.Request.Context(), userID(c), *current)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) List(c *gin.Context) {
	filter := entity.ListFilter{
		Status: c.Query("status"), Outcome: c.Query("outcome"), FailureClass: c.Query("failure_class"),
		Sensitivity: c.Query("sensitivity"), Query: c.Query("query"), Limit: queryInt(c, "limit", 50), Offset: queryInt(c, "offset", 0),
	}
	items, total, err := h.service.List(c.Request.Context(), userID(c), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items, "total": total, "limit": filter.Limit, "offset": filter.Offset})
}

func (h *Handler) Find(c *gin.Context) {
	value, err := h.service.Find(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), userID(c), c.Param("id")); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, nil)
}

func (h *Handler) Export(c *gin.Context) {
	value, err := h.service.Export(c.Request.Context(), userID(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) DeleteAll(c *gin.Context) {
	var request struct {
		Confirmation string `json:"confirmation"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Confirmation != "DELETE ALL EXPERIENCE" {
		_ = c.Error(apierror.ErrBadRequest.WithMessage("confirmation must equal DELETE ALL EXPERIENCE"))
		return
	}
	deleted, err := h.service.DeleteAll(c.Request.Context(), userID(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"deleted": deleted})
}

func (h *Handler) Search(c *gin.Context) {
	var request entity.SearchRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	items, err := h.service.Search(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items, "historical_only": true})
}

func (h *Handler) Stats(c *gin.Context) {
	ownerID := userID(c)
	if c.Query("scope") == "all" {
		if c.GetInt(consts.CtxKeyAdminLevel) <= 0 {
			_ = c.Error(apierror.ErrForbidden.WithMessage("administrator access is required"))
			return
		}
		ownerID = ""
	}
	value, err := h.service.Stats(c.Request.Context(), ownerID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, value)
}

func (h *Handler) CreateFixture(c *gin.Context) {
	var request service.CreateFixtureRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	request.ExperienceID = c.Param("id")
	value, err := h.service.CreateFixture(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListFixtures(c *gin.Context) {
	items, err := h.service.ListFixtures(c.Request.Context(), userID(c), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) CreateSuite(c *gin.Context) {
	var request service.CreateSuiteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	value, err := h.service.CreateSuite(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, value)
}

func (h *Handler) ListSuites(c *gin.Context) {
	items, err := h.service.ListSuites(c.Request.Context(), userID(c), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) RunSuite(c *gin.Context) {
	var request service.RunSuiteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	request.SuiteID = c.Param("id")
	run, results, err := h.service.RunSuite(c.Request.Context(), userID(c), request)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusCreated, gin.H{"run": run, "results": results})
}

func (h *Handler) ListRuns(c *gin.Context) {
	items, err := h.service.ListRuns(c.Request.Context(), userID(c), queryInt(c, "limit", 50))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func (h *Handler) ListResults(c *gin.Context) {
	items, err := h.service.ListResults(c.Request.Context(), userID(c), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"items": items})
}

func userID(c *gin.Context) string { return c.GetString(consts.CtxKeyUserID) }

func queryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.DefaultQuery(key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}
