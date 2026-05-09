package api

type createConversationRequest struct {
	Env map[string]string `json:"env,omitempty"`
}

type createConversationResponse struct {
	ID string `json:"id"`
}

type sendMessageRequest struct {
	Prompt string `json:"prompt"`
}

type getConversationResponse struct {
	ID             string `json:"id"`
	SandboxState   string `json:"sandbox_state"`
	SandboxID      string `json:"sandbox_id"`
	AgentSessionID string `json:"agent_session_id"`
}

type healthResponse struct {
	Status string `json:"status"`
}
