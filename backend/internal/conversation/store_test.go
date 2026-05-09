package conversation

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCreate_StoresConversation(t *testing.T) {
	s := NewStore()
	env := map[string]string{"FOO": "bar"}
	conv := s.Create(env)

	if conv.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if conv.GetState() != StateNew {
		t.Fatalf("expected StateNew, got %v", conv.GetState())
	}
	got := conv.ExtraEnv()
	if got["FOO"] != "bar" {
		t.Fatalf("expected FOO=bar in ExtraEnv, got %v", got)
	}
}

func TestCreate_NilEnv(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	if conv.ExtraEnv() != nil {
		t.Fatalf("expected nil ExtraEnv, got %v", conv.ExtraEnv())
	}
}

func TestGet_Missing(t *testing.T) {
	s := NewStore()
	if s.Get("nonexistent") != nil {
		t.Fatal("expected nil for missing conversation")
	}
}

func TestGet_Found(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	got := s.Get(conv.ID)
	if got != conv {
		t.Fatal("Get did not return the created conversation")
	}
}

func TestDelete_Removes(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	s.Delete(conv.ID)
	if s.Get(conv.ID) != nil {
		t.Fatal("expected nil after Delete")
	}
}

func TestSetRunning(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)

	headers := map[string]string{"Authorization": "Bearer tok"}
	conv.SetRunning("sandbox-1", "http://proxy/", headers)

	if conv.GetState() != StateRunning {
		t.Fatalf("expected StateRunning, got %v", conv.GetState())
	}
	url, hdrs := conv.GetProxyInfo()
	if url != "http://proxy/" {
		t.Fatalf("unexpected proxy URL: %s", url)
	}
	if hdrs["Authorization"] != "Bearer tok" {
		t.Fatalf("unexpected headers: %v", hdrs)
	}
	if conv.GetSandboxID() != "sandbox-1" {
		t.Fatalf("unexpected sandboxID: %s", conv.GetSandboxID())
	}
}

func TestSetError(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	conv.SetError()
	if conv.GetState() != StateError {
		t.Fatalf("expected StateError, got %v", conv.GetState())
	}
}

func TestEnsureProvisioned_CalledOnce(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)

	var callCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conv.EnsureProvisioned(func() error {
				callCount.Add(1)
				return nil
			})
		}()
	}
	wg.Wait()

	if callCount.Load() != 1 {
		t.Fatalf("expected fn called once, called %d times", callCount.Load())
	}
}

// TestEnsureProvisioned_FailedFnRetried verifies that when fn returns an error,
// provisioned stays false and each subsequent caller retries fn independently.
// This differs from sync.Once: errors are not cached — callers can retry.
func TestEnsureProvisioned_FailedFnRetried(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	want := errors.New("provision failed")

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			errs[idx] = conv.EnsureProvisioned(func() error { return want })
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, want) {
			t.Fatalf("goroutine %d: expected provision error, got %v", i, err)
		}
	}
}

func TestResetForReprovisioning_ClearsState(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)

	// Fully provision the conversation.
	if err := conv.EnsureProvisioned(func() error {
		conv.SetRunning("sb-1", "http://proxy/", map[string]string{"k": "v"})
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	conv.SetAgentSessionID("sess-1")

	if conv.GetState() != StateRunning {
		t.Fatalf("expected StateRunning before reset, got %v", conv.GetState())
	}

	conv.ResetForReprovisioning()

	if conv.GetState() != StateNew {
		t.Errorf("expected StateNew after reset, got %v", conv.GetState())
	}
	if conv.GetSandboxID() != "" {
		t.Errorf("expected empty sandboxID after reset, got %q", conv.GetSandboxID())
	}
	url, _ := conv.GetProxyInfo()
	if url != "" {
		t.Errorf("expected empty proxyBaseURL after reset, got %q", url)
	}
	if conv.GetAgentSessionID() != "" {
		t.Errorf("expected empty agentSessionID after reset, got %q", conv.GetAgentSessionID())
	}
}

func TestResetIfExpired_ResetsWhenNotAlive(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	conv.EnsureProvisioned(func() error {
		conv.SetRunning("sb-1", "http://proxy/", map[string]string{})
		return nil
	})

	err := conv.ResetIfExpired(func(_ string) (bool, error) { return false, nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conv.GetState() != StateNew {
		t.Errorf("expected StateNew after expiry reset, got %v", conv.GetState())
	}
	if conv.GetSandboxID() != "" {
		t.Errorf("expected empty sandboxID after expiry reset, got %q", conv.GetSandboxID())
	}
}

func TestResetIfExpired_NoResetWhenAlive(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	conv.EnsureProvisioned(func() error {
		conv.SetRunning("sb-1", "http://proxy/", map[string]string{})
		return nil
	})

	err := conv.ResetIfExpired(func(_ string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conv.GetState() != StateRunning {
		t.Errorf("expected StateRunning when sandbox is alive, got %v", conv.GetState())
	}
}

func TestResetIfExpired_NoResetWhenNotProvisioned(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	called := false

	err := conv.ResetIfExpired(func(_ string) (bool, error) {
		called = true
		return false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// isAlive should not be called if the conversation was never provisioned.
	if called {
		t.Error("expected isAlive not called when conversation is unprovisioned")
	}
}

func TestResetIfExpired_ErrorPreservesState(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	conv.EnsureProvisioned(func() error {
		conv.SetRunning("sb-1", "http://proxy/", map[string]string{})
		return nil
	})
	want := errors.New("network timeout")

	err := conv.ResetIfExpired(func(_ string) (bool, error) { return false, want })
	if !errors.Is(err, want) {
		t.Fatalf("expected error %v, got %v", want, err)
	}
	// State must be preserved — check error must not trigger a reset.
	if conv.GetState() != StateRunning {
		t.Errorf("expected StateRunning preserved on check error, got %v", conv.GetState())
	}
}

func TestResetIfExpired_ConcurrentSafeOnlyResetsOnce(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)
	conv.EnsureProvisioned(func() error {
		conv.SetRunning("sb-expired", "http://proxy/", map[string]string{})
		return nil
	})

	var wg sync.WaitGroup
	var resetCount atomic.Int32
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conv.ResetIfExpired(func(_ string) (bool, error) {
				resetCount.Add(1)
				return false, nil
			})
		}()
	}
	wg.Wait()

	// isAlive is called exactly once: the first caller resets (provisioned→false);
	// all subsequent callers skip because provisioned is already false.
	if resetCount.Load() != 1 {
		t.Errorf("expected isAlive called exactly once, called %d times", resetCount.Load())
	}
}

func TestResetForReprovisioning_AllowsReprovision(t *testing.T) {
	s := NewStore()
	conv := s.Create(nil)

	var callCount atomic.Int32

	// First provision.
	conv.EnsureProvisioned(func() error {
		callCount.Add(1)
		conv.SetRunning("sb-1", "http://proxy/", map[string]string{})
		return nil
	})
	if callCount.Load() != 1 {
		t.Fatalf("expected fn called once, got %d", callCount.Load())
	}

	// Reset and re-provision.
	conv.ResetForReprovisioning()
	conv.EnsureProvisioned(func() error {
		callCount.Add(1)
		conv.SetRunning("sb-2", "http://proxy2/", map[string]string{})
		return nil
	})
	if callCount.Load() != 2 {
		t.Fatalf("expected fn called twice after reset, got %d", callCount.Load())
	}
	if conv.GetSandboxID() != "sb-2" {
		t.Errorf("expected sandboxID=sb-2 after re-provision, got %q", conv.GetSandboxID())
	}
}

func TestStateString(t *testing.T) {
	cases := []struct {
		state State
		want  string
	}{
		{StateNew, "provisioning"},
		{StateProvisioning, "provisioning"},
		{StateRunning, "running"},
		{StateError, "error"},
		{State(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}
