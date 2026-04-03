package orchestration

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AgentStatus represents the lifecycle state of a subagent.
type AgentStatus int

const (
	AgentRegistered AgentStatus = iota
	AgentRunning
	AgentStopped
	AgentError
)

func (s AgentStatus) String() string {
	switch s {
	case AgentRegistered:
		return "registered"
	case AgentRunning:
		return "running"
	case AgentStopped:
		return "stopped"
	case AgentError:
		return "error"
	default:
		return "unknown"
	}
}

// AgentInstanceConfig holds configuration for a subagent.
type AgentInstanceConfig struct {
	Name      string
	AgentID   string
	Workspace string
}

// AgentInstance represents a running or registered subagent.
type AgentInstance struct {
	ID        string
	Config    AgentInstanceConfig
	Status    AgentStatus
	Error     string
	StartedAt time.Time
	UpdatedAt time.Time
}

// SubagentManager controls the lifecycle of child agents with bounded concurrency.
type SubagentManager struct {
	mu         sync.RWMutex
	agents     map[string]*AgentInstance
	semaphore  chan struct{}
	maxSlots   int
	allowedIDs []string // empty = no restriction
}

// NewSubagentManager creates a manager with the given concurrency limit.
func NewSubagentManager(maxSlots int, allowedIDs []string) *SubagentManager {
	return &SubagentManager{
		agents:     make(map[string]*AgentInstance),
		semaphore:  make(chan struct{}, maxSlots),
		maxSlots:   maxSlots,
		allowedIDs: allowedIDs,
	}
}

// Register adds a new subagent in Registered state.
func (m *SubagentManager) Register(id string, cfg AgentInstanceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.agents[id]; exists {
		return fmt.Errorf("agent %q already registered", id)
	}

	if len(m.allowedIDs) > 0 {
		allowed := false
		for _, aid := range m.allowedIDs {
			if aid == id {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("agent %q not in allowlist", id)
		}
	}

	m.agents[id] = &AgentInstance{
		ID:        id,
		Config:    cfg,
		Status:    AgentRegistered,
		UpdatedAt: time.Now(),
	}
	return nil
}

// Start transitions an agent to Running, acquiring a concurrency slot.
func (m *SubagentManager) Start(id string) error {
	return m.StartWithContext(context.Background(), id)
}

// StartWithContext transitions an agent to Running with context-aware slot acquisition.
func (m *SubagentManager) StartWithContext(ctx context.Context, id string) error {
	m.mu.RLock()
	inst, exists := m.agents[id]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("agent %q not found", id)
	}

	// Acquire concurrency slot.
	select {
	case m.semaphore <- struct{}{}:
		// Slot acquired.
	case <-ctx.Done():
		return fmt.Errorf("slot acquisition cancelled: %w", ctx.Err())
	}

	m.mu.Lock()
	inst.Status = AgentRunning
	inst.StartedAt = time.Now()
	inst.UpdatedAt = time.Now()
	m.mu.Unlock()

	return nil
}

// Stop transitions an agent to Stopped, releasing its concurrency slot.
func (m *SubagentManager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, exists := m.agents[id]
	if !exists {
		return fmt.Errorf("agent %q not found", id)
	}

	if inst.Status == AgentRunning {
		// Release concurrency slot.
		select {
		case <-m.semaphore:
		default:
		}
	}

	inst.Status = AgentStopped
	inst.UpdatedAt = time.Now()
	return nil
}

// Unregister removes an agent. Returns true if it was found.
func (m *SubagentManager) Unregister(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	inst, exists := m.agents[id]
	if !exists {
		return false
	}

	// Release slot if running.
	if inst.Status == AgentRunning {
		select {
		case <-m.semaphore:
		default:
		}
	}

	delete(m.agents, id)
	return true
}

// MarkError sets an agent to Error state with a message.
func (m *SubagentManager) MarkError(id string, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if inst, ok := m.agents[id]; ok {
		if inst.Status == AgentRunning {
			select {
			case <-m.semaphore:
			default:
			}
		}
		inst.Status = AgentError
		inst.Error = errMsg
		inst.UpdatedAt = time.Now()
	}
}

// GetAgent returns an agent by ID.
func (m *SubagentManager) GetAgent(id string) (AgentInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.agents[id]
	if !ok {
		return AgentInstance{}, false
	}
	return *inst, true
}

// ListAgents returns all registered agents.
func (m *SubagentManager) ListAgents() []AgentInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]AgentInstance, 0, len(m.agents))
	for _, inst := range m.agents {
		result = append(result, *inst)
	}
	return result
}

// RunningCount returns the number of currently running agents.
func (m *SubagentManager) RunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, inst := range m.agents {
		if inst.Status == AgentRunning {
			count++
		}
	}
	return count
}

// AgentCount returns the total number of registered agents.
func (m *SubagentManager) AgentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

// MaxConcurrent returns the maximum concurrent slots.
func (m *SubagentManager) MaxConcurrent() int { return m.maxSlots }

// AvailableSlots returns the number of unused concurrency slots.
func (m *SubagentManager) AvailableSlots() int {
	return m.maxSlots - len(m.semaphore)
}
