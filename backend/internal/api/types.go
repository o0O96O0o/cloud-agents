package api

type createTaskRequest struct {
	Username string            `json:"username" binding:"required"`
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
}

type respondToPermissionRequest struct {
	Decision string `json:"decision" binding:"required"` // "allow" or "deny"
}

type respondToQuestionRequest struct {
	Answers map[string]any `json:"answers" binding:"required"`
}

type healthResponse struct {
	Status string `json:"status"`
}
