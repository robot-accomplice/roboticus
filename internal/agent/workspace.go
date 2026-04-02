package agent

import (
	"fmt"
	"sync"
	"time"
)

type WorkspaceAgent struct {
	ID       string
	Name     string
	Role     string
	Status   string
	JoinedAt time.Time
}

type Workspace struct {
	mu     sync.RWMutex
	id     string
	name   string
	agents map[string]*WorkspaceAgent
}

func NewWorkspace(id, name string) *Workspace {
	return &Workspace{id: id, name: name, agents: make(map[string]*WorkspaceAgent)}
}

func (w *Workspace) ID() string   { return w.id }
func (w *Workspace) Name() string { return w.name }

func (w *Workspace) Join(agent WorkspaceAgent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.agents[agent.ID]; exists {
		return fmt.Errorf("agent %q already in workspace", agent.ID)
	}
	agent.JoinedAt = time.Now()
	agent.Status = "active"
	w.agents[agent.ID] = &agent
	return nil
}

func (w *Workspace) Leave(agentID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.agents[agentID]; !exists {
		return false
	}
	delete(w.agents, agentID)
	return true
}

func (w *Workspace) Members() []WorkspaceAgent {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]WorkspaceAgent, 0, len(w.agents))
	for _, a := range w.agents {
		result = append(result, *a)
	}
	return result
}

func (w *Workspace) MemberCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.agents)
}

func (w *Workspace) GetAgent(id string) (WorkspaceAgent, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	a, ok := w.agents[id]
	if !ok {
		return WorkspaceAgent{}, false
	}
	return *a, true
}
