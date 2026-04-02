package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSubagentManager_RegisterAndList(t *testing.T) {
	mgr := NewSubagentManager(4, nil)

	if err := mgr.Register("sub-1", AgentInstanceConfig{Name: "worker-1"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := mgr.Register("sub-2", AgentInstanceConfig{Name: "worker-2"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	list := mgr.ListAgents()
	if len(list) != 2 {
		t.Errorf("got %d agents, want 2", len(list))
	}
}

func TestSubagentManager_DuplicateRegister(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	mgr.Register("sub-1", AgentInstanceConfig{Name: "worker"})

	err := mgr.Register("sub-1", AgentInstanceConfig{Name: "duplicate"})
	if err == nil {
		t.Error("expected error for duplicate register")
	}
}

func TestSubagentManager_Lifecycle(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	mgr.Register("sub-1", AgentInstanceConfig{Name: "worker"})

	if err := mgr.Start("sub-1"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	inst, ok := mgr.GetAgent("sub-1")
	if !ok {
		t.Fatal("agent not found")
	}
	if inst.Status != AgentRunning {
		t.Errorf("status = %v, want Running", inst.Status)
	}

	if err := mgr.Stop("sub-1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	inst, _ = mgr.GetAgent("sub-1")
	if inst.Status != AgentStopped {
		t.Errorf("status = %v, want Stopped", inst.Status)
	}

	if !mgr.Unregister("sub-1") {
		t.Error("Unregister should return true")
	}
	if mgr.Unregister("sub-1") {
		t.Error("second Unregister should return false")
	}
}

func TestSubagentManager_SlotExhaustion(t *testing.T) {
	mgr := NewSubagentManager(2, nil) // only 2 slots

	// Fill both slots
	mgr.Register("sub-1", AgentInstanceConfig{Name: "w1"})
	mgr.Register("sub-2", AgentInstanceConfig{Name: "w2"})
	mgr.Start("sub-1")
	mgr.Start("sub-2")

	if mgr.RunningCount() != 2 {
		t.Fatalf("running = %d, want 2", mgr.RunningCount())
	}

	// Third start should fail (context with short timeout)
	mgr.Register("sub-3", AgentInstanceConfig{Name: "w3"})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := mgr.StartWithContext(ctx, "sub-3")
	if err == nil {
		t.Error("expected error when slots exhausted")
	}
}

func TestSubagentManager_ConcurrentAccess(t *testing.T) {
	mgr := NewSubagentManager(10, nil)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("sub-%d", id)
			mgr.Register(name, AgentInstanceConfig{Name: name})
			mgr.Start(name)
			mgr.Stop(name)
			mgr.Unregister(name)
		}(i)
	}
	wg.Wait()

	if mgr.AgentCount() != 0 {
		t.Errorf("agents remaining: %d", mgr.AgentCount())
	}
}

func TestSubagentManager_Allowlist(t *testing.T) {
	mgr := NewSubagentManager(4, []string{"allowed-1", "allowed-2"})

	if err := mgr.Register("allowed-1", AgentInstanceConfig{Name: "ok"}); err != nil {
		t.Fatalf("allowed ID should register: %v", err)
	}

	err := mgr.Register("blocked-1", AgentInstanceConfig{Name: "bad"})
	if err == nil {
		t.Error("blocked ID should fail to register")
	}
}

func TestSubagentManager_MarkError(t *testing.T) {
	mgr := NewSubagentManager(4, nil)
	mgr.Register("sub-1", AgentInstanceConfig{Name: "worker"})
	mgr.Start("sub-1")

	mgr.MarkError("sub-1", "connection lost")

	inst, _ := mgr.GetAgent("sub-1")
	if inst.Status != AgentError {
		t.Errorf("status = %v, want Error", inst.Status)
	}
	if inst.Error != "connection lost" {
		t.Errorf("error = %q, want %q", inst.Error, "connection lost")
	}
}
