package api

import "time"

// Shared request/response types used across multiple handler domains.

type createTaskRequest struct {
	Username string            `json:"username" binding:"required"`
	Title    string            `json:"title,omitempty"`
	GitURL   string            `json:"git_url,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
}

type createTaskResponse struct {
	ID string `json:"id"`
}

type sendMessageRequest struct {
	Prompt string `json:"prompt" binding:"required"`
}

type getTaskResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	State     string `json:"state"`
	SandboxID string `json:"sandbox_id"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	CWD       string `json:"cwd"`
	GitURL    string `json:"git_url,omitempty"`
	ErrorMsg  string `json:"error_msg,omitempty"`
}

type respondToPermissionRequest struct {
	Decision string `json:"decision" binding:"required"`
}

type respondToQuestionRequest struct {
	Answers map[string]any `json:"answers" binding:"required"`
}

type healthResponse struct {
	Status string `json:"status"`
}

type taskListItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	GitURL    string    `json:"git_url,omitempty"`
	ErrorMsg  string    `json:"error_msg,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type runtimeConfigResponse struct {
	LoginMode     string `json:"loginMode"`
	PasswordLogin bool   `json:"passwordLogin"`
	AllowRegister bool   `json:"allowRegister"`
	OIDCLoginText string `json:"oidcLoginText,omitempty"`
	SSOLoginText  string `json:"ssoLoginText,omitempty"`
}
