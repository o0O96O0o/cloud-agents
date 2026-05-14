package api

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// WorkspaceHandler serves the workspace and execd proxy APIs.
type WorkspaceHandler struct {
	store           TaskStore
	workspaceReader WorkspaceReader
	serverURL       string
	sandboxAPIKey   string
	httpClient      *http.Client
}

// NewWorkspaceHandler constructs a WorkspaceHandler from its dependencies.
func NewWorkspaceHandler(store TaskStore, wr WorkspaceReader, serverURL, apiKey string) *WorkspaceHandler {
	return &WorkspaceHandler{
		store:           store,
		workspaceReader: wr,
		serverURL:       serverURL,
		sandboxAPIKey:   apiKey,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
	}
}

// FileInfo is a single file or directory entry in a workspace listing.
type FileInfo struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"modTime"`
}

// WorkspaceFiles handles GET /api/tasks/:id/workspace/files?path=<absolute-dir>.
//
// @Summary      List workspace directory via OFS
// @Tags         tasks
// @Param        id    path   string  true  "Task ID"
// @Param        path  query  string  true  "Absolute directory path"
// @Success      200  {array}   FileInfo
// @Failure      400  {object}  errorResponse  "path is required"
// @Failure      404  {object}  errorResponse  "task not found"
// @Failure      409  {object}  errorResponse  "OFS not configured"
// @Failure      500  {object}  errorResponse  "internal error"
// @Router       /api/tasks/{id}/workspace/files [get]
func (h *WorkspaceHandler) WorkspaceFiles(c *gin.Context) {
	taskID := c.Param("id")
	dir := c.Query("path")
	if dir == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "path is required"})
		return
	}

	t, err := h.store.Get(c.Request.Context(), taskID)
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, errorResponse{Error: "task not found"})
		return
	}
	if checkTaskOwner(c, t.Username) {
		return
	}

	if h.workspaceReader == nil {
		c.JSON(http.StatusConflict, errorResponse{Error: "OFS not configured"})
		return
	}

	entries, err := h.workspaceReader.ListWorkspace(c.Request.Context(), t.Username, taskID, dir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}

	result := make([]FileInfo, len(entries))
	for i, e := range entries {
		result[i] = FileInfo{
			Path:    e.Path,
			Name:    e.Name,
			IsDir:   e.IsDir,
			Size:    e.Size,
			ModTime: e.ModTime.UTC().Format(time.RFC3339),
		}
	}
	c.JSON(http.StatusOK, result)
}

// WorkspaceFile handles GET /api/tasks/:id/workspace/file?path=<absolute-file>.
//
// @Summary      Download workspace file via OFS
// @Tags         tasks
// @Param        id    path   string  true  "Task ID"
// @Param        path  query  string  true  "Absolute file path"
// @Success      200
// @Failure      400  {object}  errorResponse  "path is required"
// @Failure      404  {object}  errorResponse  "task not found or file not found"
// @Failure      409  {object}  errorResponse  "OFS not configured"
// @Router       /api/tasks/{id}/workspace/file [get]
func (h *WorkspaceHandler) WorkspaceFile(c *gin.Context) {
	taskID := c.Param("id")
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "path is required"})
		return
	}

	t, err := h.store.Get(c.Request.Context(), taskID)
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, errorResponse{Error: "task not found"})
		return
	}
	if checkTaskOwner(c, t.Username) {
		return
	}

	if h.workspaceReader == nil {
		c.JSON(http.StatusConflict, errorResponse{Error: "OFS not configured"})
		return
	}

	data, err := h.workspaceReader.GetWorkspaceFile(c.Request.Context(), t.Username, taskID, filePath)
	if err != nil {
		c.JSON(http.StatusNotFound, errorResponse{Error: "file not found"})
		return
	}

	c.Data(http.StatusOK, "application/octet-stream", data)
}

// ExecdProxy proxies filesystem and command requests to the execd daemon running
// inside a task's sandbox on port 44772.
//
// @Summary      Proxy execd filesystem API
// @Description  Forwards any method+path to the execd daemon (port 44772) inside the task's sandbox.
// @Tags         tasks
// @Param        id    path  string  true  "Task ID"
// @Param        path  path  string  true  "Execd sub-path (e.g. files/search)"
// @Success      200
// @Failure      404   {object}  errorResponse  "task not found"
// @Failure      409   {object}  errorResponse  "sandbox not running"
// @Failure      502   {object}  errorResponse  "execd unreachable"
// @Router       /api/tasks/{id}/execd/{path} [get]
func (h *WorkspaceHandler) ExecdProxy(c *gin.Context) {
	taskID := c.Param("id")
	subpath := c.Param("path")

	t, err := h.store.Get(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, errorResponse{Error: "task not found"})
		return
	}

	sandboxID := t.GetSandboxID()
	if sandboxID == "" {
		c.JSON(http.StatusConflict, errorResponse{Error: "sandbox not running"})
		return
	}

	target := fmt.Sprintf("%s/sandboxes/%s/proxy/44772%s", h.serverURL, sandboxID, subpath)
	if q := c.Request.URL.RawQuery; q != "" {
		target += "?" + q
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	req.Header.Set("X-OPEN-SANDBOX-API-KEY", h.sandboxAPIKey)
	if ct := c.GetHeader("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, errorResponse{Error: err.Error()})
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			c.Header(k, v)
		}
	}
	c.Status(resp.StatusCode)
	io.Copy(c.Writer, resp.Body) //nolint:errcheck
}
