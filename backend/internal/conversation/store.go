package conversation

import (
	"sync"

	"github.com/google/uuid"
)

type State int

const (
	StateNew          State = iota
	StateProvisioning       // sandbox being created
	StateRunning            // sandbox up, agent ready
	StateError
)

func (s State) String() string {
	switch s {
	case StateNew, StateProvisioning:
		return "provisioning"
	case StateRunning:
		return "running"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

type Conversation struct {
	ID string

	mu             sync.RWMutex
	state          State
	sandboxID      string
	proxyBaseURL   string
	proxyHeaders   map[string]string
	agentSessionID string
	extraEnv       map[string]string // per-request env vars merged into sandbox at provision time

	// provisionMu serialises provisioning and reset. Lock order: provisionMu → mu.
	provisionMu sync.Mutex
	provisioned bool
}

func (c *Conversation) GetState() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Conversation) SetRunning(sandboxID, proxyBaseURL string, proxyHeaders map[string]string) {
	c.mu.Lock()
	c.state = StateRunning
	c.sandboxID = sandboxID
	c.proxyBaseURL = proxyBaseURL
	c.proxyHeaders = proxyHeaders
	c.mu.Unlock()
}

func (c *Conversation) SetError() {
	c.mu.Lock()
	c.state = StateError
	c.mu.Unlock()
}

func (c *Conversation) SetProvisioning() {
	c.mu.Lock()
	c.state = StateProvisioning
	c.mu.Unlock()
}

func (c *Conversation) GetProxyInfo() (baseURL string, headers map[string]string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proxyBaseURL, c.proxyHeaders
}

func (c *Conversation) GetAgentSessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.agentSessionID
}

func (c *Conversation) SetAgentSessionID(id string) {
	c.mu.Lock()
	c.agentSessionID = id
	c.mu.Unlock()
}

func (c *Conversation) GetSandboxID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sandboxID
}

func (c *Conversation) ExtraEnv() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.extraEnv
}

// EnsureProvisioned calls fn if not yet provisioned. Concurrent callers block until fn returns.
// Unlike sync.Once, a failed fn leaves provisioned=false so the next caller retries.
func (c *Conversation) EnsureProvisioned(fn func() error) error {
	c.provisionMu.Lock()
	defer c.provisionMu.Unlock()
	if c.provisioned {
		return nil
	}
	if err := fn(); err != nil {
		return err
	}
	c.provisioned = true
	return nil
}

// ResetForReprovisioning clears all sandbox state so a new sandbox can be allocated.
// Call this when the previously assigned sandbox has expired or become unreachable.
func (c *Conversation) ResetForReprovisioning() {
	c.provisionMu.Lock()
	defer c.provisionMu.Unlock()
	c.resetLocked()
}

// ResetIfExpired atomically checks sandbox liveness and clears state if the sandbox
// is no longer Running. isAlive is called while provisionMu is held, preventing a
// concurrent re-provision from being stomped by a racing expiry reset. Returns the
// error from isAlive, if any; on error the state is NOT reset.
func (c *Conversation) ResetIfExpired(isAlive func(sandboxID string) (bool, error)) error {
	c.provisionMu.Lock()
	defer c.provisionMu.Unlock()
	if !c.provisioned {
		return nil
	}
	c.mu.RLock()
	id := c.sandboxID
	c.mu.RUnlock()
	if id == "" {
		return nil
	}
	alive, err := isAlive(id)
	if err != nil {
		return err
	}
	if !alive {
		c.resetLocked()
	}
	return nil
}

// resetLocked clears all sandbox state. Caller must hold provisionMu.
func (c *Conversation) resetLocked() {
	c.provisioned = false
	c.mu.Lock()
	c.state = StateNew
	c.sandboxID = ""
	c.proxyBaseURL = ""
	c.proxyHeaders = nil
	c.agentSessionID = ""
	c.mu.Unlock()
}

func (c *Conversation) Info() (id, sandboxID, agentSessionID string, state State) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ID, c.sandboxID, c.agentSessionID, c.state
}

type Store struct {
	mu    sync.RWMutex
	convs map[string]*Conversation
}

func NewStore() *Store {
	return &Store{convs: make(map[string]*Conversation)}
}

func (s *Store) Create(extraEnv map[string]string) *Conversation {
	conv := &Conversation{
		ID:       uuid.New().String(),
		state:    StateNew,
		extraEnv: extraEnv,
	}
	s.mu.Lock()
	s.convs[conv.ID] = conv
	s.mu.Unlock()
	return conv
}

func (s *Store) Get(id string) *Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.convs[id]
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	delete(s.convs, id)
	s.mu.Unlock()
}
