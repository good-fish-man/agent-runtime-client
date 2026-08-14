package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"

	service "github.com/good-fish-man/agent-runtime-client/application/service/control"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/control"
	"github.com/good-fish-man/agent-runtime-client/pkg/authctx"
)

type Handler struct {
	hub         *service.Hub
	deviceToken string
}

func NewHandler(hub *service.Hub, deviceToken string) *Handler {
	return &Handler{hub: hub, deviceToken: strings.TrimSpace(deviceToken)}
}

func (h *Handler) Register(engine *gin.Engine, auth gin.HandlerFunc, publicPrefixes ...string) {
	registered := map[string]bool{}
	h.registerRoutes(engine, "/v1/control", auth, registered)
	for _, prefix := range publicPrefixes {
		prefix = strings.Trim(strings.TrimSpace(prefix), "/")
		if prefix == "" {
			continue
		}
		h.registerRoutes(engine, "/"+prefix+"/control", auth, registered)
	}
}

func (h *Handler) registerRoutes(engine *gin.Engine, base string, auth gin.HandlerFunc, registered map[string]bool) {
	base = "/" + strings.Trim(strings.TrimSpace(base), "/")
	if base == "/" || registered[base] {
		return
	}
	registered[base] = true
	engine.GET(base+"/device", h.Device)
	group := engine.Group(base)
	if auth != nil {
		group.Use(auth)
	}
	group.GET("/devices", h.Devices)
	group.POST("/devices/:device_id/bind", h.BindDevice)
	group.GET("/tasks", h.Tasks)
	group.GET("/tasks/:task_id", h.Task)
	group.GET("/tasks/:task_id/events", h.TaskEvents)
	group.GET("/tasks/:task_id/events/stream", h.StreamTaskEvents)
	group.GET("/tasks/:task_id/world", h.TaskWorld)
	group.POST("/tasks/:task_id/cancel", h.CancelTask)
	group.GET("/approvals", h.Approvals)
	group.POST("/approvals/:approval_id/decision", h.DecideApproval)
	group.POST("/actions", h.Dispatch)
}

func (h *Handler) Approvals(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	values, err := h.hub.Approvals(c.Request.Context(), authctx.UserID(c.Request.Context()), strings.ToUpper(strings.TrimSpace(c.Query("status"))), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"approvals": values})
}

func (h *Handler) DecideApproval(c *gin.Context) {
	var request struct {
		Approved bool   `json:"approved"`
		Reason   string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 10*time.Minute)
	defer cancel()
	approval, observation, err := h.hub.DecideApproval(ctx, c.Param("approval_id"), authctx.UserID(c.Request.Context()), request.Approved, request.Reason)
	if errors.Is(err, service.ErrDeviceOffline) {
		c.JSON(http.StatusAccepted, gin.H{"approval": approval, "observation": observation, "pending_recovery": true})
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "approval": approval, "observation": observation})
		return
	}
	c.JSON(http.StatusOK, gin.H{"approval": approval, "observation": observation, "pending_recovery": false})
}

func (h *Handler) CancelTask(c *gin.Context) {
	task, ok := h.authorizedTask(c)
	if !ok {
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&request)
	if strings.TrimSpace(request.Reason) == "" {
		request.Reason = "user requested cancellation"
	}
	if err := h.hub.CancelTask(c.Request.Context(), task.TaskID, request.Reason); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task_id": task.TaskID, "status": entity.StatusCancelled})
}

func (h *Handler) Tasks(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	tasks, err := h.hub.Tasks(c.Request.Context(), authctx.UserID(c.Request.Context()), c.Query("conversation_id"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

func (h *Handler) Task(c *gin.Context) {
	task, ok, err := h.hub.Task(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "task session not found"})
		return
	}
	if userID := authctx.UserID(c.Request.Context()); userID != "" && task.UserID != "" && task.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "task session belongs to another user"})
		return
	}
	c.JSON(http.StatusOK, task)
}

func (h *Handler) TaskEvents(c *gin.Context) {
	task, ok := h.authorizedTask(c)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	events, err := h.hub.Events(c.Request.Context(), task.TaskID, after, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func (h *Handler) TaskWorld(c *gin.Context) {
	task, ok := h.authorizedTask(c)
	if !ok {
		return
	}
	world, err := h.hub.WorldState(c.Request.Context(), task.TaskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if world == nil {
		c.JSON(http.StatusOK, gin.H{"task_id": task.TaskID, "revision": 0, "state": gin.H{}})
		return
	}
	c.JSON(http.StatusOK, world)
}

func (h *Handler) StreamTaskEvents(c *gin.Context) {
	task, ok := h.authorizedTask(c)
	if !ok {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	eventChannel, unsubscribe := h.hub.Subscribe(task.TaskID)
	defer unsubscribe()
	recovery := time.NewTicker(5 * time.Second)
	heartbeat := time.NewTicker(15 * time.Second)
	defer recovery.Stop()
	defer heartbeat.Stop()
	emitPersisted := func() error {
		events, err := h.hub.Events(c.Request.Context(), task.TaskID, after, 200)
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.Sequence <= after {
				continue
			}
			c.SSEvent("task_event", event)
			after = event.Sequence
		}
		if len(events) > 0 {
			c.Writer.Flush()
		}
		return nil
	}
	if err := emitPersisted(); err != nil {
		c.SSEvent("error", gin.H{"message": err.Error()})
		c.Writer.Flush()
		return
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event := <-eventChannel:
			if event.Sequence <= after {
				continue
			}
			c.SSEvent("task_event", event)
			after = event.Sequence
			c.Writer.Flush()
		case <-heartbeat.C:
			c.SSEvent("heartbeat", gin.H{"sequence": after, "sent_at": time.Now().UTC()})
			c.Writer.Flush()
		case <-recovery.C:
			if err := emitPersisted(); err != nil {
				c.SSEvent("error", gin.H{"message": err.Error()})
				c.Writer.Flush()
				return
			}
		}
	}
}

func (h *Handler) authorizedTask(c *gin.Context) (entity.TaskSession, bool) {
	task, ok, err := h.hub.Task(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return entity.TaskSession{}, false
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "task session not found"})
		return entity.TaskSession{}, false
	}
	if userID := authctx.UserID(c.Request.Context()); userID != "" && task.UserID != "" && task.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "task session belongs to another user"})
		return entity.TaskSession{}, false
	}
	return task, true
}

func (h *Handler) Device(c *gin.Context) {
	if h.deviceToken == "" {
		if !loopbackRequest(c.Request) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "device token required"})
			return
		}
		websocket.Handler(h.serveDevice).ServeHTTP(c.Writer, c.Request)
		return
	}
	if !secureEqual(bearerToken(c.GetHeader("Authorization")), h.deviceToken) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid device token"})
		return
	}
	websocket.Handler(h.serveDevice).ServeHTTP(c.Writer, c.Request)
}

type websocketConnection struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *websocketConnection) Send(value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return websocket.JSON.Send(c.conn, value)
}

func (c *websocketConnection) Close() error { return c.conn.Close() }

func (h *Handler) serveDevice(socket *websocket.Conn) {
	connection := &websocketConnection{conn: socket}
	_ = socket.SetDeadline(time.Now().Add(20 * time.Second))
	var helloPayload string
	if err := websocket.Message.Receive(socket, &helloPayload); err != nil {
		_ = connection.Send(gin.H{"protocol": entity.Protocol, "type": entity.TypeError, "error": "HELLO is required"})
		return
	}
	var hello entity.DeviceMessage
	if err := entity.DecodeStrict([]byte(helloPayload), &hello); err != nil || hello.Protocol != entity.Protocol || hello.Type != entity.TypeHello {
		_ = connection.Send(gin.H{"protocol": entity.Protocol, "type": entity.TypeError, "error": "HELLO is required"})
		return
	}
	if err := h.hub.Register(context.Background(), hello, connection); err != nil {
		_ = connection.Send(gin.H{"protocol": entity.Protocol, "type": entity.TypeError, "error": err.Error()})
		return
	}
	defer h.hub.Unregister(context.Background(), hello.DeviceID, connection)
	_ = socket.SetDeadline(time.Now().Add(45 * time.Second))
	_ = connection.Send(gin.H{"protocol": entity.Protocol, "type": entity.TypeWelcome, "device_id": hello.DeviceID, "sent_at": time.Now().UTC()})
	for {
		var payload string
		if err := websocket.Message.Receive(socket, &payload); err != nil {
			return
		}
		var raw struct {
			Protocol string `json:"protocol"`
			Type     string `json:"type"`
		}
		if err := json.Unmarshal([]byte(payload), &raw); err != nil || raw.Protocol != entity.Protocol {
			continue
		}
		switch raw.Type {
		case entity.TypeHeartbeat:
			_ = socket.SetDeadline(time.Now().Add(45 * time.Second))
			h.hub.Touch(context.Background(), hello.DeviceID)
			_ = connection.Send(gin.H{"protocol": entity.Protocol, "type": entity.TypeHeartbeatAck, "sent_at": time.Now().UTC()})
		case entity.TypeObservation:
			_ = socket.SetDeadline(time.Now().Add(45 * time.Second))
			var observation entity.Observation
			if err := entity.DecodeStrict([]byte(payload), &observation); err != nil {
				continue
			}
			h.hub.Touch(context.Background(), hello.DeviceID)
			if err := h.hub.Observe(context.Background(), observation); err != nil {
				_ = connection.Send(gin.H{"protocol": entity.Protocol, "type": entity.TypeError, "error": err.Error()})
			}
		case entity.TypeProgress:
			_ = socket.SetDeadline(time.Now().Add(45 * time.Second))
			var progress entity.Progress
			if err := entity.DecodeStrict([]byte(payload), &progress); err != nil {
				continue
			}
			h.hub.Touch(context.Background(), hello.DeviceID)
			if err := h.hub.Progress(context.Background(), progress); err != nil {
				_ = connection.Send(gin.H{"protocol": entity.Protocol, "type": entity.TypeError, "error": err.Error()})
			}
		}
	}
}

func (h *Handler) Devices(c *gin.Context) {
	devices, err := h.hub.Devices(c.Request.Context(), authctx.UserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"devices": devices})
}

func (h *Handler) BindDevice(c *gin.Context) {
	if err := h.hub.BindDevice(c.Request.Context(), c.Param("device_id"), authctx.UserID(c.Request.Context())); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"device_id": c.Param("device_id"), "bound": true})
}

func (h *Handler) Dispatch(c *gin.Context) {
	var request struct {
		DeviceID string        `json:"device_id"`
		Action   entity.Action `json:"action"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID := authctx.UserID(c.Request.Context())
	deviceID, capabilityInstanceID, err := h.hub.ResolveCapability(c.Request.Context(), userID, request.DeviceID, request.Action.Capability, request.Action.CapabilityInstanceID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if request.Action.TaskID != "" {
		if err := h.hub.BeginTask(c.Request.Context(), request.Action.TaskID, userID, "", deviceID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	request.Action.CapabilityInstanceID = capabilityInstanceID
	observation, err := h.hub.Dispatch(c.Request.Context(), deviceID, request.Action)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, observation)
}

func bearerToken(value string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix))
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func loopbackRequest(request *http.Request) bool {
	host := request.RemoteAddr
	if splitHost, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		host = splitHost
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
