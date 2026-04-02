package agent

import (
	"sync"
	"testing"
)

func TestWorkspace_JoinAndMembers(t *testing.T) {
	ws := NewWorkspace("ws-1", "TestWorkspace")

	if ws.ID() != "ws-1" {
		t.Errorf("ID = %q, want %q", ws.ID(), "ws-1")
	}
	if ws.Name() != "TestWorkspace" {
		t.Errorf("Name = %q, want %q", ws.Name(), "TestWorkspace")
	}

	err := ws.Join(WorkspaceAgent{ID: "agent-1", Name: "Alice", Role: "planner"})
	if err != nil {
		t.Fatalf("Join error: %v", err)
	}
	err = ws.Join(WorkspaceAgent{ID: "agent-2", Name: "Bob", Role: "executor"})
	if err != nil {
		t.Fatalf("Join error: %v", err)
	}

	if ws.MemberCount() != 2 {
		t.Errorf("MemberCount = %d, want 2", ws.MemberCount())
	}

	members := ws.Members()
	if len(members) != 2 {
		t.Errorf("Members len = %d, want 2", len(members))
	}
}

func TestWorkspace_JoinSetsStatusAndTime(t *testing.T) {
	ws := NewWorkspace("ws-2", "StatusWorkspace")
	_ = ws.Join(WorkspaceAgent{ID: "a1", Name: "Agent1"})

	a, ok := ws.GetAgent("a1")
	if !ok {
		t.Fatal("GetAgent should return true for joined agent")
	}
	if a.Status != "active" {
		t.Errorf("Status = %q, want %q", a.Status, "active")
	}
	if a.JoinedAt.IsZero() {
		t.Error("JoinedAt should not be zero")
	}
}

func TestWorkspace_DuplicateJoinError(t *testing.T) {
	ws := NewWorkspace("ws-3", "DupWorkspace")

	err := ws.Join(WorkspaceAgent{ID: "a1", Name: "Agent1"})
	if err != nil {
		t.Fatalf("first Join should succeed: %v", err)
	}

	err = ws.Join(WorkspaceAgent{ID: "a1", Name: "Agent1 again"})
	if err == nil {
		t.Error("duplicate Join should return error")
	}
}

func TestWorkspace_Leave(t *testing.T) {
	ws := NewWorkspace("ws-4", "LeaveWorkspace")
	_ = ws.Join(WorkspaceAgent{ID: "a1", Name: "Agent1"})
	_ = ws.Join(WorkspaceAgent{ID: "a2", Name: "Agent2"})

	left := ws.Leave("a1")
	if !left {
		t.Error("Leave should return true for existing agent")
	}
	if ws.MemberCount() != 1 {
		t.Errorf("MemberCount = %d, want 1 after leave", ws.MemberCount())
	}

	// Leaving again returns false.
	left = ws.Leave("a1")
	if left {
		t.Error("Leave should return false for already-removed agent")
	}

	// Leaving nonexistent returns false.
	left = ws.Leave("nonexistent")
	if left {
		t.Error("Leave should return false for nonexistent agent")
	}
}

func TestWorkspace_GetAgent(t *testing.T) {
	ws := NewWorkspace("ws-5", "GetWorkspace")
	_ = ws.Join(WorkspaceAgent{ID: "a1", Name: "Agent1", Role: "coordinator"})

	a, ok := ws.GetAgent("a1")
	if !ok {
		t.Fatal("GetAgent should find agent")
	}
	if a.Name != "Agent1" {
		t.Errorf("Name = %q, want %q", a.Name, "Agent1")
	}
	if a.Role != "coordinator" {
		t.Errorf("Role = %q, want %q", a.Role, "coordinator")
	}

	_, ok = ws.GetAgent("missing")
	if ok {
		t.Error("GetAgent should return false for missing agent")
	}
}

func TestWorkspace_ConcurrentAccess(t *testing.T) {
	ws := NewWorkspace("ws-6", "ConcurrentWorkspace")
	var wg sync.WaitGroup
	const n = 50

	// Concurrent joins.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = ws.Join(WorkspaceAgent{
				ID:   string(rune('a'+id%26)) + string(rune('0'+id/26)),
				Name: "agent",
			})
		}(i)
	}
	wg.Wait()

	// Concurrent reads.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ws.MemberCount()
			_ = ws.Members()
		}()
	}
	wg.Wait()
}
