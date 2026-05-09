package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/your-org/platform-backend/internal/conversation"
)

// ---- mock types ----

type mockStore struct {
	mu   sync.Mutex
	conv *conversation.Conversation
}

func (m *mockStore) Create(env map[string]string) *conversation.Conversation {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := conversation.NewStore()
	m.conv = s.Create(env)
	return m.conv
}

func (m *mockStore) Get(id string) *conversation.Conversation {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conv != nil && m.conv.ID == id {
		return m.conv
	}
	return nil
}

func (m *mockStore) Delete(_ string) {}

type mockManager struct {
	provisionErr error
	calls        atomic.Int32
	sandboxAlive bool
	aliveErr     error
}

func (m *mockManager) ProvisionForConversation(_ context.Context, conv *conversation.Conversation) error {
	m.calls.Add(1)
	return m.provisionErr
}

func (m *mockManager) DeleteSandbox(_ context.Context, _ string) error { return nil }

func (m *mockManager) IsSandboxAlive(_ context.Context, _ string) (bool, error) {
	return m.sandboxAlive, m.aliveErr
}

type mockProxy struct {
	err error
}

func (m *mockProxy) StreamMessage(_ context.Context, _ *conversation.Conversation, _ string, w http.ResponseWriter) error {
	return m.err
}

// ---- helpers ----

func newHandler(store ConversationStore, mgr SandboxManager, proxy MessageProxy) *Handler {
	return NewHandler(store, mgr, proxy)
}

func convWithSandbox(sandboxID, sessionID string) *conversation.Conversation {
	s := conversation.NewStore()
	conv := s.Create(nil)
	conv.SetRunning(sandboxID, "http://proxy/", map[string]string{})
	if sessionID != "" {
		conv.SetAgentSessionID(sessionID)
	}
	return conv
}

// provisionedConv returns a conversation that went through EnsureProvisioned so
// provisioned=true, simulating a session that was previously fully provisioned.
func provisionedConv(sandboxID string) *conversation.Conversation {
	s := conversation.NewStore()
	conv := s.Create(nil)
	conv.EnsureProvisioned(func() error {
		conv.SetRunning(sandboxID, "http://proxy/", map[string]string{})
		return nil
	})
	return conv
}

// ---- CreateConversation ----

func TestCreateConversation_NoBody(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations", nil)
	rw := httptest.NewRecorder()
	h.CreateConversation(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rw.Code)
	}
	var body map[string]string
	json.NewDecoder(rw.Body).Decode(&body)
	if body["id"] == "" {
		t.Error("expected non-empty id in response")
	}
}

func TestCreateConversation_WithEnv(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations",
		strings.NewReader(`{"env":{"FOO":"bar"}}`))
	rw := httptest.NewRecorder()
	h.CreateConversation(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rw.Code)
	}
	store.mu.Lock()
	env := store.conv.ExtraEnv()
	store.mu.Unlock()
	if env["FOO"] != "bar" {
		t.Errorf("expected FOO=bar in conversation env, got %v", env)
	}
}

func TestCreateConversation_InvalidJSON(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations",
		strings.NewReader(`{bad json`))
	rw := httptest.NewRecorder()
	h.CreateConversation(rw, req)

	// Invalid JSON is ignored; conversation still created.
	if rw.Code != http.StatusCreated {
		t.Fatalf("expected 201 even with bad JSON, got %d", rw.Code)
	}
}

// ---- GetConversation ----

func TestGetConversation_Found(t *testing.T) {
	conv := convWithSandbox("sb-1", "sess-1")
	store := &mockStore{conv: conv}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodGet, "/api/conversations/"+conv.ID, nil)
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.GetConversation(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	var body map[string]any
	json.NewDecoder(rw.Body).Decode(&body)
	if body["sandbox_state"] != "running" {
		t.Errorf("expected sandbox_state=running, got %v", body["sandbox_state"])
	}
	if body["sandbox_id"] != "sb-1" {
		t.Errorf("expected sandbox_id=sb-1, got %v", body["sandbox_id"])
	}
	if body["agent_session_id"] != "sess-1" {
		t.Errorf("expected agent_session_id=sess-1, got %v", body["agent_session_id"])
	}
}

func TestGetConversation_NotFound(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodGet, "/api/conversations/missing", nil)
	req.SetPathValue("id", "missing")
	rw := httptest.NewRecorder()
	h.GetConversation(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rw.Code)
	}
}

// ---- SendMessage ----

func TestSendMessage_NotFound(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/missing/messages",
		strings.NewReader(`{"prompt":"hi"}`))
	req.SetPathValue("id", "missing")
	rw := httptest.NewRecorder()
	h.SendMessage(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rw.Code)
	}
}

func TestSendMessage_EmptyPrompt(t *testing.T) {
	conv := convWithSandbox("", "")
	store := &mockStore{conv: conv}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		strings.NewReader(`{"prompt":""}`))
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.SendMessage(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rw.Code)
	}
}

func TestSendMessage_NoPromptField(t *testing.T) {
	conv := convWithSandbox("", "")
	store := &mockStore{conv: conv}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		strings.NewReader(`{}`))
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.SendMessage(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rw.Code)
	}
}

func TestSendMessage_ProvisionError(t *testing.T) {
	s := conversation.NewStore()
	conv := s.Create(nil)
	store := &mockStore{conv: conv}
	mgr := &mockManager{provisionErr: errors.New("quota exceeded")}
	h := newHandler(store, mgr, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		strings.NewReader(`{"prompt":"hi"}`))
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.SendMessage(rw, req)

	if rw.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rw.Code)
	}
	if conv.GetState() != conversation.StateError {
		t.Errorf("expected StateError after provision failure, got %v", conv.GetState())
	}
}

func TestSendMessage_Success(t *testing.T) {
	s := conversation.NewStore()
	conv := s.Create(nil)
	store := &mockStore{conv: conv}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		strings.NewReader(`{"prompt":"hello"}`))
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.SendMessage(rw, req)

	// After streaming, status code depends on when WriteHeader was called.
	// With a mock proxy that writes nothing, the recorder default is 200.
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
}

func TestSendMessage_ClientDisconnect(t *testing.T) {
	s := conversation.NewStore()
	conv := s.Create(nil)
	store := &mockStore{conv: conv}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled — simulates client disconnect

	// Proxy that returns nil when context is cancelled.
	h := newHandler(store, &mockManager{}, &mockProxy{err: nil})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		strings.NewReader(`{"prompt":"hi"}`))
	req = req.WithContext(ctx)
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()

	// Should not panic or log an error.
	h.SendMessage(rw, req)
}

func TestSendMessage_ProvisionCalledOnce(t *testing.T) {
	s := conversation.NewStore()
	conv := s.Create(nil)
	store := &mockStore{conv: conv}
	mgr := &mockManager{}
	h := newHandler(store, mgr, &mockProxy{})

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
				strings.NewReader(`{"prompt":"hi"}`))
			req.SetPathValue("id", conv.ID)
			rw := httptest.NewRecorder()
			h.SendMessage(rw, req)
		}()
	}
	wg.Wait()

	if mgr.calls.Load() != 1 {
		t.Errorf("expected ProvisionForConversation called once, called %d times", mgr.calls.Load())
	}
}

func TestSendMessage_SandboxAlive_NoReprovision(t *testing.T) {
	conv := provisionedConv("sb-alive")
	store := &mockStore{conv: conv}
	mgr := &mockManager{sandboxAlive: true}
	h := newHandler(store, mgr, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		strings.NewReader(`{"prompt":"hi"}`))
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.SendMessage(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	if mgr.calls.Load() != 0 {
		t.Errorf("expected no re-provisioning when sandbox is alive, got %d calls", mgr.calls.Load())
	}
}

func TestSendMessage_SandboxExpired_Reprovisions(t *testing.T) {
	conv := provisionedConv("sb-expired")
	store := &mockStore{conv: conv}
	// sandboxAlive=false simulates TTL expiry; provisioning succeeds.
	mgr := &mockManager{sandboxAlive: false}
	h := newHandler(store, mgr, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		strings.NewReader(`{"prompt":"hello after expiry"}`))
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.SendMessage(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200 after re-provision, got %d", rw.Code)
	}
	if mgr.calls.Load() != 1 {
		t.Errorf("expected ProvisionForConversation called once for re-provision, got %d", mgr.calls.Load())
	}
}

func TestSendMessage_SandboxAliveCheckError_Continues(t *testing.T) {
	conv := provisionedConv("sb-check-err")
	store := &mockStore{conv: conv}
	// aliveErr simulates a transient network error; state must be preserved and the
	// existing proxy should be used (ResetIfExpired does not reset on check error).
	mgr := &mockManager{sandboxAlive: false, aliveErr: errors.New("network timeout")}
	h := newHandler(store, mgr, &mockProxy{})

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conv.ID+"/messages",
		strings.NewReader(`{"prompt":"hi"}`))
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.SendMessage(rw, req)

	// Should proceed (not fail) — existing proxy URL is retained; it will surface errors
	// if the sandbox is truly dead.
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200 when alive check errors, got %d", rw.Code)
	}
	// No re-provisioning — sandbox was not reset because the check was inconclusive.
	if mgr.calls.Load() != 0 {
		t.Errorf("expected no re-provisioning on alive check error, got %d calls", mgr.calls.Load())
	}
}

// ---- DeleteConversation ----

func TestDeleteConversation_NotFound(t *testing.T) {
	store := &mockStore{}
	h := newHandler(store, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodDelete, "/api/conversations/missing", nil)
	req.SetPathValue("id", "missing")
	rw := httptest.NewRecorder()
	h.DeleteConversation(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rw.Code)
	}
}

func TestDeleteConversation_NoSandbox(t *testing.T) {
	s := conversation.NewStore()
	conv := s.Create(nil)
	store := &mockStore{conv: conv}
	mgr := &mockManager{}
	h := newHandler(store, mgr, &mockProxy{})

	req := httptest.NewRequest(http.MethodDelete, "/api/conversations/"+conv.ID, nil)
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.DeleteConversation(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rw.Code)
	}
	if mgr.calls.Load() != 0 {
		t.Error("DeleteSandbox should not be called when sandboxID is empty")
	}
}

func TestDeleteConversation_WithSandbox(t *testing.T) {
	conv := convWithSandbox("sb-del", "")
	store := &mockStore{conv: conv}
	var deletedID string
	mgr := &deletingManager{onDelete: func(id string) { deletedID = id }}
	h := newHandler(store, mgr, &mockProxy{})

	req := httptest.NewRequest(http.MethodDelete, "/api/conversations/"+conv.ID, nil)
	req.SetPathValue("id", conv.ID)
	rw := httptest.NewRecorder()
	h.DeleteConversation(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rw.Code)
	}
	if deletedID != "sb-del" {
		t.Errorf("expected DeleteSandbox called with sb-del, got %q", deletedID)
	}
}

// deletingManager records the sandbox ID passed to DeleteSandbox.
type deletingManager struct {
	onDelete func(string)
}

func (m *deletingManager) ProvisionForConversation(_ context.Context, _ *conversation.Conversation) error {
	return nil
}
func (m *deletingManager) DeleteSandbox(_ context.Context, id string) error {
	if m.onDelete != nil {
		m.onDelete(id)
	}
	return nil
}
func (m *deletingManager) IsSandboxAlive(_ context.Context, _ string) (bool, error) {
	return true, nil
}

// ---- Health ----

func TestHealth(t *testing.T) {
	h := newHandler(&mockStore{}, &mockManager{}, &mockProxy{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rw := httptest.NewRecorder()
	h.Health(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rw.Code)
	}
	var body map[string]string
	json.NewDecoder(rw.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body)
	}
}
