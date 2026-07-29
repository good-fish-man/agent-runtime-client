package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/runtime"
	service "github.com/good-fish-man/agent-runtime-client/application/service/runtime"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
	"github.com/good-fish-man/agent-runtime-client/pkg/validate"
	"github.com/good-fish-man/agent-runtime-client/types/apierror"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	"github.com/good-fish-man/agent-runtime-client/types/response"
)

// Run handles POST /v1/run (non-streaming).
func (h *Handler) Run(c *gin.Context) {
	var req dto.RunReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.Run(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// RunStream handles POST /v1/run/stream (SSE).
func (h *Handler) RunStream(c *gin.Context) {
	var req dto.RunReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	h.stream(c, func(emit service.StreamFunc) error {
		return h.svc.RunStream(c.Request.Context(), &req, emit)
	})
}

// Agent handles POST /v1/agent (non-streaming).
func (h *Handler) Agent(c *gin.Context) {
	var req dto.AgentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.RunAgent(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// AgentStream handles POST /v1/agent/stream (SSE).
func (h *Handler) AgentStream(c *gin.Context) {
	var req dto.AgentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	h.stream(c, func(emit service.StreamFunc) error {
		return h.svc.RunAgentStream(c.Request.Context(), &req, emit)
	})
}

// GenerateMedia handles POST /v1/media/generate.
func (h *Handler) GenerateMedia(c *gin.Context) {
	var req dto.MediaGenerationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	result, err := h.svc.GenerateMedia(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, result)
}

// CreateMediaJob queues a durable image or video generation.
func (h *Handler) CreateMediaJob(c *gin.Context) {
	var req dto.MediaGenerationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	if err := validate.Struct(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	job, err := h.svc.CreateMediaJob(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.OkStatus(c, http.StatusAccepted, job)
}

func (h *Handler) ListMediaJobs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	jobs, err := h.svc.ListMediaJobs(c.Request.Context(), c.Query("mediaType"), limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, jobs)
}

func (h *Handler) FindMediaJob(c *gin.Context) {
	job, err := h.svc.FindMediaJob(c.Request.Context(), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, job)
}

func (h *Handler) DeleteMediaJob(c *gin.Context) {
	if err := h.svc.DeleteMediaJob(c.Request.Context(), c.Param("id")); err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, gin.H{"deleted": true})
}

// Resume handles POST /v1/resume.
func (h *Handler) Resume(c *gin.Context) {
	var req dto.ResumeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.Resume(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// Stop handles POST /v1/stop.
func (h *Handler) Stop(c *gin.Context) {
	var req dto.StopReq
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apierror.ErrBadRequest.WithMessage(err.Error()))
		return
	}
	res, err := h.svc.Stop(c.Request.Context(), &req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Ok(c, res)
}

// stream sets SSE headers and pumps events produced by run to the client. Once
// the stream has started, upstream errors are delivered as an SSE "error" event
// rather than an envelope (the response is already committed).
func (h *Handler) stream(c *gin.Context, run func(emit service.StreamFunc) error) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		_ = c.Error(apierror.ErrInternal.WithMessage("streaming not supported"))
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.WriteHeaderNow()

	emit := func(ev *entity.StreamEvent) error {
		if err := writeSSE(c.Writer, ev.Type, ev.Payload()); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := run(emit); err != nil {
		_ = c.Error(err)
		apiErr := apierror.FromError(err)
		if writeErr := writeSSE(c.Writer, consts.SSEEventError, gin.H{"code": apiErr.Code, "message": apiErr.Message, "trace_id": c.GetString(consts.CtxKeyTraceID)}); writeErr != nil {
			_ = c.Error(fmt.Errorf("write stream error event: %w", writeErr))
		}
		flusher.Flush()
	}
}

func writeSSE(w gin.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	return err
}
