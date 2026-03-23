package pipeline

import (
	"strings"
	"testing"
)

func TestExtractSubtasks_Numbered(t *testing.T) {
	content := "Please do:\n1. Fix the bug\n2. Add tests\n3. Update docs"
	tasks := extractSubtasks(content)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d: %v", len(tasks), tasks)
	}
	if tasks[0] != "Fix the bug" {
		t.Errorf("task[0] = %q", tasks[0])
	}
}

func TestExtractSubtasks_Bulleted(t *testing.T) {
	content := "Tasks:\n- Deploy to staging\n- Run smoke tests\n- Notify team"
	tasks := extractSubtasks(content)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestExtractSubtasks_Mixed(t *testing.T) {
	content := "1. First thing\n- Second thing\n* Third thing\n2) Fourth thing"
	tasks := extractSubtasks(content)
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d: %v", len(tasks), tasks)
	}
}

func TestExtractSubtasks_Empty(t *testing.T) {
	tasks := extractSubtasks("")
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestExtractSubtasks_NoTasks(t *testing.T) {
	tasks := extractSubtasks("Just a regular paragraph with no list items.")
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestEvaluateDecomposition_Centralized(t *testing.T) {
	result := EvaluateDecomposition("Hello, how are you?", 0)
	if result.Decision != DecompCentralized {
		t.Errorf("short message: decision = %v, want Centralized", result.Decision)
	}
}

func TestEvaluateDecomposition_Delegated(t *testing.T) {
	// Long content with 3+ subtasks.
	content := strings.Repeat("Context paragraph. ", 20) +
		"\n1. Analyze the data\n2. Generate a report\n3. Send to stakeholders\n4. Schedule follow-up"
	result := EvaluateDecomposition(content, 5)
	if result.Decision != DecompDelegated {
		t.Errorf("multi-task: decision = %v, want Delegated", result.Decision)
	}
	if len(result.Subtasks) < 3 {
		t.Errorf("expected >= 3 subtasks, got %d", len(result.Subtasks))
	}
}

func TestEvaluateDecomposition_SpecialistProposal(t *testing.T) {
	content := strings.Repeat("We need a detailed financial analysis ", 30) +
		"financial analysis of the quarterly revenue data including projections."
	result := EvaluateDecomposition(content, 15)
	if result.Decision != DecompSpecialistProposal {
		t.Errorf("specialist: decision = %v, want SpecialistProposal", result.Decision)
	}
}

func TestEvaluateDecomposition_ShortWithManySubtasks(t *testing.T) {
	// Short content with subtasks but under 200 chars — should stay centralized.
	content := "Do:\n1. A\n2. B\n3. C"
	result := EvaluateDecomposition(content, 0)
	if result.Decision != DecompCentralized {
		t.Errorf("short multi-task: decision = %v, want Centralized", result.Decision)
	}
}
