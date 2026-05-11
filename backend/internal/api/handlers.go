package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/your-org/platform-backend/internal/storage"
	"github.com/your-org/platform-backend/internal/task"
)

// TaskStore is the storage interface for Task records.
type TaskStore = task.Repository

// SandboxManager provisions and tears down the compute sandbox that backs a task.
type SandboxManager interface {
	// ProvisionForTask allocates a sandbox for t and attaches its ID to t.
	ProvisionForTask(ctx context.Context, t *task.Task) error
	// DeleteSandbox destroys the sandbox identified by sandboxID.
	DeleteSandbox(ctx context.Context, sandboxID string) error
	// IsSandboxAlive reports whether sandboxID is still in Running state.
	// Returns (false, nil) when the sandbox has expired or been terminated.
	IsSandboxAlive(ctx context.Context, sandboxID string) (bool, error)
}

// FileStore retrieves task history from OFS-backed file storage.
type FileStore interface {
	ListHistory(ctx context.Context, username, taskID string) ([]string, error)
	GetHistory(ctx context.Context, key string) ([]storage.ConversationEntry, error)
	GetSessionMeta(ctx context.Context, username, taskID string) (*storage.SessionMeta, error)
}

// MessageProxy streams a prompt from the client through to the task's sandbox.
type MessageProxy interface {
	// StreamMessage forwards prompt to the sandbox associated with t and writes
	// the streamed response directly to w.
	StreamMessage(ctx context.Context, t *task.Task, prompt string, w http.ResponseWriter) error
}

// Handler wires together the store, sandbox manager, message proxy, and file store
// to serve the tasks REST API.
type Handler struct {
	store     TaskStore
	manager   SandboxManager
	proxy     MessageProxy
	fileStore FileStore
}

// NewHandler constructs a Handler from its dependencies.
func NewHandler(store TaskStore, mgr SandboxManager, proxy MessageProxy, fileStore FileStore) *Handler {
	return &Handler{
		store:     store,
		manager:   mgr,
		proxy:     proxy,
		fileStore: fileStore,
	}
}

// CreateTask handles POST /api/tasks.
func (h *Handler) CreateTask(c *gin.Context) {
	var body createTaskRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.String(http.StatusBadRequest, "invalid request body")
		return
	}

	t, err := h.store.Create(c.Request.Context(), body.Username, body.Env)
	if err != nil {
		log.Printf("create task: %v", err)
		c.String(http.StatusInternalServerError, "failed to create task")
		return
	}
	c.JSON(http.StatusCreated, createTaskResponse{ID: t.ID})
}

// SendMessage handles POST /api/tasks/:id/messages.
//
// Lazily provisions the task's sandbox on first use, then streams the
// assistant response back to the caller. Provisioning runs under a background
// context so that a client disconnect does not abort it.
func (h *Handler) SendMessage(c *gin.Context) {
	id := c.Param("id")
	t, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		log.Printf("get task %s: %v", id, err)
		c.String(http.StatusInternalServerError, "failed to get task")
		return
	}
	if t == nil {
		c.String(http.StatusNotFound, "task not found")
		return
	}

	var body sendMessageRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.String(http.StatusBadRequest, "prompt is required")
		return
	}

	// Sandboxes expire silently via TTL. ResetIfExpired checks liveness under the
	// provisioning lock so a concurrent re-provision cannot be stomped by a racing reset.
	if err := t.ResetIfExpired(func(sandboxID string) (bool, error) {
		aliveCtx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		alive, err := h.manager.IsSandboxAlive(aliveCtx, sandboxID)
		if err != nil {
			log.Printf("sandbox status check failed for task %s sandbox %s: %v", id, sandboxID, err)
			return true, err // treat check error as alive — proxy will surface real errors
		}
		if !alive {
			log.Printf("sandbox %s expired for task %s, re-provisioning", sandboxID, id)
		}
		return alive, nil
	}); err != nil {
		log.Printf("sandbox liveness check error for task %s, proceeding: %v", id, err)
	}

	if t.GetState() == task.StateNew {
		t.SetProvisioning()
	}

	// Use background context so provisioning survives client disconnects.
	provisionCtx := context.Background()
	err = t.EnsureProvisioned(func() error {
		return h.manager.ProvisionForTask(provisionCtx, t)
	})
	if err != nil {
		t.SetError()
		log.Printf("provision failed for task %s: %v", id, err)
		c.String(http.StatusBadGateway, "failed to provision sandbox")
		return
	}

	if err := h.proxy.StreamMessage(c.Request.Context(), t, body.Prompt, c.Writer); err != nil {
		if c.Request.Context().Err() != nil {
			return // client disconnected
		}
		log.Printf("stream error for task %s: %v", id, err)
	}
}

// GetTask handles GET /api/tasks/:id.
func (h *Handler) GetTask(c *gin.Context) {
	id := c.Param("id")
	t, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		log.Printf("get task %s: %v", id, err)
		c.String(http.StatusInternalServerError, "failed to get task")
		return
	}
	if t == nil {
		c.String(http.StatusNotFound, "task not found")
		return
	}

	_, sandboxID, sessionID, stateStr := t.Info()
	c.JSON(http.StatusOK, getTaskResponse{
		ID:        id,
		Username:  t.Username,
		State:     stateStr,
		SandboxID: sandboxID,
		SessionID: sessionID,
	})
}

// DeleteTask handles DELETE /api/tasks/:id.
func (h *Handler) DeleteTask(c *gin.Context) {
	id := c.Param("id")
	t, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		log.Printf("get task %s: %v", id, err)
		c.String(http.StatusInternalServerError, "failed to get task")
		return
	}
	if t == nil {
		c.String(http.StatusNotFound, "task not found")
		return
	}

	sandboxID := t.GetSandboxID()
	if err := h.store.Delete(c.Request.Context(), id); err != nil {
		log.Printf("delete task %s: %v", id, err)
		c.String(http.StatusInternalServerError, "failed to delete task")
		return
	}

	if sandboxID != "" {
		if err := h.manager.DeleteSandbox(context.Background(), sandboxID); err != nil {
			log.Printf("delete sandbox %s: %v", sandboxID, err)
		}
	}

	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
}

// GetTaskHistory handles GET /api/tasks/:id/history.
func (h *Handler) GetTaskHistory(c *gin.Context) {
	id := c.Param("id")
	t, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		log.Printf("get task %s: %v", id, err)
		c.String(http.StatusInternalServerError, "failed to get task")
		return
	}
	if t == nil {
		c.String(http.StatusNotFound, "task not found")
		return
	}

	if h.fileStore == nil {
		c.String(http.StatusServiceUnavailable, "history storage not configured")
		return
	}

	keys, err := h.fileStore.ListHistory(c.Request.Context(), t.Username, id)
	if err != nil {
		log.Printf("list history for task %s: %v", id, err)
		c.String(http.StatusInternalServerError, "failed to list history")
		return
	}

	entries := make([]storage.ConversationEntry, 0)
	if len(keys) > 0 {
		entries, err = h.fileStore.GetHistory(c.Request.Context(), keys[0])
		if err != nil {
			log.Printf("get history for task %s: %v", id, err)
			c.String(http.StatusInternalServerError, "failed to get history")
			return
		}
		if entries == nil {
			entries = make([]storage.ConversationEntry, 0)
		}
	}

	c.JSON(http.StatusOK, entries)
}

// Health handles GET /health.
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, healthResponse{Status: "ok"})
}
