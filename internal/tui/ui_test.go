package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestChatModel_Init(t *testing.T) {
	m := NewChatModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
}

func TestChatModel_TypeAndSend(t *testing.T) {
	m := NewChatModel()

	// Type characters.
	var model tea.Model = m
	for _, ch := range "hello" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}

	cm := model.(ChatModel)
	if cm.Input() != "hello" {
		t.Fatalf("expected input 'hello', got %q", cm.Input())
	}

	// Send with enter.
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm = model.(ChatModel)
	if cm.Input() != "" {
		t.Fatalf("expected empty input after send, got %q", cm.Input())
	}
	if len(cm.Messages()) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cm.Messages()))
	}
	if cm.Messages()[0].Role != "user" || cm.Messages()[0].Content != "hello" {
		t.Fatalf("unexpected message: %+v", cm.Messages()[0])
	}
}

func TestChatModel_Backspace(t *testing.T) {
	m := NewChatModel()
	var model tea.Model = m

	// Type "abc".
	for _, ch := range "abc" {
		model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	// Backspace.
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	cm := model.(ChatModel)
	if cm.Input() != "ab" {
		t.Fatalf("expected 'ab', got %q", cm.Input())
	}
}

func TestChatModel_EmptyEnter(t *testing.T) {
	m := NewChatModel()
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm := model.(ChatModel)
	if len(cm.Messages()) != 0 {
		t.Fatal("expected no message on empty enter")
	}
}

func TestChatModel_WindowSize(t *testing.T) {
	m := NewChatModel()
	model, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	cm := model.(ChatModel)
	if cm.width != 120 || cm.height != 40 {
		t.Fatalf("unexpected dimensions: %dx%d", cm.width, cm.height)
	}
}

func TestChatModel_View(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	m.height = 24
	view := m.View()
	if !strings.Contains(view, "█") {
		t.Fatal("expected cursor in view")
	}
}
